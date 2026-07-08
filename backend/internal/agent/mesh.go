package agent

import (
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/rackops/rackops/internal/mesh"
	"github.com/rackops/rackops/internal/types"
)

// meshState tracks the agent's mesh membership.
type meshState struct {
	keys   mesh.KeyPair
	net    *mesh.Net
	meshIP string
	hubKey string
	// relays: localPort -> listener forwarding to a mesh address.
	relays map[int]net.Listener
}

// initMeshKeys loads or creates the agent's WireGuard identity.
func (a *Agent) initMeshKeys() error {
	keyPath := filepath.Join(a.cfg.DataDir, "mesh-node.key")
	if data, err := os.ReadFile(keyPath); err == nil {
		parts := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
		if len(parts) == 2 {
			a.mesh.keys = mesh.KeyPair{Private: parts[0], Public: parts[1]}
			return nil
		}
	}
	kp, err := mesh.NewKeyPair()
	if err != nil {
		return err
	}
	a.mesh.keys = kp
	return os.WriteFile(keyPath, []byte(kp.Private+"\n"+kp.Public), 0o600)
}

// syncMesh applies the server's mesh assignment: brings the interface up on
// first assignment and keeps exposed service ports proxied onto the mesh.
func (a *Agent) syncMesh(info *types.MeshInfo, containers []types.ContainerStatus, ports []int) {
	if info == nil || info.MeshIP == "" {
		return
	}
	// Bring the mesh up once we know our IP.
	if a.mesh.net == nil {
		n, err := mesh.Up(mesh.Config{
			PrivateKey: a.mesh.keys.Private,
			MeshIP:     info.MeshIP,
			Peers: []mesh.Peer{{
				PublicKey: info.HubKey,
				MeshIP:    "10.77.0.1",
				Endpoint:  info.HubAddr,
			}},
		})
		if err != nil {
			log.Printf("mesh: up failed: %v", err)
			return
		}
		a.mesh.net = n
		a.mesh.meshIP = info.MeshIP
		a.mesh.hubKey = info.HubKey
		log.Printf("mesh: joined as %s via hub %s", info.MeshIP, info.HubAddr)
	}

	// Expose the requested service ports on the mesh so ingress/relay nodes
	// can reach them regardless of NAT between nodes.
	want := map[int]bool{}
	for _, p := range ports {
		if p > 0 {
			want[p] = true
		}
	}
	for _, p := range a.mesh.net.ProxiedPorts() {
		if !want[p] {
			a.mesh.net.UnproxyPort(p)
		}
	}
	for p := range want {
		if err := a.mesh.net.ProxyPort(p, localhostPort(p)); err != nil {
			log.Printf("mesh: proxy port %d: %v", p, err)
		}
	}
	_ = containers

	// Relay listeners (ingress node): forward 127.0.0.1:<port> over the mesh
	// to services running on other nodes, so Traefik can route to them.
	if a.mesh.relays == nil {
		a.mesh.relays = map[int]net.Listener{}
	}
	wantRelay := map[int]string{}
	for _, r := range info.Relays {
		wantRelay[r.LocalPort] = r.TargetAddr
	}
	for port, ln := range a.mesh.relays {
		if _, ok := wantRelay[port]; !ok {
			_ = ln.Close()
			delete(a.mesh.relays, port)
		}
	}
	for port, target := range wantRelay {
		if _, exists := a.mesh.relays[port]; exists {
			continue
		}
		ln, err := net.Listen("tcp", "127.0.0.1:"+itoa(port))
		if err != nil {
			log.Printf("mesh relay: listen %d: %v", port, err)
			continue
		}
		a.mesh.relays[port] = ln
		go a.acceptRelay(ln, target)
		log.Printf("mesh relay: 127.0.0.1:%d -> %s", port, target)
	}
}

// acceptRelay pipes accepted local connections to a mesh address.
func (a *Agent) acceptRelay(ln net.Listener, target string) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			out, err := a.mesh.net.Dial(target)
			if err != nil {
				log.Printf("mesh relay: dial %s: %v", target, err)
				return
			}
			defer out.Close()
			done := make(chan struct{}, 2)
			go func() { _, _ = io.Copy(out, c); done <- struct{}{} }()
			go func() { _, _ = io.Copy(c, out); done <- struct{}{} }()
			<-done
		}()
	}
}

func localhostPort(p int) string {
	return "127.0.0.1:" + itoa(p)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
