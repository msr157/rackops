package server

import (
	"embed"
	_ "embed"
	"net/http"
	"sort"
	"strings"
)

// The OpenAPI document is generated from apiRoutes — the SAME table Router()
// registers handlers from. A route cannot exist without appearing in the docs
// and vice versa; the two can't drift because there is only one source.

// route is one documented API endpoint.
type route struct {
	method  string
	path    string // net/http pattern, e.g. /api/services/{id}
	tag     string
	summary string
	handler http.HandlerFunc
	open    bool // reachable without auth (mirrors authMiddleware's open list)
}

// apiRoutes builds the full route table. Handlers close over the API value.
func (a *API) apiRoutes() []route {
	return []route{
		// Agent protocol
		{"POST", "/api/agent/ingest", "agent", "Agent telemetry ingest: report metrics/containers, receive queued tasks (join-token auth)", a.handleIngest, true},
		{"POST", "/api/agent/logstream", "agent", "Agent log-chunk push for live log streaming (join-token auth)", a.handleLogStream, true},

		// Fleet
		{"GET", "/api/nodes", "fleet", "List the active organization's machines", a.handleListNodes, false},
		{"GET", "/api/nodes/{id}", "fleet", "Get one machine", a.handleGetNode, false},
		{"GET", "/api/nodes/{id}/fs", "fleet", "Browse a machine's filesystem (for bind mounts)", a.handleNodeFS, false},
		{"GET", "/api/overview", "fleet", "Aggregated KPIs for the active organization", a.handleOverview, false},
		{"GET", "/api/metrics/history", "fleet", "Metric history for the fleet, a machine, or an app (?scope=fleet|node|service&id=&window=24h|7d)", a.handleMetricsHistory, false},
		{"GET", "/api/events", "fleet", "Event feed (org-scoped)", a.handleEvents, false},
		{"GET", "/api/alerts", "fleet", "Open alerts (use ?all=true to include resolved)", a.handleAlerts, false},
		{"GET", "/api/join-command", "fleet", "Command for joining a new machine to this organization", a.handleJoinCommand, false},

		// Projects / environments / services
		{"GET", "/api/projects", "apps", "List projects", a.handleListProjects, false},
		{"POST", "/api/projects", "apps", "Create a project (with production/staging/development envs)", a.handleCreateProject, false},
		{"GET", "/api/projects/{id}/envs", "apps", "List a project's environments", a.handleListEnvs, false},
		{"POST", "/api/projects/{id}/envs", "apps", "Add an environment to a project", a.handleCreateEnv, false},
		{"GET", "/api/services", "apps", "List services (filter: ?project= &env=)", a.handleListServices, false},
		{"POST", "/api/services", "apps", "Create + deploy a service (image/git/compose/template; nodeId \"auto\" places it)", a.handleCreateService, false},
		{"GET", "/api/services/{id}", "apps", "Get one service", a.handleGetService, false},
		{"PATCH", "/api/services/{id}", "apps", "Edit a service (redeploy=true applies immediately; node change migrates)", a.handleUpdateService, false},
		{"DELETE", "/api/services/{id}", "apps", "Remove a service and its container", a.handleDeleteService, false},
		{"POST", "/api/services/{id}/{action}", "apps", "Service action: start | stop | restart | redeploy", a.handleServiceAction, false},
		{"POST", "/api/services/{id}/clear-logs", "apps", "Hide log output before now (sets the log horizon)", a.handleClearLogs, false},
		{"GET", "/api/services/{id}/logs", "apps", "Fetch recent runtime logs from the machine", a.handleServiceLogs, false},
		{"POST", "/api/services/{id}/logs/stream", "apps", "Start a live log tail (chunks arrive on /api/stream)", a.handleStartLogStream, false},
		{"DELETE", "/api/services/{id}/logs/stream", "apps", "Stop a live log tail", a.handleStopLogStream, false},
		{"GET", "/api/services/{id}/deployments", "apps", "Deployment history with build logs", a.handleServiceDeployments, false},

		// Placement & templates
		{"POST", "/api/placement", "placement", "Score the org's machines for a workload (CPU/mem/thermal/storage/GPU)", a.handlePlacement, false},
		{"GET", "/api/suggest-port", "placement", "Suggest a free host port (?node= &port=preferred)", a.handleSuggestPort, false},
		{"GET", "/api/templates", "placement", "One-click template catalog", a.handleTemplates, false},

		// Networking
		{"GET", "/api/mesh", "network", "WireGuard mesh status", a.handleMeshStatus, false},
		{"GET", "/api/mesh/dial", "network", "Debug: dial a mesh address from the hub", a.handleMeshDial, false},
		{"GET", "/api/domains", "network", "List the org's domains with verification state", a.handleListDomains, false},
		{"POST", "/api/domains", "network", "Add a domain (wildcards: *.example.com)", a.handleCreateDomain, false},
		{"DELETE", "/api/domains/{id}", "network", "Remove a domain and detach its services", a.handleDeleteDomain, false},
		{"POST", "/api/domains/{id}/verify", "network", "Re-run DNS/edge/TLS verification now", a.handleVerifyDomain, false},
		{"GET", "/api/edge-probe", "network", "Edge reachability probe target (used during domain verification)", a.handleEdgeProbe, true},
		{"GET", "/api/network/diagnosis", "network", "Analyze public reachability (LAN-only? NAT?)", a.handleNetworkDiagnosis, false},
		{"GET", "/api/ingress", "network", "Edge proxy (Traefik) configuration + status", a.handleGetIngress, false},
		{"POST", "/api/ingress", "network", "Enable/disable/update the edge proxy", a.handleSetIngress, false},
		{"GET", "/api/traefik/config", "network", "Traefik dynamic config (consumed by the edge container)", a.handleTraefikConfig, true},
		{"POST", "/api/services/{id}/domain", "network", "Route a domain to a service", a.handleSetServiceDomain, false},
		{"POST", "/api/services/{id}/expose", "network", "Mint a zero-DNS nip.io URL for a service", a.handleExposeService, false},
		{"POST", "/api/services/{id}/quick-tunnel", "network", "Start a Cloudflare quick tunnel (testing)", a.handleStartQuickTunnel, false},
		{"DELETE", "/api/services/{id}/quick-tunnel", "network", "Stop the quick tunnel", a.handleStopQuickTunnel, false},
		{"GET", "/api/frontdoor", "network", "RackOps Front Door configuration + status", a.handleGetFrontDoor, false},
		{"POST", "/api/frontdoor", "network", "Turn RackOps Front Door on or off for this install", a.handleSetFrontDoor, false},
		{"POST", "/api/services/{id}/frontdoor", "network", "Publish an app at a permanent RackOps Front Door URL", a.handleEnableServiceFrontDoor, false},
		{"DELETE", "/api/services/{id}/frontdoor", "network", "Remove an app's Front Door URL", a.handleDisableServiceFrontDoor, false},

		// Auth & account
		{"GET", "/api/auth/status", "auth", "Setup state: does an owner account exist yet?", a.handleAuthStatus, true},
		{"POST", "/api/auth/signup", "auth", "Create an account (first becomes owner; later ones need admin auth)", a.handleSignup, true},
		{"POST", "/api/auth/login", "auth", "Sign in, returns a bearer token", a.handleLogin, true},
		{"GET", "/api/auth/me", "auth", "The authenticated account", a.handleMe, true},
		{"POST", "/api/auth/change-password", "auth", "Change the authenticated account's password", a.handleChangePassword, true},
		{"POST", "/api/auth/forgot-password", "auth", "Request a password-reset link (emailed + printed in the server log)", a.handleForgotPassword, true},
		{"POST", "/api/auth/reset-password", "auth", "Consume a reset token and set a new password", a.handleResetPassword, true},
		{"GET", "/api/auth/invitation/{token}", "auth", "Preview an invitation (email, organization, role)", a.handleGetInvitation, true},
		{"POST", "/api/auth/invitation", "auth", "Accept an invitation: create the account and join the organization", a.handleAcceptInvitation, true},
		{"GET", "/api/users", "auth", "List accounts (admin only)", a.handleListUsers, false},
		{"POST", "/api/tokens", "auth", "Create an API key (shown once; acts with your role)", a.handleCreateToken, false},
		{"GET", "/api/tokens", "auth", "List your API keys (admins: all keys)", a.handleListTokens, false},
		{"DELETE", "/api/tokens/{id}", "auth", "Revoke an API key (own keys; admins: any)", a.handleDeleteToken, false},

		// Organizations
		{"GET", "/api/orgs", "orgs", "List organizations you belong to", a.handleListOrgs, false},
		{"POST", "/api/orgs", "orgs", "Create an organization (platform admin)", a.handleCreateOrg, false},
		{"PATCH", "/api/orgs/{id}", "orgs", "Rename an organization", a.handleUpdateOrg, false},
		{"DELETE", "/api/orgs/{id}", "orgs", "Delete an empty organization (never the last one)", a.handleDeleteOrg, false},
		{"PATCH", "/api/orgs/{id}/members/{userId}", "orgs", "Change a member's role or permissions", a.handleSetOrgMember, false},
		{"DELETE", "/api/orgs/{id}/members/{userId}", "orgs", "Remove a member from the organization", a.handleRemoveOrgMember, false},
		{"POST", "/api/orgs/{id}/invitations", "orgs", "Create a 48h invitation link", a.handleCreateInvitation, false},
		{"GET", "/api/orgs/{id}/invitations", "orgs", "List pending invitations", a.handleListInvitations, false},
		{"DELETE", "/api/orgs/{id}/invitations/{invId}", "orgs", "Revoke an invitation", a.handleDeleteInvitation, false},

		// Settings & assistant
		{"GET", "/api/settings", "settings", "The active org's integrations settings (secrets masked)", a.handleGetSettings, false},
		{"POST", "/api/settings", "settings", "Update the active org's integrations settings", a.handleSetSettings, false},
		{"POST", "/api/assistant", "settings", "Ask the AI Co-Pilot (grounded in live fleet state)", a.handleAssistant, false},
		{"POST", "/api/assistant/test", "settings", "Validate the configured AI provider", a.handleAssistantTest, false},

		// Support
		{"GET", "/api/support/tickets", "support", "List your support tickets (admins: all of the install's)", a.handleListTickets, false},
		{"POST", "/api/support/tickets", "support", "Open a support ticket (forwarded to RackOps support)", a.handleCreateTicket, false},
		{"GET", "/api/support/tickets/{id}", "support", "Get one ticket's thread", a.handleGetTicket, false},
		{"POST", "/api/support/tickets/{id}/reply", "support", "Reply on a ticket", a.handleReplyTicket, false},
		{"POST", "/api/support/tickets/{id}/resolve", "support", "Mark a ticket resolved", a.handleResolveTicket, false},

		// Updates & cluster
		{"GET", "/api/version-check", "platform", "Current + latest version (server-side cached 6h)", a.handleVersionCheck, false},
		{"POST", "/api/update", "platform", "Self-update the server binary and restart (owner/admin session)", a.handleApplyUpdate, false},
		{"GET", "/api/cluster", "platform", "HA cluster status", a.handleClusterStatus, true},
		{"POST", "/api/cluster/join", "platform", "Join this server to an HA cluster (join-token auth)", a.handleClusterJoin, true},
	}
}

// handleOpenAPI serves the generated OpenAPI 3 document.
func (a *API) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}

	paths := map[string]map[string]any{}
	tagSet := map[string]bool{}
	for _, rt := range a.apiRoutes() {
		// net/http patterns are already OpenAPI-shaped, minus the {rest...} tail.
		p := strings.ReplaceAll(rt.path, "{rest...}", "{rest}")
		if paths[p] == nil {
			paths[p] = map[string]any{}
		}
		op := map[string]any{
			"tags":    []string{rt.tag},
			"summary": rt.summary,
			"responses": map[string]any{
				"200": map[string]any{"description": "Success"},
			},
		}
		if !rt.open {
			op["security"] = []map[string]any{{"apiKey": map[string]any{}}, {"bearer": map[string]any{}}}
		}
		// Path params from the pattern.
		var params []map[string]any
		for _, seg := range strings.Split(p, "/") {
			if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
				params = append(params, map[string]any{
					"name": strings.Trim(seg, "{}"), "in": "path", "required": true,
					"schema": map[string]string{"type": "string"},
				})
			}
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		paths[p][strings.ToLower(rt.method)] = op
		tagSet[rt.tag] = true
	}

	var tags []map[string]string
	for t := range tagSet {
		tags = append(tags, map[string]string{"name": t})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i]["name"] < tags[j]["name"] })

	doc := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "RackOps Lite API",
			"version":     ServerVersion,
			"description": "The complete control-plane API. Authenticate with an API key from Profile → API keys — send it as `X-Api-Key` or `Authorization: Bearer rk_…`. Keys act with their owner's role. Scope requests to an organization with the `X-Rackops-Org` header (defaults to your first organization).",
		},
		"servers":    []map[string]string{{"url": scheme + "://" + r.Host}},
		"tags":       tags,
		"paths":      paths,
		"components": map[string]any{"securitySchemes": securitySchemes},
	}
	writeJSON(w, doc)
}

var securitySchemes = map[string]any{
	"apiKey": map[string]any{"type": "apiKey", "in": "header", "name": "X-Api-Key"},
	"bearer": map[string]any{"type": "http", "scheme": "bearer"},
}

//go:embed swagger.html
var swaggerHTML []byte

// swaggerAssets embeds the swagger-ui dist bundle (vendored, no CDN — the
// explorer works on airgapped installs and can't be supply-chain'd at runtime).
//
//go:embed swaggerui
var swaggerAssets embed.FS

// handleSwagger serves the API explorer page.
func (a *API) handleSwagger(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(swaggerHTML)
}

// handleSwaggerAsset serves the embedded swagger-ui js/css.
func (a *API) handleSwaggerAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("asset")
	data, err := swaggerAssets.ReadFile("swaggerui/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(name, ".js"):
		w.Header().Set("Content-Type", "application/javascript")
	case strings.HasSuffix(name, ".css"):
		w.Header().Set("Content-Type", "text/css")
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
}
