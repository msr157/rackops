// RackOps Lite control plane — for homelabs, developers, and small teams.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/msr157/rackops/internal/server"
)

func main() {
	port := flag.String("port", "8080", "listen port")
	dashboard := flag.String("dashboard", "", "path to a built dashboard dist/ to serve at / (optional)")
	flag.Parse()

	// DATABASE_URL follows the standard Postgres DSN format:
	// postgres://user:password@host:port/dbname?sslmode=disable
	// When unset the server runs with an in-memory node registry.
	dbURL := os.Getenv("DATABASE_URL")

	handler := server.New(server.Options{
		Edition:      "lite",
		DashboardDir: *dashboard,
		DatabaseURL:  dbURL,
	})

	addr := ":" + *port
	log.Printf("rackops-lite control plane listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}
