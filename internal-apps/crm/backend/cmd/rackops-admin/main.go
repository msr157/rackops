// Command rackops-admin is the RackOps vendor console: everything that runs
// on RackOps' own machines, in one process.
//
//	Support     support tickets forwarded by customer installs, and replies
//	CRM         install inventory, versions, usage analytics
//	Front Door  the tunnel edge customers dial for public HTTPS
//
// These were three moving parts; running them as one removes the failure
// modes that come from wiring them together — two daemons to supervise, two
// data directories to back up, two sets of credentials, and an admin API
// that had to be exposed over the network so one could talk to the other.
// Merged, the tunnel registry is just another page in the console, behind
// the same login.
//
// Minimum viable command — everything else has a working default:
//
//	RACKOPS_ADMIN_EMAIL=ops@rackops.in RACKOPS_ADMIN_PASSWORD=… \
//	  rackops-admin serve --frontdoor --zone link.rackops.in \
//	    --cert /etc/rackops/fullchain.pem --key /etc/rackops/privkey.pem
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rackops/crm/internal/server"
	"github.com/rackops/rackops/pkg/edge"
	versionpkg "github.com/rackops/rackops/pkg/version"
)

const version = versionpkg.Current

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "version":
		fmt.Println("rackops-admin", version)
	case "export-reply":
		runExportReply(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `RackOps Admin — the vendor console: support, CRM and the Front Door tunnel

Usage:
  rackops-admin serve [flags]    Run the console (and the tunnel edge)
  rackops-admin version          Print version
  rackops-admin export-reply --data-dir PATH --install ID --ticket ID --message ID --output FILE
                                Export one agent reply locally without changing delivery

Console flags:
  --listen :8091                 Console + ingest API address
  --data-dir ~/.rackops-admin    State directory (SQLite, secrets, tenants)

Front Door flags (the tunnel customers connect to):
  --frontdoor                    Run the tunnel edge in this process
  --zone link.rackops.in         DNS zone public app addresses live under
  --public-addr :443             One port: visitor HTTPS and tunnel sessions
  --http-addr :80                Plain-HTTP redirect to HTTPS ("" disables)
  --pop-id pop1                  Name of this point of presence
  --cert / --key                 Wildcard certificate for *.<zone> (PEM)
  --support-email                Address shown on visitor error pages

Required environment:
  RACKOPS_ADMIN_EMAIL            Console login email
  RACKOPS_ADMIN_PASSWORD         Console login password

Optional environment (support email relay; off when unset):
  SMTP_HOST, SMTP_PORT, SMTP_USERNAME, SMTP_PASSWORD, SMTP_FROM

Legacy CRM_ADMIN_EMAIL / CRM_ADMIN_PASSWORD are still accepted.`)
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".rackops-admin"
	}
	return filepath.Join(home, ".rackops-admin")
}

func runExportReply(args []string) {
	fs := flag.NewFlagSet("export-reply", flag.ExitOnError)
	dir := fs.String("data-dir", defaultDataDir(), "existing Admin state directory")
	install := fs.String("install", "", "exact install ID")
	ticket := fs.String("ticket", "", "exact ticket ID")
	message := fs.String("message", "", "exact agent reply ID")
	output := fs.String("output", "", "new private output file; refuses overwrite")
	_ = fs.Parse(args)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	digest, err := server.ExportAgentReply(ctx, *dir, *install, *ticket, *message, *output)
	if err != nil {
		log.Fatalf("reply export: %v", err)
	}
	fmt.Printf("Reply body exported. SHA-256: %s\nOriginal and delivery state unchanged.\n", digest)
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := fs.String("listen", ":8091", "console + ingest API address")
	dataDir := fs.String("data-dir", defaultDataDir(), "state directory")

	frontDoor := fs.Bool("frontdoor", false, "run the Front Door tunnel edge in this process")
	zone := fs.String("zone", "link.rackops.in", "DNS zone public app addresses live under")
	publicAddr := fs.String("public-addr", ":443", "one public address: visitor HTTPS and tunnel sessions")
	httpAddr := fs.String("http-addr", ":80", "plain-HTTP redirect address (empty disables)")
	popID := fs.String("pop-id", "pop1", "identifier for this point of presence")
	certPath := fs.String("cert", "", "wildcard TLS certificate chain (PEM); omit for a development certificate")
	keyPath := fs.String("key", "", "wildcard TLS private key (PEM)")
	supportEmail := fs.String("support-email", "support@rackops.in", "address shown on visitor error pages")
	enrollTokenFile := fs.String("enroll-token-file", "", "file holding the enrollment token customer control planes present (default: <data-dir>/frontdoor-enroll.token, created if absent)")
	_ = fs.Parse(args)

	admin, err := server.NewAdminAuth(adminEmail(), adminPassword())
	if err != nil {
		log.Fatalf("admin: %v", err)
	}

	store, err := server.OpenStore(*dataDir)
	if err != nil {
		log.Fatalf("admin: open store: %v", err)
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
		log.Printf("admin: email relay configured (host %s)", os.Getenv("SMTP_HOST"))
	} else {
		log.Printf("admin: email relay NOT configured — password-reset relay will return 503")
	}

	api := server.NewAPI(store, mailer, admin, server.DashboardFS())

	// The tunnel edge, in-process. A failure to start it is loud but not
	// fatal: support tickets must keep working even when the edge cannot
	// bind a port, because that is exactly when customers write in.
	var fd *server.FrontDoor
	if *frontDoor {
		fd, err = startFrontDoor(*dataDir, *zone, *popID, *publicAddr, *httpAddr, *certPath, *keyPath, *supportEmail, *enrollTokenFile)
		if err != nil {
			log.Printf("admin: Front Door failed to start: %v", err)
			log.Printf("admin: the console is still running; fix the above and restart")
		} else {
			api.FrontDoor = fd
		}
	} else {
		log.Printf("admin: Front Door disabled (pass --frontdoor to run the tunnel edge here)")
	}

	srv := &http.Server{
		Addr:              *listen,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("rackops-admin %s: console on %s (data dir %s)", version, *listen, *dataDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("admin: serve: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("admin: shutting down…")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	// Drain the tunnel too: in-flight downloads finish and the final usage
	// snapshot lands before the process exits.
	if err := fd.Shutdown(ctx); err != nil {
		log.Printf("admin: front door shutdown: %v", err)
	}
	log.Println("admin: stopped")
}

// startFrontDoor brings the tunnel edge up, sharing this process's data dir.
func startFrontDoor(dataDir, zone, popID, publicAddr, httpAddr, certPath, keyPath, supportEmail, enrollTokenFile string) (*server.FrontDoor, error) {
	certs, err := loadCerts(certPath, keyPath, zone)
	if err != nil {
		return nil, err
	}
	if enrollTokenFile == "" {
		enrollTokenFile = filepath.Join(dataDir, "frontdoor-enroll.token")
	}
	enrollToken, err := loadOrCreateToken(enrollTokenFile)
	if err != nil {
		return nil, err
	}
	// One data directory for the whole vendor side: the SQLite database and
	// the tunnel's tenant registry sit together, so "back up the admin box"
	// is one path, not two.
	registry, err := edge.OpenRegistry(filepath.Join(dataDir, "frontdoor-tenants.json"))
	if err != nil {
		return nil, err
	}
	fd, err := server.StartFrontDoor(edge.Config{
		POPID:        popID,
		Zone:         zone,
		PublicAddr:   publicAddr,
		HTTPAddr:     httpAddr,
		Certs:        certs,
		Registry:     registry,
		SupportEmail: supportEmail,
	}, registry, enrollToken)
	if err != nil {
		return nil, err
	}
	log.Printf("admin: Front Door serving *.%s on %s (pop %s)", zone, fd.PublicAddr, popID)
	if certs.SelfSigned() {
		log.Printf("admin: WARNING — Front Door is using a self-signed certificate. Browsers will")
		log.Printf("admin: WARNING — reject it. Pass --cert and --key for a real wildcard.")
	}
	return fd, nil
}

// loadOrCreateToken reads the enrollment token, minting one on first run.
//
// Generating it rather than demanding it is the difference between a setup
// that is one command and one that starts with "first, invent a secret and
// put it somewhere". The value is shown in the console so an operator can
// copy a complete set of customer flags out of the page.
func loadOrCreateToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(b)); tok != "" {
			return tok, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate enrollment token: %w", err)
	}
	tok := hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create token dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(tok), 0o600); err != nil {
		return "", fmt.Errorf("write enrollment token: %w", err)
	}
	log.Printf("admin: created a Front Door enrollment token at %s", path)
	return tok, nil
}

// loadCerts picks the certificate source, falling back to a development
// certificate so the console comes up rather than refusing to start.
func loadCerts(certPath, keyPath, zone string) (*edge.CertSource, error) {
	switch {
	case certPath != "" && keyPath != "":
		cs, err := edge.NewCertSource(certPath, keyPath)
		if err != nil {
			return nil, err
		}
		log.Printf("admin: certificate loaded from %s (expires %s)", certPath, cs.Expiry().Format(time.RFC3339))
		return cs, nil
	case certPath != "" || keyPath != "":
		return nil, errors.New("--cert and --key must be given together")
	default:
		return edge.NewSelfSignedCertSource(zone)
	}
}

// adminEmail and adminPassword accept the RackOps Admin names and fall back
// to the older CRM ones, so an existing deployment keeps working across the
// rename instead of failing to boot on a variable nobody knew had changed.
func adminEmail() string {
	return firstNonEmpty(os.Getenv("RACKOPS_ADMIN_EMAIL"), os.Getenv("CRM_ADMIN_EMAIL"))
}

func adminPassword() string {
	return firstNonEmpty(os.Getenv("RACKOPS_ADMIN_PASSWORD"), os.Getenv("CRM_ADMIN_PASSWORD"))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
