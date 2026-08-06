package edge

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log"
	"math/big"
	"os"
	"sync/atomic"
	"time"
)

// CertSource supplies the POP's TLS certificate and reloads it when the
// files on disk change.
//
// The wildcard certificate for the Front Door zone needs DNS-01 validation,
// which is provider-specific. Rather than embed one DNS provider's API and
// lock the deployment to it, the POP reads a certificate and key from disk
// and watches them: any ACME client (certbot, lego, acme.sh, Caddy) can own
// renewal with whatever provider the operator prefers. Hot reload means a
// renewal never needs a restart, so certificate rotation is not an outage.
type CertSource struct {
	certPath string
	keyPath  string

	// current holds a *tls.Certificate, swapped atomically so in-flight
	// handshakes never observe a half-updated pair.
	current atomic.Pointer[tls.Certificate]

	// lastMod tracks the file timestamps that produced `current`, so the
	// watcher reloads only on real changes.
	lastMod atomic.Pointer[[2]time.Time]

	// selfSigned marks a generated development certificate. Set once at
	// construction and never mutated, so it needs no synchronisation.
	selfSigned bool
}

// NewCertSource loads a certificate and key and starts watching them.
func NewCertSource(certPath, keyPath string) (*CertSource, error) {
	cs := &CertSource{certPath: certPath, keyPath: keyPath}
	if err := cs.reload(); err != nil {
		return nil, err
	}
	go cs.watch()
	return cs, nil
}

// NewSelfSignedCertSource generates an in-memory self-signed certificate for
// the given zone.
//
// This exists so `rackops-edge` runs out of the box for local development
// and integration tests. It is never appropriate in production: browsers
// will refuse it, which is the intended outcome — a POP that silently served
// an untrusted certificate would be worse than one that refuses to start.
func NewSelfSignedCertSource(zone string) (*CertSource, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("edge: generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("edge: generate serial: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "*." + zone, Organization: []string{"RackOps Front Door (development)"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(0, 0, 90),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{zone, "*." + zone, "localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("edge: create certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("edge: parse certificate: %w", err)
	}
	cs := &CertSource{selfSigned: true}
	cs.current.Store(&tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
		Leaf:        leaf,
	})
	return cs, nil
}

// TLSConfig returns a config that always serves the current certificate.
func (c *CertSource) TLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			cert := c.current.Load()
			if cert == nil {
				return nil, fmt.Errorf("edge: no certificate loaded")
			}
			return cert, nil
		},
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
	}
}

// SelfSigned reports whether this source is serving the generated
// development certificate rather than a real one. Browsers reject it, so the
// console shows it prominently instead of leaving it to a startup log line
// nobody scrolls back to.
func (c *CertSource) SelfSigned() bool { return c.selfSigned }

// Expiry reports when the current certificate expires, for the health
// endpoint. A POP whose certificate is about to lapse should be visible
// before visitors start seeing warnings, not after.
func (c *CertSource) Expiry() time.Time {
	cert := c.current.Load()
	if cert == nil || cert.Leaf == nil {
		return time.Time{}
	}
	return cert.Leaf.NotAfter
}

// reload reads the certificate pair from disk and swaps it in.
func (c *CertSource) reload() error {
	pair, err := tls.LoadX509KeyPair(c.certPath, c.keyPath)
	if err != nil {
		return fmt.Errorf("edge: load certificate: %w", err)
	}
	// LoadX509KeyPair leaves Leaf nil; parsing it once here keeps Expiry and
	// every handshake from re-parsing the DER.
	if len(pair.Certificate) > 0 {
		if leaf, err := x509.ParseCertificate(pair.Certificate[0]); err == nil {
			pair.Leaf = leaf
		}
	}
	c.current.Store(&pair)
	c.rememberModTimes()
	return nil
}

func (c *CertSource) rememberModTimes() {
	certInfo, err1 := os.Stat(c.certPath)
	keyInfo, err2 := os.Stat(c.keyPath)
	if err1 != nil || err2 != nil {
		return
	}
	c.lastMod.Store(&[2]time.Time{certInfo.ModTime(), keyInfo.ModTime()})
}

// watch polls the certificate files and reloads on change.
//
// Polling rather than inotify is deliberate: ACME clients renew by writing a
// temp file and renaming, some replace the whole directory, and containers
// may bind-mount the path. A one-minute poll handles all of those
// identically, and a certificate that arrives a minute late is a non-event
// next to a watcher that misses the rename entirely.
func (c *CertSource) watch() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		certInfo, err1 := os.Stat(c.certPath)
		keyInfo, err2 := os.Stat(c.keyPath)
		if err1 != nil || err2 != nil {
			continue
		}
		prev := c.lastMod.Load()
		if prev != nil && certInfo.ModTime().Equal(prev[0]) && keyInfo.ModTime().Equal(prev[1]) {
			continue
		}
		if err := c.reload(); err != nil {
			// Keep serving the old certificate: a half-written renewal is
			// temporary, and dropping TLS over it would be a self-inflicted
			// outage.
			log.Printf("edge: certificate reload failed, keeping previous: %v", err)
			continue
		}
		log.Printf("edge: certificate reloaded (expires %s)", c.Expiry().Format(time.RFC3339))
	}
}
