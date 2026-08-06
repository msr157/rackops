package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rackops/rackops/internal/types"
)

// edgeProbeHost is a synthetic hostname the Traefik config routes back to the
// control plane. Probing the ingress IP with this Host header proves OUR edge
// is what answers there (vs. another web server squatting on the port).
const edgeProbeHost = "edge.rackops.internal"

// ---- Verification engine ----

// RunDomainVerifier re-checks domains in the background: every 30s while a
// domain is unverified (catches DNS propagation fast after setup), hourly once
// verified (catches silent breakage — changed records, new ISP IP). Under HA
// only the leader verifies; results replicate through normal state sync.
func (a *API) RunDomainVerifier() {
	for range time.Tick(30 * time.Second) {
		if a.Cluster != nil && !a.Cluster.IsLeader() {
			continue
		}
		var due []string
		a.Store.View(func() {
			for id, d := range a.Store.Domains {
				if d.Status != types.DomainVerified || time.Since(d.LastChecked) >= time.Hour {
					due = append(due, id)
				}
			}
		})
		for _, id := range due {
			a.verifyDomain(id)
		}
	}
}

// verifyDomain runs the full check pipeline for one domain and persists the
// outcome: DNS resolution vs the ingress IP, an edge HTTP probe, and a TLS
// certificate probe. Safe to call concurrently with handlers.
func (a *API) verifyDomain(id string) *types.Domain {
	// Snapshot inputs.
	var d types.Domain
	var found bool
	var ingress types.IngressConfig
	var expectedIP, edgeLANIP string
	a.Store.View(func() {
		if dom := a.Store.Domains[id]; dom != nil {
			d, found = *dom, true
		}
		ingress = a.Store.Ingress
		if n := a.Store.Nodes[ingress.NodeID]; n != nil {
			expectedIP = firstNonEmpty(n.PublicIP, n.IP)
			edgeLANIP = n.IP
		}
	})
	if !found {
		return nil
	}

	// The name to resolve: wildcards are checked via a random probe label so
	// we validate the *.base record rather than the literal "*." name.
	checkName := d.Name
	if d.Wildcard {
		checkName = "rk-probe-" + NewID()[:6] + "." + d.WildcardBase()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	resolved, lookupErr := net.DefaultResolver.LookupHost(ctx, checkName)

	// Edge probes. The strongest validation is proving OUR Traefik answers at
	// the address the domain actually resolves to — that's the exact path a
	// visitor's browser takes. Falls back to the edge's known IPs to at least
	// distinguish "edge down" from "DNS points elsewhere".
	edgeAtResolved := false
	edgeAlive := false
	if ingress.Enabled && ingress.Status == "running" {
		for _, ip := range resolved {
			if probeEdge(ip, ingress) {
				edgeAtResolved, edgeAlive = true, true
				break
			}
		}
		if !edgeAtResolved {
			for _, ip := range uniqueNonEmpty(expectedIP, edgeLANIP) {
				if probeEdge(ip, ingress) {
					edgeAlive = true
					break
				}
			}
		}
	}

	status := types.DomainPending
	msg := ""
	switch {
	case expectedIP == "":
		msg = "enable the edge (ingress) on a node first — that node's IP is what the DNS record must point at"
	case lookupErr != nil:
		msg = fmt.Sprintf("not resolving yet — create the DNS record (A: %s → %s) at your registrar; propagation can take up to an hour", recordHost(d.Name), expectedIP)
	case containsIP(resolved, expectedIP):
		status = types.DomainVerified
		if !edgeAtResolved && edgeAlive {
			msg = fmt.Sprintf("DNS is correct but the edge did not answer on %s:%d from here — if this machine is inside the same NAT that can be hairpin routing; test from outside", expectedIP, ingress.HTTPPort)
		} else if !edgeAlive && ingress.Enabled {
			msg = fmt.Sprintf("DNS is correct but the edge did not answer on port %d — check port forwarding / firewall (another server may hold the port)", ingress.HTTPPort)
		}
	case edgeAtResolved && allPrivateIPs(resolved):
		// Resolves to a private address where our edge demonstrably answers
		// (typical nip.io testing) — valid on the LAN, honest about the rest.
		status = types.DomainVerified
		msg = fmt.Sprintf("resolves to a LAN address (%s) where your edge answers — works inside your network, but NOT from the internet (point it at %s for that)", strings.Join(resolved, ", "), expectedIP)
	case edgeAtResolved:
		// Different public IP than we predicted, but it reaches our edge
		// (multi-homed hosts, CDNs, secondary IPs) — trust the probe.
		status = types.DomainVerified
		msg = fmt.Sprintf("resolves to %s, which reaches your edge", strings.Join(resolved, ", "))
	default:
		status = types.DomainMisconfigured
		msg = fmt.Sprintf("resolves to %s — expected %s (fix the A record at your registrar)", strings.Join(resolved, ", "), expectedIP)
	}
	edgeOK := edgeAtResolved || (len(resolved) == 0 && edgeAlive)

	// Cert probe (exact domains only): wildcards get per-subdomain certs when
	// each subdomain is first routed, so there is nothing to probe on the base.
	certOK := false
	certIssuer := ""
	var certExpiry time.Time
	if !d.Wildcard && ingress.Enabled && ingress.ACMEEmail != "" {
		for _, ip := range uniqueNonEmpty(expectedIP, edgeLANIP) {
			if ok, issuer, expiry := probeCert(ip, ingress.HTTPSPort, d.Name); ok {
				certOK, certIssuer, certExpiry = true, issuer, expiry
				break
			} else if issuer != "" {
				certIssuer = issuer // e.g. "TRAEFIK DEFAULT CERT" — informative even when not OK
			}
		}
	}

	var out *types.Domain
	a.Store.Mutate(func() {
		dom := a.Store.Domains[id]
		if dom == nil {
			return
		}
		dom.Status = status
		dom.Message = msg
		dom.ExpectedIP = expectedIP
		dom.ResolvedIPs = resolved
		dom.EdgeOK = edgeOK
		dom.CertOK = certOK
		dom.CertIssuer = certIssuer
		dom.CertExpiry = certExpiry
		dom.LastChecked = time.Now()
		out = dom
	})
	if out != nil {
		a.Hub.Broadcast("domain_update", out)
	}
	return out
}

// probeEdge requests the edge-probe route with the synthetic Host header.
// Only OUR Traefik (which routes edgeProbeHost back to the control plane)
// returns the marker — Apache/nginx/anything else squatting on the port won't.
// With ACME on, plain HTTP is redirected, so HTTPS is probed as the fallback.
func probeEdge(ip string, ingress types.IngressConfig) bool {
	try := func(scheme string, port int) bool {
		client := &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				// The probe host is synthetic — Traefik serves its default
				// self-signed cert for it; we only care about the body marker.
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: edgeProbeHost},
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		req, err := http.NewRequest("GET", fmt.Sprintf("%s://%s:%d/api/edge-probe", scheme, ip, port), nil)
		if err != nil {
			return false
		}
		req.Host = edgeProbeHost
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		buf := make([]byte, 128)
		n, _ := resp.Body.Read(buf)
		return resp.StatusCode == http.StatusOK && strings.Contains(string(buf[:n]), `"edge-ok"`)
	}
	if try("http", ingress.HTTPPort) {
		return true
	}
	return ingress.ACMEEmail != "" && try("https", ingress.HTTPSPort)
}

// probeCert TLS-handshakes the edge with the domain's SNI and inspects the
// served certificate — exactly what a browser sees. Returns ok only for a
// currently-valid cert that matches the name and isn't Traefik's self-signed
// placeholder.
func probeCert(ip string, port int, domain string) (ok bool, issuer string, expiry time.Time) {
	d := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(d, "tcp", fmt.Sprintf("%s:%d", ip, port), &tls.Config{
		ServerName:         domain,
		InsecureSkipVerify: true, // we inspect manually — self-signed must not abort the handshake
	})
	if err != nil {
		return false, "", time.Time{}
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return false, "", time.Time{}
	}
	cert := certs[0]
	issuer = cert.Issuer.CommonName
	if issuer == "" && len(cert.Issuer.Organization) > 0 {
		issuer = cert.Issuer.Organization[0]
	}
	expiry = cert.NotAfter
	if strings.Contains(strings.ToUpper(issuer), "TRAEFIK") || strings.Contains(strings.ToUpper(cert.Subject.CommonName), "TRAEFIK") {
		return false, issuer, expiry // placeholder cert = ACME hasn't issued yet
	}
	if err := cert.VerifyHostname(domain); err != nil {
		return false, issuer, expiry
	}
	return time.Now().Before(cert.NotAfter), issuer, expiry
}

// handleVerifyDomain re-checks one domain on demand.
func (a *API) handleVerifyDomain(w http.ResponseWriter, r *http.Request) {
	d := a.verifyDomain(r.PathValue("id"))
	if d == nil {
		http.Error(w, "domain not found", http.StatusNotFound)
		return
	}
	writeJSON(w, d)
}

// handleEdgeProbe is the open endpoint the edge's probe router targets.
func (a *API) handleEdgeProbe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"rackops": "edge-ok"})
}

// ---- Network diagnosis (public access wizard) ----

// handleNetworkDiagnosis reports how (or whether) this deployment can be
// reached from the internet, powering the setup wizard's guidance.
func (a *API) handleNetworkDiagnosis(w http.ResponseWriter, r *http.Request) {
	type nodeDiag struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		LanIP    string `json:"lanIp"`
		PublicIP string `json:"publicIp,omitempty"`
		// direct = machine holds the public IP itself (cloud VPS);
		// nat = behind a router (port forwarding may work, CGNAT won't);
		// unknown = public IP detection hasn't completed/failed.
		Reachability string `json:"reachability"`
	}
	var out struct {
		EdgeEnabled bool       `json:"edgeEnabled"`
		EdgeStatus  string     `json:"edgeStatus,omitempty"`
		EdgeNodeID  string     `json:"edgeNodeId,omitempty"`
		HasDirectIP bool       `json:"hasDirectIp"` // some node IS publicly addressed
		LanOnly     bool       `json:"lanOnly"`     // no node has a confirmed public path
		Nodes       []nodeDiag `json:"nodes"`
		MeshEnabled bool       `json:"meshEnabled"`
	}
	a.Store.View(func() {
		out.EdgeEnabled = a.Store.Ingress.Enabled
		out.EdgeStatus = a.Store.Ingress.Status
		out.EdgeNodeID = a.Store.Ingress.NodeID
		for _, n := range a.Store.Nodes {
			nd := nodeDiag{ID: n.ID, Name: n.Name, LanIP: n.IP, PublicIP: n.PublicIP}
			switch {
			case n.PublicIP != "" && n.PublicIP == n.IP:
				nd.Reachability = "direct"
				out.HasDirectIP = true
			case n.PublicIP != "":
				nd.Reachability = "nat"
			default:
				nd.Reachability = "unknown"
			}
			out.Nodes = append(out.Nodes, nd)
		}
	})
	out.LanOnly = !out.HasDirectIP
	out.MeshEnabled = a.Mesh != nil
	// No join command here on purpose: the VPS-anchor path stands up a FRESH
	// control plane, whose join token this server cannot know. The wizard
	// sends people to that dashboard's own Add-machine dialog instead.
	writeJSON(w, out)
}

// ---- helpers ----

// recordHost is the "host" field value for the registrar's DNS form:
// "app.example.com" → "app", "*.example.com" → "*", "example.com" → "@".
func recordHost(domain string) string {
	base := strings.TrimPrefix(domain, "*.")
	parts := strings.Split(base, ".")
	if len(parts) <= 2 {
		if strings.HasPrefix(domain, "*.") {
			return "*"
		}
		return "@"
	}
	host := strings.Join(parts[:len(parts)-2], ".")
	if strings.HasPrefix(domain, "*.") {
		return "*." + host
	}
	return host
}

func containsIP(ips []string, want string) bool {
	for _, ip := range ips {
		if ip == want {
			return true
		}
	}
	return false
}

// allPrivateIPs reports whether every address is RFC1918/loopback/link-local.
func allPrivateIPs(ips []string) bool {
	if len(ips) == 0 {
		return false
	}
	for _, s := range ips {
		ip := net.ParseIP(s)
		if ip == nil || !(ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
			return false
		}
	}
	return true
}

func uniqueNonEmpty(vals ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range vals {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
