// Package mesh implements RackOps' secure connectivity layer: a userspace
// WireGuard network (wireguard-go + netstack) that needs no root, no TUN
// device, and no port forwarding on member nodes. The control plane runs a
// hub (10.77.0.1); agents dial out to it — CGNAT-safe by construction —
// and expose a TCP proxy so any mesh peer can reach services on the node.
package mesh

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"sync"

	"golang.org/x/crypto/curve25519"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// KeyPair is a WireGuard Curve25519 key pair.
type KeyPair struct {
	Private string // base64
	Public  string // base64
}

// NewKeyPair generates a WireGuard key pair.
func NewKeyPair() (KeyPair, error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return KeyPair{}, err
	}
	// Clamp per Curve25519.
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{
		Private: base64.StdEncoding.EncodeToString(priv[:]),
		Public:  base64.StdEncoding.EncodeToString(pub),
	}, nil
}

func b64ToHex(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// Peer describes a WireGuard peer for config generation.
type Peer struct {
	PublicKey string // base64
	MeshIP    string // 10.77.x.x
	Endpoint  string // host:port, empty for dial-in-only peers
}

// Net is a running userspace WireGuard network member.
type Net struct {
	dev  *device.Device
	tnet *netstack.Net
	ip   netip.Addr

	mu       sync.Mutex
	proxies  map[int]net.Listener // mesh port -> listener
	stopping bool
}

// Config configures a mesh member.
type Config struct {
	PrivateKey string // base64
	MeshIP     string // this node's mesh address
	ListenPort int    // UDP port (hub only; 0 = ephemeral for dial-out nodes)
	Peers      []Peer
}

// Up brings the mesh interface up.
func Up(cfg Config) (*Net, error) {
	ip, err := netip.ParseAddr(cfg.MeshIP)
	if err != nil {
		return nil, fmt.Errorf("bad mesh ip: %w", err)
	}
	tun, tnet, err := netstack.CreateNetTUN([]netip.Addr{ip}, nil, 1420)
	if err != nil {
		return nil, err
	}
	dev := device.NewDevice(tun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "mesh "))

	privHex, err := b64ToHex(cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	uapi := "private_key=" + privHex + "\n"
	if cfg.ListenPort > 0 {
		uapi += fmt.Sprintf("listen_port=%d\n", cfg.ListenPort)
	}
	for _, p := range cfg.Peers {
		pubHex, err := b64ToHex(p.PublicKey)
		if err != nil {
			return nil, err
		}
		uapi += "public_key=" + pubHex + "\n"
		uapi += "allowed_ip=" + p.MeshIP + "/32\n"
		if p.Endpoint != "" {
			uapi += "endpoint=" + p.Endpoint + "\n"
			uapi += "persistent_keepalive_interval=25\n"
		}
	}
	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		return nil, fmt.Errorf("wg config: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, err
	}
	return &Net{dev: dev, tnet: tnet, ip: ip, proxies: map[int]net.Listener{}}, nil
}

// AddPeer adds or updates a peer at runtime.
func (n *Net) AddPeer(p Peer) error {
	pubHex, err := b64ToHex(p.PublicKey)
	if err != nil {
		return err
	}
	uapi := "public_key=" + pubHex + "\nallowed_ip=" + p.MeshIP + "/32\n"
	if p.Endpoint != "" {
		uapi += "endpoint=" + p.Endpoint + "\npersistent_keepalive_interval=25\n"
	}
	return n.dev.IpcSet(uapi)
}

// Dial opens a TCP connection to a mesh address (e.g. "10.77.0.5:8080").
func (n *Net) Dial(addr string) (net.Conn, error) {
	return n.tnet.Dial("tcp", addr)
}

// ProxyPort exposes local TCP port `local` on the mesh at the same port:
// connections arriving over WireGuard to <meshIP>:port are forwarded to
// 127.0.0.1:local on this machine.
func (n *Net) ProxyPort(port int, localAddr string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, exists := n.proxies[port]; exists {
		return nil
	}
	ln, err := n.tnet.ListenTCP(&net.TCPAddr{IP: n.ip.AsSlice(), Port: port})
	if err != nil {
		return err
	}
	n.proxies[port] = ln
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go forward(c, localAddr)
		}
	}()
	return nil
}

// UnproxyPort stops exposing a port on the mesh.
func (n *Net) UnproxyPort(port int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if ln, ok := n.proxies[port]; ok {
		_ = ln.Close()
		delete(n.proxies, port)
	}
}

// ProxiedPorts lists currently exposed mesh ports.
func (n *Net) ProxiedPorts() []int {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]int, 0, len(n.proxies))
	for p := range n.proxies {
		out = append(out, p)
	}
	return out
}

// ProxyMesh listens on this member's mesh address at `port` and forwards
// connections to another mesh address (dialed over the mesh). Used by the
// hub to relay spoke-to-spoke TCP, since netstack does not IP-forward.
func (n *Net) ProxyMesh(port int, targetMeshAddr string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, exists := n.proxies[port]; exists {
		return nil
	}
	ln, err := n.tnet.ListenTCP(&net.TCPAddr{IP: n.ip.AsSlice(), Port: port})
	if err != nil {
		return err
	}
	n.proxies[port] = ln
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				out, err := n.tnet.Dial("tcp", targetMeshAddr)
				if err != nil {
					log.Printf("mesh hub relay: dial %s: %v", targetMeshAddr, err)
					return
				}
				defer out.Close()
				done := make(chan struct{}, 2)
				go func() { _, _ = io.Copy(out, c); done <- struct{}{} }()
				go func() { _, _ = io.Copy(c, out); done <- struct{}{} }()
				<-done
			}()
		}
	}()
	return nil
}

// Close tears the mesh interface down.
func (n *Net) Close() {
	n.mu.Lock()
	n.stopping = true
	for _, ln := range n.proxies {
		_ = ln.Close()
	}
	n.mu.Unlock()
	n.dev.Close()
}

func forward(src net.Conn, dst string) {
	defer src.Close()
	out, err := net.Dial("tcp", dst)
	if err != nil {
		log.Printf("mesh proxy: dial %s: %v", dst, err)
		return
	}
	defer out.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(out, src); done <- struct{}{} }()
	go func() { _, _ = io.Copy(src, out); done <- struct{}{} }()
	<-done
}
