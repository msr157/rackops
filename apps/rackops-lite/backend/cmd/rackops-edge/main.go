// Command rackops-edge is the RackOps Front Door point of presence: the
// vendor-run process that holds a public IP, terminates TLS for the Front
// Door zone, and forwards each request down a tunnel to a customer machine
// that has no public address of its own.
//
// It is deliberately a separate binary from `rackops`. Customers run
// `rackops`; only RackOps runs this. Keeping them apart means a customer
// install carries no POP code paths, and a POP carries no control-plane
// state.
//
// Typical deployment:
//
//	rackops-edge \
//	  --pop-id blr1 \
//	  --zone link.rackops.in \
//	  --cert /etc/rackops/fullchain.pem \
//	  --key  /etc/rackops/privkey.pem \
//	  --admin-token-file /etc/rackops/admin.token
//
// Everything public shares one address (default :443): visitor HTTPS and
// tunnel sessions are told apart by ALPN on TCP, and QUIC takes the same port
// on UDP. Both sides need 443 specifically — visitors because that is what a
// URL without a port means, tunnel clients because that is what restrictive
// networks allow — so sharing is the only arrangement where both work.
//
// The wildcard certificate is obtained out of band by any ACME client using
// DNS-01; this process watches the files and reloads on renewal without a
// restart.
package main

import (
	"context"
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

	"github.com/rackops/rackops/pkg/edge"
	versionpkg "github.com/rackops/rackops/pkg/version"
)

// version is the single product version, shared with the rackops binary.
const version = versionpkg.Current

func main() {
	fs := flag.NewFlagSet("rackops-edge", flag.ExitOnError)
	popID := fs.String("pop-id", "pop1", "identifier for this point of presence")
	zone := fs.String("zone", "link.rackops.in", "DNS zone served by this POP")
	publicAddr := fs.String("public-addr", ":443", "the one public address: visitor HTTPS and tunnel sessions (TCP+UDP)")
	httpAddr := fs.String("http-addr", ":80", "address for the plain-HTTP redirect to HTTPS (empty disables)")
	adminAddr := fs.String("admin-addr", "127.0.0.1:9443", "address for the admin API")
	certPath := fs.String("cert", "", "TLS certificate chain (PEM); omit for a self-signed development certificate")
	keyPath := fs.String("key", "", "TLS private key (PEM)")
	dataDir := fs.String("data-dir", "/var/lib/rackops-edge", "state directory")
	adminToken := fs.String("admin-token", "", "bearer token for the admin API")
	adminTokenFile := fs.String("admin-token-file", "", "file containing the admin API token (preferred over --admin-token)")
	supportEmail := fs.String("support-email", "support@rackops.in", "contact address shown on error pages")
	flushEvery := fs.Duration("flush-interval", 15*time.Second, "how often usage counters are persisted")
	showVersion := fs.Bool("version", false, "print version and exit")
	_ = fs.Parse(os.Args[1:])

	if *showVersion {
		fmt.Println("rackops-edge", version)
		return
	}

	token, err := resolveAdminToken(*adminToken, *adminTokenFile)
	if err != nil {
		log.Fatalf("edge: %v", err)
	}

	certs, err := loadCerts(*certPath, *keyPath, *zone)
	if err != nil {
		log.Fatalf("edge: %v", err)
	}

	registry, err := edge.OpenRegistry(filepath.Join(*dataDir, "tenants.json"))
	if err != nil {
		log.Fatalf("edge: %v", err)
	}

	e, err := edge.New(edge.Config{
		POPID:         *popID,
		Zone:          *zone,
		PublicAddr:    *publicAddr,
		HTTPAddr:      *httpAddr,
		Certs:         certs,
		Registry:      registry,
		FlushInterval: *flushEvery,
		SupportEmail:  *supportEmail,
	})
	if err != nil {
		log.Fatalf("edge: %v", err)
	}

	admin, err := edge.NewAdminAPI(e, token)
	if err != nil {
		log.Fatalf("edge: %v", err)
	}

	if err := e.Start(); err != nil {
		log.Fatalf("edge: %v", err)
	}

	adminSrv := &http.Server{
		Addr:              *adminAddr,
		Handler:           admin.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("edge: admin API stopped: %v", err)
		}
	}()
	log.Printf("edge: admin API on %s", *adminAddr)
	log.Printf("edge: rackops-edge %s ready (pop %s, zone %s)", version, *popID, *zone)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	// Drain rather than cut: in-flight downloads finish, and the final
	// usage flush lands before the process exits.
	log.Printf("edge: shutting down…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = adminSrv.Shutdown(shutdownCtx)
	if err := e.Shutdown(shutdownCtx); err != nil {
		log.Printf("edge: shutdown: %v", err)
	}
	log.Printf("edge: stopped")
}

// resolveAdminToken reads the admin token, preferring the file form.
//
// Passing secrets as command-line flags leaks them to every process on the
// box via /proc/*/cmdline, so the file form is documented as preferred and
// the flag form is kept only for container environments that inject it.
func resolveAdminToken(inline, file string) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read admin token file: %w", err)
		}
		token := strings.TrimSpace(string(b))
		if token == "" {
			return "", fmt.Errorf("admin token file %s is empty", file)
		}
		return token, nil
	}
	if inline == "" {
		return "", fmt.Errorf("an admin token is required: pass --admin-token-file (preferred) or --admin-token")
	}
	return inline, nil
}

// loadCerts picks the certificate source: real files when given, otherwise a
// self-signed certificate with a loud warning so nobody ships it by accident.
func loadCerts(certPath, keyPath, zone string) (*edge.CertSource, error) {
	if certPath != "" && keyPath != "" {
		cs, err := edge.NewCertSource(certPath, keyPath)
		if err != nil {
			return nil, err
		}
		log.Printf("edge: certificate loaded from %s (expires %s)", certPath, cs.Expiry().Format(time.RFC3339))
		return cs, nil
	}
	if certPath != "" || keyPath != "" {
		return nil, fmt.Errorf("--cert and --key must be given together")
	}
	log.Printf("edge: WARNING — no --cert/--key given, generating a self-signed certificate for *.%s.", zone)
	log.Printf("edge: WARNING — browsers will reject it. This mode is for development only.")
	return edge.NewSelfSignedCertSource(zone)
}
