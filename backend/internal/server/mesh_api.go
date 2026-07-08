package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// handleMeshStatus reports hub state and peer reachability.
func (a *API) handleMeshStatus(w http.ResponseWriter, r *http.Request) {
	if a.Mesh == nil {
		writeJSON(w, map[string]any{"enabled": false})
		return
	}
	type peerInfo struct {
		NodeID  string `json:"nodeId"`
		MeshIP  string `json:"meshIp"`
		Reachable bool `json:"reachable,omitempty"`
	}
	var peers []peerInfo
	a.Store.View(func() {
		for _, n := range a.Store.Nodes {
			if n.MeshIP != "" {
				peers = append(peers, peerInfo{NodeID: n.ID, MeshIP: n.MeshIP})
			}
		}
	})
	writeJSON(w, map[string]any{
		"enabled": true,
		"hubIp":   "10.77.0.1",
		"hubKey":  a.Mesh.Keys.Public,
		"udpPort": a.Mesh.UDPPort,
		"peers":   peers,
	})
}

// handleMeshDial tests TCP connectivity to a mesh address (debug/relay probe).
func (a *API) handleMeshDial(w http.ResponseWriter, r *http.Request) {
	if a.Mesh == nil {
		http.Error(w, "mesh disabled", http.StatusServiceUnavailable)
		return
	}
	addr := r.URL.Query().Get("addr")
	if addr == "" {
		http.Error(w, "addr required (e.g. 10.77.0.2:8080)", http.StatusBadRequest)
		return
	}
	start := time.Now()
	conn, err := a.Mesh.Net.Dial(addr)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	_ = conn.Close()
	writeJSON(w, map[string]any{"ok": true, "rttMs": time.Since(start).Milliseconds()})
}

// RelayHandler reverse-proxies public HTTP traffic to services on mesh nodes.
// Route: /relay/{serviceId}/... — used by ingress nodes to reach workloads
// running on CGNAT'd machines that have no publicly reachable address.
func (a *API) handleRelay(w http.ResponseWriter, r *http.Request) {
	if a.Mesh == nil {
		http.Error(w, "mesh disabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	var meshIP string
	var port int
	a.Store.View(func() {
		if svc := a.Store.Services[id]; svc != nil {
			port = svc.Port
			if n := a.Store.Nodes[svc.NodeID]; n != nil {
				meshIP = n.MeshIP
			}
		}
	})
	if meshIP == "" || port == 0 {
		http.Error(w, "service not found or not mesh-reachable", http.StatusNotFound)
		return
	}

	target := fmt.Sprintf("%s:%d", meshIP, port)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(&url.URL{Scheme: "http", Host: target})
			pr.Out.URL.Path = "/" + r.PathValue("rest")
			pr.Out.URL.RawQuery = r.URL.RawQuery
			pr.SetXForwarded()
		},
		Transport: &http.Transport{
			// Dial over the WireGuard mesh instead of the host network.
			DialContext: func(_ context.Context, network, addr string) (net.Conn, error) {
				return a.Mesh.Net.Dial(addr)
			},
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "relay: "+err.Error(), http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}
