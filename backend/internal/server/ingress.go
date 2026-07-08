package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// ingressServiceID is the well-known pseudo-service id for the Traefik edge.
// Kept stable so deploy/teardown/health-correlation all agree.
const ingressServiceID = "ingress-traefik"

// relayPortBase is the first port used for hub->spoke TCP relay listeners.
const relayPortBase = 42000

// userEmail resolves the requesting user's email (best-effort; "" if unknown).
func (a *API) userEmail(r *http.Request) string {
	c := claimsFrom(r)
	if c == nil {
		return ""
	}
	var email string
	a.Store.View(func() {
		if u := a.Store.Users[c.Sub]; u != nil {
			email = u.Email
		}
	})
	return email
}

// ---- Domains CRUD ----

func (a *API) handleListDomains(w http.ResponseWriter, r *http.Request) {
	var out []*types.Domain
	a.Store.View(func() {
		for _, d := range a.Store.Domains {
			out = append(out, d)
		}
	})
	writeJSON(w, out)
}

func (a *API) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.ToLower(strings.TrimSpace(req.Name))
	if name == "" || !strings.Contains(name, ".") {
		http.Error(w, "a valid domain name is required", http.StatusBadRequest)
		return
	}
	// Reject duplicates.
	var dup bool
	a.Store.View(func() {
		for _, d := range a.Store.Domains {
			if d.Name == name {
				dup = true
			}
		}
	})
	if dup {
		http.Error(w, "domain already added", http.StatusConflict)
		return
	}
	d := &types.Domain{ID: NewID(), Name: name, CreatedBy: a.userEmail(r), CreatedAt: time.Now()}
	a.Store.Mutate(func() { a.Store.Domains[d.ID] = d })
	a.Store.AddEvent("domain", "Domain added: "+name, "", "")
	writeJSON(w, d)
}

func (a *API) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var name string
	a.Store.Mutate(func() {
		if d := a.Store.Domains[id]; d != nil {
			name = d.Name
		}
		delete(a.Store.Domains, id)
		// Detach any service still pointing at this domain so we don't leave
		// dangling routers in the Traefik config.
		for _, svc := range a.Store.Services {
			if name != "" && svc.Domain == name {
				svc.Domain = ""
				svc.Ingress = types.IngressNone
				svc.RelayPort = 0
				svc.UpdatedAt = time.Now()
			}
		}
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---- Ingress setup ----

// handleGetIngress returns the current edge configuration and live status.
func (a *API) handleGetIngress(w http.ResponseWriter, r *http.Request) {
	var cfg types.IngressConfig
	a.Store.View(func() { cfg = a.Store.Ingress })
	writeJSON(w, cfg)
}

// handleSetIngress configures the edge and deploys Traefik to the chosen node.
func (a *API) handleSetIngress(w http.ResponseWriter, r *http.Request) {
	var cfg types.IngressConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 80
	}
	if cfg.HTTPSPort == 0 {
		cfg.HTTPSPort = 443
	}
	cfg.ACMEEmail = strings.TrimSpace(cfg.ACMEEmail)
	var node *types.Node
	a.Store.View(func() { node = a.Store.Nodes[cfg.NodeID] })
	if cfg.Enabled && node == nil {
		http.Error(w, "ingress node not found", http.StatusBadRequest)
		return
	}

	if cfg.Enabled {
		cfg.Status = "pending"
		cfg.StatusMsg = "deploying Traefik…"
		cfg.LastDeploy = time.Now()
		a.Store.Mutate(func() { a.Store.Ingress = cfg })
		a.deployTraefik(cfg)
		a.Store.AddEvent("ingress", fmt.Sprintf("Ingress enabling on %s (Traefik :%d/:%d)", node.Name, cfg.HTTPPort, cfg.HTTPSPort), cfg.NodeID, "")
	} else {
		// Tear down.
		prevNode := cfg.NodeID
		a.Store.View(func() { prevNode = a.Store.Ingress.NodeID })
		cfg.Status = ""
		cfg.StatusMsg = ""
		a.Store.Mutate(func() { a.Store.Ingress = cfg })
		payload, _ := json.Marshal(DeployPayload{ServiceID: ingressServiceID, Name: "edge"})
		a.Store.EnqueueTask(prevNode, types.AgentTask{
			ID: NewID(), Kind: types.TaskRemove, ServiceID: ingressServiceID,
			Payload: string(payload), CreatedAt: time.Now(),
		})
		a.Store.AddEvent("ingress", "Ingress disabled", prevNode, "")
	}
	a.Hub.Broadcast("ingress_update", cfg)
	writeJSON(w, cfg)
}

// applyIngressResult records the Traefik edge deploy outcome as ingress health.
func (a *API) applyIngressResult(nodeID string, res types.TaskResult) {
	var cfg types.IngressConfig
	a.Store.Mutate(func() {
		if res.OK {
			a.Store.Ingress.Status = "running"
			a.Store.Ingress.StatusMsg = ""
		} else {
			a.Store.Ingress.Status = "failed"
			a.Store.Ingress.StatusMsg = tailLog(res.Log, 400)
		}
		cfg = a.Store.Ingress
	})
	if res.OK {
		a.Store.AddEvent("ingress", "Edge (Traefik) is running", nodeID, "")
	} else {
		a.Store.AddEvent("ingress", "Edge (Traefik) failed to start: "+tailLog(res.Log, 160), nodeID, "")
	}
	a.Hub.Broadcast("ingress_update", cfg)
}

// tailLog returns the last n characters of s (trimmed), for compact messages.
func tailLog(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = "…" + s[len(s)-n:]
	}
	return s
}

// deployTraefik queues the Traefik edge container on the ingress node.
// Host networking keeps backends reachable at 127.0.0.1 (local services and
// mesh relay listeners alike).
func (a *API) deployTraefik(cfg types.IngressConfig) {
	serverAddr := a.advertiseAddr()
	cmd := []string{
		"--providers.http.endpoint=http://" + serverAddr + "/api/traefik/config",
		"--providers.http.pollInterval=5s",
		fmt.Sprintf("--entrypoints.web.address=:%d", cfg.HTTPPort),
		fmt.Sprintf("--entrypoints.websecure.address=:%d", cfg.HTTPSPort),
		"--api.dashboard=false",
		"--log.level=INFO",
	}
	if cfg.ACMEEmail != "" {
		cmd = append(cmd,
			"--certificatesresolvers.le.acme.email="+cfg.ACMEEmail,
			"--certificatesresolvers.le.acme.storage=/data/acme.json",
			"--certificatesresolvers.le.acme.httpchallenge=true",
			"--certificatesresolvers.le.acme.httpchallenge.entrypoint=web",
			// Push all plain HTTP to HTTPS once certs can be issued.
			"--entrypoints.web.http.redirections.entrypoint.to=websecure",
			"--entrypoints.web.http.redirections.entrypoint.scheme=https",
		)
	}
	payload, _ := json.Marshal(DeployPayload{
		ServiceID: ingressServiceID,
		Name:      "edge",
		Image:     "traefik:v3.1",
		Command:   strings.Join(cmd, " "),
		Volumes:   []string{"/data"},
		HostNet:   true,
	})
	a.Store.EnqueueTask(cfg.NodeID, types.AgentTask{
		ID: NewID(), Kind: types.TaskDeploy, ServiceID: ingressServiceID,
		Payload: string(payload), CreatedAt: time.Now(),
	})
}

// advertiseAddr is the host:port agents/Traefik use to reach this server.
func (a *API) advertiseAddr() string {
	if a.Mesh != nil {
		return fmt.Sprintf("%s:%s", a.Mesh.AdvertiseHost, a.listenPort())
	}
	return "127.0.0.1:" + a.listenPort()
}

// ListenPort is set by main at startup.
var listenPortVal = "8080"

// SetListenPort records the server's HTTP port for config generation.
func SetListenPort(p string) { listenPortVal = p }

func (a *API) listenPort() string { return listenPortVal }

// relaySpecsFor returns the relay listeners the given node should run:
// non-empty only for the ingress node, one spec per domain-routed service
// hosted on a different node. Relay ports are allocated from relayPortBase.
//
// The store lock is held ONLY to read/allocate ports; the actual mesh proxy
// reconciliation (which opens/closes listeners = network I/O) runs afterward,
// outside the lock, and prunes relays for services that were deleted/unrouted.
func (a *API) relaySpecsFor(nodeID string) []types.RelaySpec {
	var specs []types.RelaySpec
	desired := map[int]string{} // relayPort -> meshIP:port on the hub

	a.Store.Mutate(func() {
		if !a.Store.Ingress.Enabled || a.Store.Ingress.NodeID != nodeID {
			return
		}
		used := map[int]bool{}
		for _, svc := range a.Store.Services {
			if svc.RelayPort > 0 {
				used[svc.RelayPort] = true
			}
		}
		next := relayPortBase
		alloc := func() int {
			for used[next] {
				next++
			}
			used[next] = true
			return next
		}
		for _, svc := range a.Store.Services {
			if svc.Domain == "" || svc.Port == 0 || svc.NodeID == nodeID {
				// Local or unrouted services don't need a relay; drop any stale port.
				if svc.NodeID == nodeID {
					svc.RelayPort = 0
				}
				continue
			}
			node := a.Store.Nodes[svc.NodeID]
			if node == nil || node.MeshIP == "" {
				continue
			}
			if svc.RelayPort == 0 {
				svc.RelayPort = alloc()
			}
			desired[svc.RelayPort] = fmt.Sprintf("%s:%d", node.MeshIP, svc.Port)
			specs = append(specs, types.RelaySpec{
				LocalPort:  svc.RelayPort,
				TargetAddr: fmt.Sprintf("10.77.0.1:%d", svc.RelayPort),
			})
		}
	})

	// Reconcile hub-side relay listeners OUTSIDE the store lock.
	a.reconcileRelays(desired)
	return specs
}

// reconcileRelays makes the hub's mesh relay listeners match `desired`
// (relayPort -> targetMeshAddr): opens missing ones, closes stale ones. This
// is what actually frees the port + goroutine when a routed service is
// deleted or unrouted.
func (a *API) reconcileRelays(desired map[int]string) {
	if a.Mesh == nil {
		return
	}
	for _, p := range a.Mesh.Net.ProxiedPorts() {
		if p < relayPortBase {
			continue // not a relay port
		}
		if _, ok := desired[p]; !ok {
			a.Mesh.Net.UnproxyPort(p)
		}
	}
	for port, target := range desired {
		_ = a.Mesh.Net.ProxyMesh(port, target)
	}
}

// handleSetServiceDomain assigns (or clears) a domain + ingress mode on a service.
func (a *API) handleSetServiceDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain  string            `json:"domain"`
		Ingress types.IngressType `json:"ingress"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var svc *types.Service
	a.Store.View(func() { svc = a.Store.Services[r.PathValue("id")] })
	if svc == nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	a.Store.Mutate(func() {
		svc.Domain = domain
		svc.Ingress = req.Ingress
		if svc.Domain == "" {
			svc.Ingress = types.IngressNone
			svc.RelayPort = 0
		} else if svc.Ingress == "" {
			// Infer mode from placement relative to the edge.
			if svc.NodeID == a.Store.Ingress.NodeID {
				svc.Ingress = types.IngressStatic
			} else {
				svc.Ingress = types.IngressRelay
			}
		}
		svc.UpdatedAt = time.Now()
	})
	if svc.Domain != "" {
		a.Store.AddEvent("ingress", fmt.Sprintf("%s routed to %s", svc.Name, svc.Domain), svc.NodeID, svc.ID)
	}
	a.Hub.Broadcast("service_update", svc)
	writeJSON(w, svc)
}

// handleExposeService assigns an instant nip.io URL to a service so it is
// reachable immediately, without the user owning or configuring any domain.
// The name resolves to the ingress node's public (or LAN) IP via nip.io.
func (a *API) handleExposeService(w http.ResponseWriter, r *http.Request) {
	var svc *types.Service
	var edgeIP string
	var edgeEnabled bool
	a.Store.View(func() {
		svc = a.Store.Services[r.PathValue("id")]
		edgeEnabled = a.Store.Ingress.Enabled
		if n := a.Store.Nodes[a.Store.Ingress.NodeID]; n != nil {
			edgeIP = firstNonEmpty(n.PublicIP, n.IP)
		}
	})
	if svc == nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	if svc.Port == 0 {
		http.Error(w, "service has no HTTP port to expose", http.StatusBadRequest)
		return
	}
	if !edgeEnabled || edgeIP == "" {
		http.Error(w, "enable the edge (ingress) on a node with a reachable IP first", http.StatusConflict)
		return
	}
	domain := autoDomain(svc.Name, edgeIP)
	a.Store.Mutate(func() {
		svc.Domain = domain
		if svc.NodeID == a.Store.Ingress.NodeID {
			svc.Ingress = types.IngressStatic
		} else {
			svc.Ingress = types.IngressRelay
		}
		svc.UpdatedAt = time.Now()
	})
	a.Store.AddEvent("ingress", fmt.Sprintf("%s exposed at %s", svc.Name, domain), svc.NodeID, svc.ID)
	a.Hub.Broadcast("service_update", svc)
	writeJSON(w, map[string]string{"domain": domain, "url": "http://" + domain})
}

// autoDomain builds a zero-DNS nip.io hostname: <name>.<ip-with-dashes>.nip.io.
func autoDomain(name, ip string) string {
	return sanitizeName(name) + "." + strings.ReplaceAll(ip, ".", "-") + "." + types.AutoDomainSuffix
}

// sanitizeName lowercases and keeps DNS-label-safe characters.
func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else if r == ' ' || r == '_' || r == '.' {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "app"
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- Traefik dynamic config (HTTP provider) ----

// handleTraefikConfig renders the dynamic configuration for the edge:
// one router+service per RackOps service that has a domain assigned.
func (a *API) handleTraefikConfig(w http.ResponseWriter, r *http.Request) {
	routers := map[string]any{}
	services := map[string]any{}

	var ingress types.IngressConfig
	a.Store.View(func() {
		ingress = a.Store.Ingress
		for _, svc := range a.Store.Services {
			if svc.Domain == "" || svc.Port == 0 || svc.Status != types.StatusRunning {
				continue
			}
			node := a.Store.Nodes[svc.NodeID]
			if node == nil {
				continue
			}
			var backend string
			if svc.NodeID == ingress.NodeID {
				// Local to the ingress node.
				backend = fmt.Sprintf("http://127.0.0.1:%d", svc.Port)
			} else if svc.RelayPort > 0 {
				// Remote: reached via the agent's mesh relay listener.
				backend = fmt.Sprintf("http://127.0.0.1:%d", svc.RelayPort)
			} else {
				continue
			}
			key := "rackops-" + svc.ID
			router := map[string]any{
				"rule":        fmt.Sprintf("Host(`%s`)", svc.Domain),
				"service":     key,
				"entryPoints": []string{"web"},
			}
			if ingress.ACMEEmail != "" {
				router["entryPoints"] = []string{"web", "websecure"}
				router["tls"] = map[string]any{"certResolver": "le"}
			}
			routers[key] = router
			services[key] = map[string]any{
				"loadBalancer": map[string]any{
					"servers": []map[string]string{{"url": backend}},
				},
			}
		}
	})

	writeJSON(w, map[string]any{
		"http": map[string]any{"routers": routers, "services": services},
	})
}
