// RackOps Lite control plane — for homelabs, developers, and small teams.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/msr157/rackops/internal/server"
)

func main() {
	port := flag.String("port", "8080", "listen port")
	dashboard := flag.String("dashboard", "", "path to a built dashboard dist/ to serve at / (optional)")
	flag.Parse()

	handler := server.New(server.Options{
		Edition:      "lite",
		DashboardDir: *dashboard,
	})

	addr := ":" + *port
	log.Printf("rackops-lite control plane listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}
