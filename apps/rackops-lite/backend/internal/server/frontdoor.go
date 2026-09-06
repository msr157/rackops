package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// Front Door: the control-plane half of RackOps' own tunnel service.
//
// The shape mirrors the ingress edge it sits in front of. Turning Front Door
// on for a service assigns it a vendor hostname and sets svc.Domain, so the
// hostname flows into the existing Traefik configuration as an ordinary
// router. The tunnel then delivers public traffic to that same Traefik. One
// routing table, one place where "which app answers this hostname" is
// decided, and per-app middlewares and health checks come along for free.
//
// Two things here are deliberate departures from the quick-tunnel code this
// supersedes. Every handler is org-scoped and permission-checked — the
// product audit found the quick-tunnel handlers fetched services by raw ID
// with no ownership check, letting any logged-in user tunnel another org's
// app. And enrollment is idempotent, so a re-enroll after a key rotation
// converges rather than erroring.

// frontDoorEnrollTimeout bounds calls to the vendor edge's admin API. The
// user is waiting on this, so it fails fast and says why.
const frontDoorEnrollTimeout = 15 * time.Second

// handleGetFrontDoor returns the install-wide Front Door configuration.
func (a *API) handleGetFrontDoor(w http.ResponseWriter, r *http.Request) {
	org, _ := a.requireOrg(w, r)
	if org == nil {
		return
	}
	var cfg types.FrontDoorConfig
	a.Store.View(func() { cfg = a.Store.FrontDoor })
	writeJSON(w, cfg)
}

// handleSetFrontDoor enables or disables Front Door for the install.
//
// Enabling is the "no hassle" path: if no ingress edge is running, one is
// started automatically on the best-placed node, because requiring the user
// to go set up Traefik first is exactly the friction this feature exists to
// delete.
func (a *API) handleSetFrontDoor(w http.ResponseWriter, r *http.Request) {
	org, member := a.requireOrg(w, r)
	if org == nil {
		return
	}
	if !orgCan(member, func(p types.OrgPermissions) bool { return p.ManageDomains }) {
		http.Error(w, "you don't have permission to manage public access in this organization", http.StatusForbidden)
		return
	}
	var req struct {
		Enabled   bool   `json:"enabled"`
		Workspace string `json:"workspace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if !req.Enabled {
		a.Store.Mutate(func() {
			a.Store.FrontDoor.Enabled = false
			a.Store.FrontDoor.Status = types.FrontDoorOff
			a.Store.FrontDoor.StatusMsg = ""
			a.Store.FrontDoor.LastChange = time.Now()
		})
		a.Store.AddEvent("frontdoor", "Front Door turned off", "", "")
		var cfg types.FrontDoorConfig
		a.Store.View(func() { cfg = a.Store.FrontDoor })
		a.Hub.Broadcast("frontdoor_update", cfg)
		writeJSON(w, cfg)
		return
	}

	if a.FrontDoorPOP == "" || a.FrontDoorAdmin == "" {
		http.Error(w, "this install is not configured for RackOps Front Door — set --frontdoor-pop and --frontdoor-admin on the server", http.StatusConflict)
		return
	}

	workspace := sanitizeName(req.Workspace)
	if workspace == "" || workspace == "app" {
		http.Error(w, "choose a workspace name: it becomes part of every public address", http.StatusBadRequest)
		return
	}

	// Front Door rides the ingress edge, so make sure one exists. Doing this
	// implicitly is the difference between one click and a detour through
	// another page.
	nodeID, err := a.ensureIngressForFrontDoor()
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	a.Store.Mutate(func() {
		fd := &a.Store.FrontDoor
		fd.Enabled = true
		fd.Workspace = workspace
		fd.POPAddr = a.FrontDoorPOP
		fd.Zone = a.FrontDoorZone
		fd.NodeID = nodeID
		if fd.TenantID == "" {
			fd.TenantID = "inst-" + NewID()
		}
		fd.Status = types.FrontDoorConnecting
		fd.StatusMsg = "enrolling with the RackOps edge…"
		fd.LastChange = time.Now()
	})

	// Enrollment needs the node's public key, which arrives on the agent's
	// report. Rather than block the request on that round trip, register
	// what we can now; the reconciler re-enrolls once the key is known.
	if err := a.enrollFrontDoor(); err != nil {
		a.Store.Mutate(func() {
			a.Store.FrontDoor.StatusMsg = err.Error()
		})
	}

	var cfg types.FrontDoorConfig
	a.Store.View(func() { cfg = a.Store.FrontDoor })
	a.Store.AddEvent("frontdoor", "Front Door turned on for workspace "+workspace, nodeID, "")
	a.Hub.Broadcast("frontdoor_update", cfg)
	writeJSON(w, cfg)
}

// ensureIngressForFrontDoor guarantees a Traefik edge is running and returns
// the node it runs on, deploying one if needed.
//
// Front Door does not need the edge node to have a public IP — that is the
// whole point — so node selection only requires a node that is online.
func (a *API) ensureIngressForFrontDoor() (string, error) {
	var cfg types.IngressConfig
	var candidate string
	a.Store.View(func() {
		cfg = a.Store.Ingress
		if cfg.Enabled && cfg.NodeID != "" {
			if n := a.Store.Nodes[cfg.NodeID]; n != nil && n.Status == types.NodeOnline {
				candidate = cfg.NodeID
			}
		}
		if candidate == "" {
			// Prefer a node that already has a public IP (it may also serve
			// direct traffic), then any online node.
			for _, n := range a.Store.Nodes {
				if n.Status != types.NodeOnline {
					continue
				}
				if candidate == "" || (n.HasPublic && !a.Store.Nodes[candidate].HasPublic) {
					candidate = n.ID
				}
			}
		}
	})
	if candidate == "" {
		return "", fmt.Errorf("no machine is online to run the edge proxy — add or start a machine first")
	}
	if cfg.Enabled && cfg.NodeID == candidate && cfg.Status == "running" {
		return candidate, nil
	}

	// Deploy (or move) the edge. Ports default to the standard pair; the
	// tunnel reaches Traefik over loopback, so these need not be reachable
	// from the internet.
	newCfg := cfg
	newCfg.Enabled = true
	newCfg.NodeID = candidate
	if newCfg.HTTPPort == 0 {
		newCfg.HTTPPort = 80
	}
	if newCfg.HTTPSPort == 0 {
		newCfg.HTTPSPort = 443
	}
	newCfg.Status = "pending"
	newCfg.StatusMsg = "starting the edge proxy for Front Door…"
	newCfg.LastDeploy = time.Now()
	a.Store.Mutate(func() { a.Store.Ingress = newCfg })
	a.deployTraefik(newCfg)
	a.Hub.Broadcast("ingress_update", newCfg)
	return candidate, nil
}

// handleEnableServiceFrontDoor gives one service a public Front Door URL.
func (a *API) handleEnableServiceFrontDoor(w http.ResponseWriter, r *http.Request) {
	svc, _, member := a.serviceForOrg(w, r)
	if svc == nil {
		return
	}
	if !orgCan(member, func(p types.OrgPermissions) bool { return p.ManageDomains }) {
		http.Error(w, "you don't have permission to manage public access in this organization", http.StatusForbidden)
		return
	}
	if svc.Port == 0 {
		http.Error(w, "this app publishes no HTTP port to expose", http.StatusBadRequest)
		return
	}

	var cfg types.FrontDoorConfig
	a.Store.View(func() { cfg = a.Store.FrontDoor })
	if !cfg.Enabled {
		http.Error(w, "turn Front Door on first — it's on the Network page", http.StatusConflict)
		return
	}

	host := frontDoorHostname(svc.Name, cfg.Workspace, cfg.Zone)
	if taken := a.hostnameOwner(host); taken != "" && taken != svc.ID {
		http.Error(w, "another app in this install already uses that public address — rename this app first", http.StatusConflict)
		return
	}

	a.Store.Mutate(func() {
		svc.Domain = host
		svc.FrontDoor = true
		if svc.NodeID == a.Store.Ingress.NodeID {
			svc.Ingress = types.IngressStatic
		} else {
			svc.Ingress = types.IngressRelay
		}
		svc.UpdatedAt = time.Now()
	})

	if err := a.enrollFrontDoor(); err != nil {
		// The route is recorded either way; the reconciler retries
		// enrollment. Say so rather than implying the URL is dead.
		a.Store.AddEvent("frontdoor", fmt.Sprintf("%s will be public at %s once the edge accepts it (%v)", svc.Name, host, err), svc.NodeID, svc.ID)
	} else {
		a.Store.AddEvent("frontdoor", fmt.Sprintf("%s is public at https://%s", svc.Name, host), svc.NodeID, svc.ID)
	}
	a.serviceChanged(svc)
	writeJSON(w, map[string]string{"domain": host, "url": "https://" + host})
}

// handleDisableServiceFrontDoor removes a service's public Front Door URL.
func (a *API) handleDisableServiceFrontDoor(w http.ResponseWriter, r *http.Request) {
	svc, _, member := a.serviceForOrg(w, r)
	if svc == nil {
		return
	}
	if !orgCan(member, func(p types.OrgPermissions) bool { return p.ManageDomains }) {
		http.Error(w, "you don't have permission to manage public access in this organization", http.StatusForbidden)
		return
	}
	a.Store.Mutate(func() {
		svc.FrontDoor = false
		svc.Domain = ""
		svc.Ingress = types.IngressNone
		svc.UpdatedAt = time.Now()
	})
	if err := a.enrollFrontDoor(); err != nil {
		a.Store.AddEvent("frontdoor", "Removed a public address, but the edge did not confirm: "+err.Error(), svc.NodeID, svc.ID)
	}
	a.Store.AddEvent("frontdoor", svc.Name+" is no longer public", svc.NodeID, svc.ID)
	a.serviceChanged(svc)
	w.WriteHeader(http.StatusNoContent)
}

// hostnameOwner returns the service ID already using a Front Door hostname.
func (a *API) hostnameOwner(host string) string {
	var owner string
	a.Store.View(func() {
		for _, s := range a.Store.Services {
			if s.FrontDoor && strings.EqualFold(s.Domain, host) {
				owner = s.ID
				return
			}
		}
	})
	return owner
}

// frontDoorHostname builds the public name for an app.
func frontDoorHostname(appName, workspace, zone string) string {
	return fmt.Sprintf("%s-%s.%s", sanitizeName(appName), workspace, zone)
}

// frontDoorHostnames lists every hostname this install currently serves.
func (a *API) frontDoorHostnames() []string {
	var out []string
	a.Store.View(func() {
		for _, s := range a.Store.Services {
			if s.FrontDoor && s.Domain != "" {
				out = append(out, s.Domain)
			}
		}
	})
	return out
}

// enrollFrontDoor registers this install and its hostnames with the vendor
// edge. It is idempotent, so it is safe to call on every change and from the
// reconciler.
func (a *API) enrollFrontDoor() error {
	var cfg types.FrontDoorConfig
	var publicKey string
	a.Store.View(func() {
		cfg = a.Store.FrontDoor
		if n := a.Store.Nodes[cfg.NodeID]; n != nil {
			publicKey = n.FrontDoorKey
		}
	})
	if !cfg.Enabled || cfg.TenantID == "" {
		return nil
	}
	if publicKey == "" {
		// The tunnel node has not reported its identity yet. This resolves
		// itself within one report interval.
		return fmt.Errorf("waiting for %s to report its Front Door key", nodeLabel(a, cfg.NodeID))
	}
	if a.FrontDoorAdmin == "" {
		return fmt.Errorf("no Front Door edge is configured on this server")
	}

	body, _ := json.Marshal(map[string]any{
		"publicKey": publicKey,
		"workspace": cfg.Workspace,
		"hostnames": a.frontDoorHostnames(),
	})
	req, err := http.NewRequest(http.MethodPut,
		strings.TrimSuffix(a.FrontDoorAdmin, "/")+"/enroll/v1/tenants/"+cfg.TenantID,
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not reach the RackOps edge: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.FrontDoorToken)

	client := &http.Client{Timeout: frontDoorEnrollTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the RackOps edge: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := readAllLimited(resp.Body, 512)
		return fmt.Errorf("the RackOps edge refused the registration: %s", strings.TrimSpace(msg))
	}
	a.Store.Mutate(func() { a.Store.FrontDoor.StatusMsg = "" })
	return nil
}

// applyFrontDoorReport folds an agent's reported tunnel state into the
// store, so the console reads one place instead of joining node records.
func (a *API) applyFrontDoorReport(nodeID string, st *types.FrontDoorState) {
	if st == nil {
		return
	}
	var changed bool
	var cfg types.FrontDoorConfig
	a.Store.Mutate(func() {
		// Record the node's identity regardless of whether Front Door is on:
		// it is what enrollment needs, and capturing it early means turning
		// the feature on does not wait a full report interval.
		if n := a.Store.Nodes[nodeID]; n != nil && n.FrontDoorKey != st.PublicKey {
			n.FrontDoorKey = st.PublicKey
			changed = true
		}
		fd := &a.Store.FrontDoor
		if fd.NodeID != nodeID {
			return // another node's report; it holds no tunnel
		}
		if fd.Status != st.Status || fd.StatusMsg != st.Message {
			changed = true
		}
		fd.Status = st.Status
		fd.StatusMsg = st.Message
		fd.POPID = st.POPID
		fd.Transport = st.Transport
		fd.UsedBytes = st.UsedBytes
		fd.QuotaBytes = st.QuotaBytes
		fd.ConnectedAt = st.ConnectedAt
		cfg = *fd
	})
	if changed && cfg.Enabled {
		a.Hub.Broadcast("frontdoor_update", cfg)
	}
}

// frontDoorSpecFor builds the assignment for a node's next agent response.
// It returns nil for every node except the one holding the tunnel, so no
// other machine tries to open one.
func (a *API) frontDoorSpecFor(nodeID string) *types.FrontDoorSpec {
	var spec *types.FrontDoorSpec
	a.Store.View(func() {
		fd := a.Store.FrontDoor
		if !fd.Enabled || fd.NodeID != nodeID || fd.TenantID == "" || fd.POPAddr == "" {
			return
		}
		var hosts []string
		for _, s := range a.Store.Services {
			if s.FrontDoor && s.Domain != "" {
				hosts = append(hosts, s.Domain)
			}
		}
		spec = &types.FrontDoorSpec{
			Enabled:   true,
			POPAddr:   fd.POPAddr,
			TenantID:  fd.TenantID,
			Hostnames: hosts,
			// Traefik runs with host networking on the tunnel node, so its
			// dedicated Front Door entrypoint is reachable on loopback.
			LocalTarget: fmt.Sprintf("127.0.0.1:%d", types.FrontDoorEntryPort),
		}
	})
	if spec != nil {
		sortStrings(spec.Hostnames)
	}
	return spec
}

// RunFrontDoorReconciler keeps the vendor edge's view of this install in
// step with the store.
//
// Enrollment happens inline on every change, but changes can fail: the edge
// may be briefly unreachable, or the tunnel node may not have reported its
// key yet. Without this loop, a transient failure at the wrong moment would
// leave an app with a URL that never starts working, and nothing would ever
// retry. It is leader-only under HA for the same reason the domain verifier
// is: two servers enrolling the same tenant would fight.
func (a *API) RunFrontDoorReconciler() {
	// Enrollment is cheap and idempotent; a minute is fast enough to fix a
	// blip and slow enough to be invisible in the edge's logs.
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	var lastHosts string
	for range t.C {
		if a.Cluster != nil && !a.Cluster.IsLeader() {
			continue
		}
		var enabled bool
		a.Store.View(func() { enabled = a.Store.FrontDoor.Enabled })
		if !enabled {
			continue
		}
		// Re-enroll when the hostname set changed, or when the last attempt
		// left a status message behind (meaning it did not succeed).
		hosts := strings.Join(a.frontDoorHostnames(), ",")
		var stale bool
		a.Store.View(func() { stale = a.Store.FrontDoor.StatusMsg != "" })
		if hosts == lastHosts && !stale {
			continue
		}
		if err := a.enrollFrontDoor(); err != nil {
			a.Store.Mutate(func() { a.Store.FrontDoor.StatusMsg = err.Error() })
			continue
		}
		lastHosts = hosts
	}
}

// nodeLabel returns a node's display name for messages, falling back to its
// ID so an error is never about "node ".
func nodeLabel(a *API, nodeID string) string {
	var name string
	a.Store.View(func() {
		if n := a.Store.Nodes[nodeID]; n != nil {
			name = n.Name
		}
	})
	if name == "" {
		return "this machine"
	}
	return name
}

// readAllLimited reads at most n bytes, for turning an error response body
// into a message without trusting its length.
func readAllLimited(r interface{ Read([]byte) (int, error) }, n int64) (string, error) {
	buf := make([]byte, n)
	read, err := r.Read(buf)
	if read > 0 {
		return string(buf[:read]), nil
	}
	return "", err
}

// sortStrings sorts in place; the spec's hostname order must be stable so
// the agent's Equal check does not see a change that is not one.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
