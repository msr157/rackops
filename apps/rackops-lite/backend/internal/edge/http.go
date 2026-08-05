package edge

import (
	"bufio"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
)

// meteredWriter counts response bytes on their way to the visitor.
//
// It must preserve the optional interfaces net/http hands to handlers, or
// wrapping it would silently break real features: Flush for SSE and
// streaming logs, Hijack for websocket upgrades, ReadFrom for efficient
// large-file copies. Losing Flush turns a live log tail into a page that
// appears to hang, which is exactly the kind of regression that gets blamed
// on "the tunnel being slow".
type meteredWriter struct {
	http.ResponseWriter
	n atomic.Int64
}

func (m *meteredWriter) Write(p []byte) (int, error) {
	n, err := m.ResponseWriter.Write(p)
	m.n.Add(int64(n))
	return n, err
}

func (m *meteredWriter) Flush() {
	if f, ok := m.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (m *meteredWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := m.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("edge: response writer does not support hijacking")
	}
	return h.Hijack()
}

// Unwrap lets net/http's ResponseController reach the underlying writer for
// deadline control on long-lived streams.
func (m *meteredWriter) Unwrap() http.ResponseWriter { return m.ResponseWriter }

// countingReadCloser counts request-body bytes. Uploads consume the same
// POP bandwidth as downloads and must be billed the same way.
type countingReadCloser struct {
	io.ReadCloser
	n atomic.Int64
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.n.Add(int64(n))
	return n, err
}

// writeStatus renders a visitor-facing status page.
//
// These pages are the public face of every RackOps customer's app when
// something is wrong, so they are plain, honest, and free of stack traces or
// internal identifiers. Content negotiation keeps API clients happy: a
// fetch() that gets an HTML error page instead of JSON produces a far more
// confusing bug report than a clean JSON error.
func (e *Edge) writeStatus(w http.ResponseWriter, r *http.Request, code int, title, detail string) {
	w.Header().Set("X-Rackops-Pop", e.cfg.POPID)
	w.Header().Set("Cache-Control", "no-store")

	if prefersJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		fmt.Fprintf(w, "{\"error\":%q,\"detail\":%q,\"pop\":%q}\n", title, detail, e.cfg.POPID)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprintf(w, statusPage,
		code, html.EscapeString(title), html.EscapeString(title),
		html.EscapeString(detail), html.EscapeString(e.cfg.SupportEmail),
		html.EscapeString(e.cfg.SupportEmail), html.EscapeString(e.cfg.POPID))
}

// prefersJSON reports whether the caller looks like an API client rather
// than a browser.
func prefersJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		return true
	}
	// A request that never asked for HTML is almost certainly programmatic.
	return accept != "" && !strings.Contains(accept, "text/html") && !strings.Contains(accept, "*/*")
}

// statusPage is the visitor-facing error template. It follows the product's
// quiet console styling — near-black, signal green — so an error page still
// looks like part of RackOps rather than a default server page.
const statusPage = `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>%d %s · RackOps</title>
<style>
  :root { color-scheme: dark; }
  body { margin:0; min-height:100vh; display:grid; place-items:center;
         background:#0a0b0d; color:#e6e8ea;
         font:15px/1.6 ui-sans-serif,-apple-system,"Segoe UI",Roboto,sans-serif; }
  main { max-width:30rem; padding:2.5rem 1.5rem; text-align:center; }
  .mark { width:.5rem; height:.5rem; border-radius:50%%; background:#3DF582;
          display:inline-block; margin-bottom:1.5rem; }
  h1 { font-size:1.25rem; font-weight:600; margin:0 0 .5rem; letter-spacing:-.01em; }
  p { margin:0 0 1.5rem; color:#8b9096; }
  a { color:#3DF582; text-decoration:none; }
  a:hover { text-decoration:underline; }
  footer { font-size:12px; color:#4a4f55; margin-top:2rem; }
</style>
</head><body><main>
  <span class="mark"></span>
  <h1>%s</h1>
  <p>%s</p>
  <footer>
    If this is your app, check it in your RackOps console —
    or write to <a href="mailto:%s">%s</a>.<br>RackOps Front Door · %s
  </footer>
</main></body></html>
`
