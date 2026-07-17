// Command crm is the RackOps vendor-side CRM: it receives support tickets
// and usage snapshots forwarded by customers' self-hosted RackOps Lite
// instances, relays password-reset emails, and serves the internal
// operations dashboard.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rackops/crm/internal/server"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "version":
		fmt.Println("crm", version)
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `RackOps CRM — vendor-side support & telemetry console

Usage:
  crm serve [flags]    Run the CRM server
  crm version          Print version

Serve flags:
  --listen :8091               Address to listen on
  --data-dir ~/.rackops-crm    State directory (SQLite + session secret)

Environment:
  CRM_ADMIN_EMAIL      Admin login email (required)
  CRM_ADMIN_PASSWORD   Admin login password (required)
  SMTP_HOST            SMTP relay host (reset-email relay off when unset)
  SMTP_PORT            SMTP port (default 587)
  SMTP_USERNAME        SMTP username
  SMTP_PASSWORD        SMTP password
  SMTP_FROM            From header (default "RackOps Support <support@rackops.in>")`)
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".rackops-crm"
	}
	return filepath.Join(home, ".rackops-crm")
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := fs.String("listen", ":8091", "listen address")
	dataDir := fs.String("data-dir", defaultDataDir(), "state directory")
	_ = fs.Parse(args)

	admin, err := server.NewAdminAuth(os.Getenv("CRM_ADMIN_EMAIL"), os.Getenv("CRM_ADMIN_PASSWORD"))
	if err != nil {
		log.Fatalf("crm: %v", err)
	}

	store, err := server.OpenStore(*dataDir)
	if err != nil {
		log.Fatalf("crm: open store: %v", err)
	}
	defer store.Close()

	mailer := server.NewSMTPMailer(server.SMTPConfig{
		Host:     os.Getenv("SMTP_HOST"),
		Port:     os.Getenv("SMTP_PORT"),
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     os.Getenv("SMTP_FROM"),
	})
	if mailer.Configured() {
		log.Printf("crm: email relay configured (host %s)", os.Getenv("SMTP_HOST"))
	} else {
		log.Printf("crm: email relay NOT configured — /ingest/v1/reset-email will return 503")
	}

	api := server.NewAPI(store, mailer, admin, server.DashboardFS())
	srv := &http.Server{
		Addr:              *listen,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("crm %s: listening on %s (data dir %s)", version, *listen, *dataDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("crm: serve: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("crm: shutting down…")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
