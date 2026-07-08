package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

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
	d := &types.Domain{ID: NewID(), Name: name, CreatedAt: time.Now()}
	a.Store.Mutate(func() { a.Store.Domains[d.ID] = d })
	a.Store.AddEvent("domain", "Domain added: "+name, "", "")
	writeJSON(w, d)
}

func (a *API) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.Store.Mutate(func() { delete(a.Store.Domains, id) })
	w.WriteHeader(http.StatusNoContent)
}

// ---- Ingress setup ----

// handleGetIngress returns the current edge configuration.
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
	var node *types.Node
	a.Store.View(func() { node = a.Store.Nodes[cfg.NodeID] })
	if cfg.Enabled && node == nil {
		http.Error(w, "ingress node not found", http.StatusBadRequest)
		return
	}
	a.Store.Mutate(func() { a.Store.Ingress = cfg })

	if cfg.Enabled {
		a.deployTraefik(cfg)
		a.Store.AddEvent("ingress", fmt.Sprintf("Ingress enabled on %s (Traefik :%d/:%d)", node.Name, cfg.HTTPPort, cfg.HTTPSPort), cfg.NodeID, "")
	} else {
		// Tear down.
		payload, _ := json.Marshal(DeployPayload{ServiceID: "ingress-traefik", Name: "ingress-traefik"})
		a.Store.EnqueueTask(cfg.NodeID, types.AgentTask{
			ID: NewID(), Kind: types.TaskRemove, ServiceID: "ingress-traefik",
			Payload: string(payload), CreatedAt: time.Now(),
		})
		a.Store.AddEvent("ingress", "Ingress disabled", cfg.NodeID, "")
	}
	writeJSON(w, cfg)
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
			"--certificatesresolvers.le.acme.httpchallenge.entrypoint=web",
		)
	}
	payload, _ := json.Marshal(DeployPayload{
		ServiceID: "ingress-traefik",
		Name:      "ingress-traefik",
		Image:     "traefik:v3.1",
		Command:   strings.Join(cmd, " "),
		Volumes:   []string{"/data"},
		HostNet:   true,
	})
	a.Store.EnqueueTask(cfg.NodeID, types.AgentTask{
		ID: NewID(), Kind: types.TaskDeploy, ServiceID: "ingress-traefik",
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
// hosted on a different node. Relay ports are allocated 42000+.
func (a *API) relaySpecsFor(nodeID string) []types.RelaySpec {
	var specs []types.RelaySpec
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
		next := 42000
		alloc := func() int {
			for used[next] {
				next++
			}
			used[next] = true
			return next
		}
		for _, svc := range a.Store.Services {
			if svc.Domain == "" || svc.Port == 0 || svc.NodeID == nodeID {
				continue
			}
			node := a.Store.Nodes[svc.NodeID]
			if node == nil || node.MeshIP == "" {
				continue
			}
			if svc.RelayPort == 0 {
				svc.RelayPort = alloc()
			}
			// Hub relays spoke-to-spoke TCP: open a hub-side mesh listener
			// that dials the target node, and point the ingress agent at it.
			if a.Mesh != nil {
				_ = a.Mesh.Net.ProxyMesh(svc.RelayPort, fmt.Sprintf("%s:%d", node.MeshIP, svc.Port))
			}
			specs = append(specs, types.RelaySpec{
				LocalPort:  svc.RelayPort,
				TargetAddr: fmt.Sprintf("10.77.0.1:%d", svc.RelayPort),
			})
		}
	})
	return specs
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
	a.Store.Mutate(func() {
		svc.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
		svc.Ingress = req.Ingress
		if svc.Domain == "" {
			svc.Ingress = types.IngressNone
			svc.RelayPort = 0
		}
		svc.UpdatedAt = time.Now()
	})
	if svc.Domain != "" {
		a.Store.AddEvent("ingress", fmt.Sprintf("%s routed to %s", svc.Name, svc.Domain), svc.NodeID, svc.ID)
	}
	a.Hub.Broadcast("service_update", svc)
	writeJSON(w, svc)
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
				"rule":    fmt.Sprintf("Host(`%s`)", svc.Domain),
				"service": key,
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
