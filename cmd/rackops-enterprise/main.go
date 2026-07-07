// RackOps Enterprise control plane — multi-server orgs; adds stubs for
// clusters, RBAC/SSO, and other enterprise features (see internal/enterprise).
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/msr157/rackops/internal/enterprise"
	"github.com/msr157/rackops/internal/server"
)

func main() {
	port := flag.String("port", "8081", "listen port")
	dashboard := flag.String("dashboard", "", "path to a built dashboard dist/ to serve at / (optional)")
	flag.Parse()

	handler := server.New(server.Options{
		Edition:      "enterprise",
		DashboardDir: *dashboard,
		Extra:        enterprise.Routes,
	})

	addr := ":" + *port
	log.Printf("rackops-enterprise control plane listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}
