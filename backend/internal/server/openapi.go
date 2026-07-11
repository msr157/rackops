package server

import (
	"net/http"
)

// Swagger/OpenAPI, Dokploy-style: a machine-readable spec at
// /api/openapi.json and interactive docs at /swagger. Auth uses the same
// API keys created in Profile → API keys (Authorization: Bearer rk_… or
// the x-api-key header).

type oaMap = map[string]any

func oaOp(tag, summary, desc string, extra oaMap) oaMap {
	op := oaMap{
		"tags":    []string{tag},
		"summary": summary,
		"responses": oaMap{
			"200": oaMap{"description": "OK"},
			"401": oaMap{"description": "Missing or invalid credentials"},
		},
	}
	if desc != "" {
		op["description"] = desc
	}
	for k, v := range extra {
		op[k] = v
	}
	return op
}

func idParam(name, desc string) oaMap {
	return oaMap{
		"name": name, "in": "path", "required": true,
		"description": desc, "schema": oaMap{"type": "string"},
	}
}

func jsonBody(props oaMap, required []string) oaMap {
	schema := oaMap{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return oaMap{
		"required": true,
		"content":  oaMap{"application/json": oaMap{"schema": schema}},
	}
}

func str(desc string) oaMap  { return oaMap{"type": "string", "description": desc} }
func num(desc string) oaMap  { return oaMap{"type": "integer", "description": desc} }
func boolT(desc string) oaMap { return oaMap{"type": "boolean", "description": desc} }

// openAPISpec builds the spec. Hand-maintained next to the routes it
// documents; kept to the endpoints external clients actually use.
func openAPISpec() oaMap {
	return oaMap{
		"openapi": "3.0.3",
		"info": oaMap{
			"title":       "RackOps API",
			"version":     ServerVersion,
			"description": "The RackOps control-plane REST API. Create an API key in the dashboard (Profile → API keys), then authenticate with `Authorization: Bearer rk_…` or the `x-api-key` header. Everything the dashboard does goes through this API — deploys, lifecycle, logs, fleet, domains.",
		},
		"servers": []oaMap{{"url": "/", "description": "This RackOps server"}},
		"components": oaMap{
			"securitySchemes": oaMap{
				"bearerAuth": oaMap{"type": "http", "scheme": "bearer", "description": "API key (rk_…) or session JWT"},
				"apiKeyAuth": oaMap{"type": "apiKey", "in": "header", "name": "x-api-key", "description": "API key (rk_…)"},
			},
		},
		"security": []oaMap{{"bearerAuth": []string{}}, {"apiKeyAuth": []string{}}},
		"tags": []oaMap{
			{"name": "Fleet", "description": "Machines, metrics, and cluster overview"},
			{"name": "Services", "description": "Apps: deploy, lifecycle, logs"},
			{"name": "Projects", "description": "Projects and environments"},
			{"name": "Organizations", "description": "Organisations and membership"},
			{"name": "Network", "description": "Edge proxy, domains, tunnels"},
			{"name": "Auth", "description": "Accounts, sessions, and API keys"},
			{"name": "Support", "description": "Support tickets"},
			{"name": "System", "description": "Version, updates, settings"},
		},
		"paths": oaMap{
			"/api/overview": oaMap{"get": oaOp("Fleet", "Cluster overview KPIs", "Machines online, apps running, memory/storage totals, open alerts.", nil)},
			"/api/nodes":    oaMap{"get": oaOp("Fleet", "List machines", "Every node with hardware inventory and live metrics.", nil)},
			"/api/nodes/{id}": oaMap{"get": oaOp("Fleet", "Get one machine", "", oaMap{
				"parameters": []oaMap{idParam("id", "Node ID")},
			})},
			"/api/alerts": oaMap{"get": oaOp("Fleet", "Open alerts", "Pass ?all=true to include resolved alerts.", nil)},
			"/api/events": oaMap{"get": oaOp("Fleet", "Activity feed", "Latest events, newest first.", nil)},

			"/api/services": oaMap{
				"get": oaOp("Services", "List services", "Filter with ?project= and ?env=.", nil),
				"post": oaOp("Services", "Deploy a service", "Deploy an image, git repo, compose stack, or template. nodeId \"auto\" lets the scheduler place it.", oaMap{
					"requestBody": jsonBody(oaMap{
						"name": str("Service name"), "image": str("Docker image (image deploys)"),
						"gitRepo": str("Git URL (git deploys)"), "templateId": str("Template ID (catalog deploys)"),
						"composeYml": str("docker-compose YAML (compose deploys)"),
						"port":       num("Container port the app listens on"),
						"nodeId":     str("Target node ID, or \"auto\""),
						"projectId":  str("Project ID"), "envId": str("Environment ID"),
					}, []string{"name"}),
				}),
			},
			"/api/services/{id}": oaMap{
				"get": oaOp("Services", "Get a service", "", oaMap{"parameters": []oaMap{idParam("id", "Service ID")}}),
				"patch": oaOp("Services", "Update a service", "Set redeploy=true to apply immediately.", oaMap{
					"parameters":  []oaMap{idParam("id", "Service ID")},
					"requestBody": jsonBody(oaMap{"image": str("New image"), "env": oaMap{"type": "array", "description": "Env vars", "items": oaMap{"type": "object"}}, "redeploy": boolT("Apply now")}, nil),
				}),
				"delete": oaOp("Services", "Remove a service", "Deletes the container; managed volume data stays on disk.", oaMap{"parameters": []oaMap{idParam("id", "Service ID")}}),
			},
			"/api/services/{id}/{action}": oaMap{"post": oaOp("Services", "Lifecycle action", "action is one of start, stop, restart, redeploy. Adopted apps reject redeploy (no build recipe) — use restart.", oaMap{
				"parameters": []oaMap{idParam("id", "Service ID"), idParam("action", "start | stop | restart | redeploy")},
			})},
			"/api/services/{id}/logs":        oaMap{"get": oaOp("Services", "Fetch runtime logs", "", oaMap{"parameters": []oaMap{idParam("id", "Service ID")}})},
			"/api/services/{id}/deployments": oaMap{"get": oaOp("Services", "Deployment history", "", oaMap{"parameters": []oaMap{idParam("id", "Service ID")}})},
			"/api/templates":                 oaMap{"get": oaOp("Services", "Template catalog", "45+ one-click apps.", nil)},

			"/api/projects": oaMap{
				"get": oaOp("Projects", "List projects", "Filter with ?org=.", nil),
				"post": oaOp("Projects", "Create a project", "", oaMap{
					"requestBody": jsonBody(oaMap{"name": str("Project name"), "description": str(""), "orgId": str("Owning organisation (default org if omitted)")}, []string{"name"}),
				}),
			},
			"/api/projects/{id}/envs": oaMap{
				"get":  oaOp("Projects", "List environments", "", oaMap{"parameters": []oaMap{idParam("id", "Project ID")}}),
				"post": oaOp("Projects", "Create an environment", "", oaMap{"parameters": []oaMap{idParam("id", "Project ID")}, "requestBody": jsonBody(oaMap{"name": str("Environment name")}, []string{"name"})}),
			},

			"/api/orgs": oaMap{
				"get":  oaOp("Organizations", "List organisations", "Platform admins see all; members see theirs.", nil),
				"post": oaOp("Organizations", "Create an organisation", "", oaMap{"requestBody": jsonBody(oaMap{"name": str("Organisation name")}, []string{"name"})}),
			},
			"/api/orgs/{id}": oaMap{
				"patch":  oaOp("Organizations", "Rename an organisation", "", oaMap{"parameters": []oaMap{idParam("id", "Organisation ID")}, "requestBody": jsonBody(oaMap{"name": str("New name")}, []string{"name"})}),
				"delete": oaOp("Organizations", "Delete an organisation", "Must be empty; the last organisation can't be deleted.", oaMap{"parameters": []oaMap{idParam("id", "Organisation ID")}}),
			},
			"/api/orgs/{id}/members": oaMap{"post": oaOp("Organizations", "Add or update a member", "", oaMap{
				"parameters":  []oaMap{idParam("id", "Organisation ID")},
				"requestBody": jsonBody(oaMap{"email": str("User's email"), "role": str("owner | admin | member")}, nil),
			})},
			"/api/orgs/{id}/members/{userId}": oaMap{"delete": oaOp("Organizations", "Remove a member", "", oaMap{
				"parameters": []oaMap{idParam("id", "Organisation ID"), idParam("userId", "User ID")},
			})},

			"/api/domains": oaMap{
				"get":  oaOp("Network", "List domains", "Includes DNS/edge/cert verification state.", nil),
				"post": oaOp("Network", "Add a domain", "Supports wildcards (*.example.com).", oaMap{"requestBody": jsonBody(oaMap{"name": str("Domain name")}, []string{"name"})}),
			},
			"/api/ingress": oaMap{
				"get":  oaOp("Network", "Edge proxy config & status", "", nil),
				"post": oaOp("Network", "Enable/update the edge proxy", "", oaMap{"requestBody": jsonBody(oaMap{"enabled": boolT(""), "nodeId": str("Node running Traefik"), "httpPort": num(""), "httpsPort": num(""), "acmeEmail": str("Set to enable Let's Encrypt")}, nil)}),
			},
			"/api/services/{id}/expose":       oaMap{"post": oaOp("Network", "Get an instant URL", "Assigns a nip.io URL through the edge.", oaMap{"parameters": []oaMap{idParam("id", "Service ID")}})},
			"/api/services/{id}/quick-tunnel": oaMap{"post": oaOp("Network", "Start a quick tunnel", "Temporary trycloudflare.com URL.", oaMap{"parameters": []oaMap{idParam("id", "Service ID")}})},

			"/api/auth/login": oaMap{"post": oaOp("Auth", "Sign in", "Returns a session JWT.", oaMap{
				"security":    []oaMap{},
				"requestBody": jsonBody(oaMap{"email": str(""), "password": str("")}, []string{"email", "password"}),
			})},
			"/api/auth/me": oaMap{"get": oaOp("Auth", "Current account", "", nil)},
			"/api/tokens": oaMap{
				"get": oaOp("Auth", "List API keys", "Hashes only — the secret is shown once at creation.", nil),
				"post": oaOp("Auth", "Create an API key", "The response contains the rk_… secret exactly once.", oaMap{
					"requestBody": jsonBody(oaMap{"name": str("Key name, e.g. vscode-mcp")}, nil),
				}),
			},
			"/api/tokens/{id}": oaMap{"delete": oaOp("Auth", "Revoke an API key", "", oaMap{"parameters": []oaMap{idParam("id", "Key ID")}})},

			"/api/support/tickets": oaMap{
				"get": oaOp("Support", "List support tickets", "", nil),
				"post": oaOp("Support", "Open a support ticket", "Stored locally and forwarded to the vendor CRM when configured.", oaMap{
					"requestBody": jsonBody(oaMap{"subject": str(""), "body": str("Describe the problem"), "category": str("bug | question | feature-request | billing | other"), "severity": str("low | normal | high | urgent")}, []string{"subject", "body"}),
				}),
			},
			"/api/support/tickets/{id}/reply": oaMap{"post": oaOp("Support", "Reply / change status", "", oaMap{
				"parameters":  []oaMap{idParam("id", "Ticket ID")},
				"requestBody": jsonBody(oaMap{"body": str("Reply text"), "status": str("open | pending | resolved | closed")}, nil),
			})},

			"/api/version-check": oaMap{"get": oaOp("System", "Check for updates", "Cached 6h; ?force=true refreshes.", nil)},
			"/api/update":        oaMap{"post": oaOp("System", "Apply update", "Downloads the latest release over the running binary and restarts (admins only).", nil)},
		},
	}
}

func (a *API) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, openAPISpec())
}

// swaggerHTML is a self-contained Swagger UI page (assets from the standard
// CDN — the spec itself is served locally). Styled to sit next to RackOps.
const swaggerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>RackOps API — Swagger</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"/>
<style>
  body { margin: 0; background: #14120e; }
  .topbar { display: none; }
  .swagger-ui, .swagger-ui .info .title, .swagger-ui .opblock-tag,
  .swagger-ui .opblock .opblock-summary-description, .swagger-ui .info li,
  .swagger-ui .info p, .swagger-ui table thead tr th, .swagger-ui .parameter__name,
  .swagger-ui .response-col_status, .swagger-ui label, .swagger-ui .tab li,
  .swagger-ui .model, .swagger-ui .model-title, .swagger-ui section.models h4 { color: #e8e2d4 !important; }
  .swagger-ui .scheme-container, .swagger-ui section.models, .swagger-ui select,
  .swagger-ui input { background: #1c1914 !important; color: #e8e2d4 !important; }
  .swagger-ui .opblock { background: #1c1914; border-color: #3a352b; }
  .swagger-ui .opblock .opblock-section-header { background: #23201a; }
  .swagger-ui .opblock .opblock-section-header h4, .swagger-ui .opblock-description-wrapper p,
  .swagger-ui .btn, .swagger-ui .parameter__type { color: #cfc8b8 !important; }
  .swagger-ui .info .description .renderedMarkdown p code, .swagger-ui .prop-type { color: #f0a832 !important; }
  #ro-note { font: 13px system-ui, sans-serif; color: #a89f8c; background: #1c1914;
    border-bottom: 1px solid #3a352b; padding: 10px 20px; }
  #ro-note b { color: #f0a832; }
</style>
</head>
<body>
<div id="ro-note"><b>RackOps API docs.</b> Hit <b>Authorize</b> and paste an API key from Profile → API keys (starts with rk_) to try requests right here.</div>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
window.onload = () => {
  window.ui = SwaggerUIBundle({
    url: '/api/openapi.json',
    dom_id: '#swagger-ui',
    docExpansion: 'list',
    defaultModelsExpandDepth: -1,
    persistAuthorization: true,
    tryItOutEnabled: true,
  });
};
</script>
</body>
</html>`

func (a *API) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerHTML))
}
