package server

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rackops/rackops/internal/mesh"
	"github.com/rackops/rackops/internal/types"
)

// MeshHub is the control plane's mesh coordinator: it runs the WireGuard hub
// (10.77.0.1), allocates member IPs, and answers key exchanges from agents.
type MeshHub struct {
	Net     *mesh.Net
	Keys    mesh.KeyPair
	UDPPort int
	// AdvertiseHost is the address agents can reach this server on.
	AdvertiseHost string

	mu        sync.Mutex
	peers     map[string]types.MeshPeer // nodeID -> peer
	nextIP    int                       // last allocated host octet pair
	dataDir   string
	allocPath string
}

// meshAllocations is the on-disk form of the hub's IP assignments, persisted so
// a node keeps its mesh IP across hub restarts (RelayPort routing in the store
// is pinned to it, so reallocating would silently break domain routing).
type meshAllocations struct {
	NextIP int                       `json:"nextIp"`
	Peers  map[string]types.MeshPeer `json:"peers"`
}

// NewMeshHub starts the hub, loading persisted keys and IP allocations from
// dataDir and re-registering known peers with the fresh WireGuard device.
func NewMeshHub(dataDir, advertiseHost string, udpPort int) (*MeshHub, error) {
	h := &MeshHub{
		UDPPort:       udpPort,
		AdvertiseHost: advertiseHost,
		peers:         map[string]types.MeshPeer{},
		nextIP:        1, // .0.1 is the hub itself
		dataDir:       dataDir,
		allocPath:     filepath.Join(dataDir, "mesh-hub.json"),
	}
	h.loadAllocations()

	// Load or create hub keys.
	keyPath := filepath.Join(dataDir, "mesh-hub.key")
	if data, err := os.ReadFile(keyPath); err == nil {
		parts := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
		if len(parts) == 2 {
			h.Keys = mesh.KeyPair{Private: parts[0], Public: parts[1]}
		}
	}
	if h.Keys.Private == "" {
		kp, err := mesh.NewKeyPair()
		if err != nil {
			return nil, err
		}
		h.Keys = kp
		_ = os.WriteFile(keyPath, []byte(kp.Private+"\n"+kp.Public), 0o600)
	}

	n, err := mesh.Up(mesh.Config{
		PrivateKey: h.Keys.Private,
		MeshIP:     "10.77.0.1",
		ListenPort: udpPort,
	})
	if err != nil {
		return nil, err
	}
	h.Net = n

	// Re-register persisted peers so routing works before each agent's next
	// key exchange — the fresh device otherwise knows no peers.
	for id, p := range h.peers {
		if p.PublicKey == "" {
			continue
		}
		if err := h.Net.AddPeer(mesh.Peer{PublicKey: p.PublicKey, MeshIP: p.MeshIP}); err != nil {
			log.Printf("mesh: restore peer %s: %v", id, err)
		}
	}
	log.Printf("mesh: hub up at 10.77.0.1 (udp :%d, %d known peers)", udpPort, len(h.peers))
	return h, nil
}

// loadAllocations restores persisted IP assignments (best-effort).
func (h *MeshHub) loadAllocations() {
	data, err := os.ReadFile(h.allocPath)
	if err != nil {
		return
	}
	var a meshAllocations
	if err := json.Unmarshal(data, &a); err != nil {
		log.Printf("mesh: ignoring corrupt allocations file: %v", err)
		return
	}
	if a.Peers != nil {
		h.peers = a.Peers
	}
	if a.NextIP > h.nextIP {
		h.nextIP = a.NextIP
	}
}

// saveAllocations persists IP assignments atomically. Callers hold h.mu.
func (h *MeshHub) saveAllocations() {
	data, err := json.Marshal(meshAllocations{NextIP: h.nextIP, Peers: h.peers})
	if err != nil {
		return
	}
	tmp := h.allocPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		log.Printf("mesh: persist allocations: %v", err)
		return
	}
	_ = os.Rename(tmp, h.allocPath)
}

// HandleExchange processes an agent's mesh info and returns its assignment +
// the current peer list. Called from the ingest handler.
func (h *MeshHub) HandleExchange(nodeID string, in *types.MeshInfo) *types.MeshInfo {
	if in == nil || in.PublicKey == "" {
		return nil
	}
	h.mu.Lock()
	peer, known := h.peers[nodeID]
	if !known || peer.PublicKey != in.PublicKey {
		if !known {
			h.nextIP++
			peer = types.MeshPeer{
				NodeID: nodeID,
				MeshIP: fmt.Sprintf("10.77.%d.%d", h.nextIP/256, h.nextIP%256),
			}
		}
		peer.PublicKey = in.PublicKey
		h.peers[nodeID] = peer
		// Register with the hub interface (dial-in peer: no endpoint).
		if err := h.Net.AddPeer(mesh.Peer{PublicKey: peer.PublicKey, MeshIP: peer.MeshIP}); err != nil {
			log.Printf("mesh: add peer %s: %v", nodeID, err)
		} else {
			log.Printf("mesh: node %s joined as %s", nodeID, peer.MeshIP)
		}
		h.saveAllocations()
	}
	// Snapshot the full peer list for member-to-member awareness.
	list := make([]types.MeshPeer, 0, len(h.peers))
	for _, p := range h.peers {
		list = append(list, p)
	}
	h.mu.Unlock()

	return &types.MeshInfo{
		MeshIP:  peer.MeshIP,
		HubKey:  h.Keys.Public,
		HubAddr: fmt.Sprintf("%s:%d", h.AdvertiseHost, h.UDPPort),
		Peers:   list,
	}
}

// PeerIP returns a node's mesh IP ("" if not joined).
func (h *MeshHub) PeerIP(nodeID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.peers[nodeID].MeshIP
}
