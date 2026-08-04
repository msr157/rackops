package server

import (
	"crypto/tls"
	"fmt"
	"html"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Mailer sends transactional email. It is an interface so tests can fake it.
type Mailer interface {
	// Send delivers a message to one recipient. Configured reports whether
	// the relay has SMTP credentials at all.
	Send(to, subject, textBody, htmlBody string) error
	Configured() bool
}

// SMTPConfig carries relay settings from the environment.
type SMTPConfig struct {
	Host     string
	Port     string // default 587
	Username string
	Password string
	From     string // default "RackOps Support <support@rackops.in>"
	ReplyTo  string
}

// smtpMailer sends via stdlib net/smtp with PLAIN auth over STARTTLS.
type smtpMailer struct {
	cfg SMTPConfig
}

// NewSMTPMailer builds the production mailer. cfg.Host may be empty, in
// which case Configured() reports false and Send fails.
func NewSMTPMailer(cfg SMTPConfig) Mailer {
	if cfg.Port == "" {
		cfg.Port = "587"
	}
	if cfg.From == "" {
		cfg.From = "RackOps Support <support@rackops.in>"
	}
	if cfg.ReplyTo == "" {
		cfg.ReplyTo = "support@rackops.in"
	}
	return &smtpMailer{cfg: cfg}
}

func (m *smtpMailer) Configured() bool {
	return m.cfg.Host != "" && m.cfg.Username != "" && m.cfg.Password != ""
}

// fromAddress extracts the bare address from a "Name <addr>" From header.
func fromAddress(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		if j := strings.LastIndex(from, ">"); j > i {
			return from[i+1 : j]
		}
	}
	return from
}

func (m *smtpMailer) Send(to, subject, textBody, htmlBody string) error {
	if !m.Configured() {
		return fmt.Errorf("email relay not configured")
	}
	addr := net.JoinHostPort(m.cfg.Host, m.cfg.Port)
	msg := buildMIME(m.cfg.From, m.cfg.ReplyTo, to, subject, textBody, htmlBody)

	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: m.cfg.Host}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	if ok, _ := c.Extension("AUTH"); ok {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(fromAddress(m.cfg.From)); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return c.Quit()
}

// buildMIME assembles a multipart/alternative message (plain + HTML).
func buildMIME(from, replyTo, to, subject, textBody, htmlBody string) []byte {
	const boundary = "rackops-crm-alt-1a2b3c"
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Reply-To: %s\r\n", replyTo)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", boundary, textBody)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=utf-8\r\n\r\n%s\r\n", boundary, htmlBody)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return []byte(b.String())
}

// resetEmailBodies renders the fixed password-reset template.
func resetEmailBodies(instanceName, link string) (text, htmlBody string) {
	text = fmt.Sprintf(`Hi,

A password reset was requested for your account on the RackOps instance
%q. Open the link below to choose a new password:

    %s

The link is valid for 1 hour. If you didn't request this, you can safely
ignore this email — your password stays unchanged.

— RackOps Support
support@rackops.in`, instanceName, link)

	hn := html.EscapeString(instanceName)
	hl := html.EscapeString(link)
	htmlBody = fmt.Sprintf(`<!doctype html>
<html><body style="font-family:Arial,Helvetica,sans-serif;color:#1a1a1a;line-height:1.6;max-width:560px;margin:0 auto;padding:24px">
<p>Hi,</p>
<p>A password reset was requested for your account on the RackOps instance <strong>%s</strong>. Click the button below to choose a new password:</p>
<p style="margin:28px 0"><a href="%s" style="background:#ffb02e;color:#241a05;text-decoration:none;padding:11px 22px;border-radius:8px;font-weight:600">Reset password</a></p>
<p style="font-size:13px;color:#555">Or copy this link into your browser:<br><a href="%s">%s</a></p>
<p style="font-size:13px;color:#555">The link is valid for 1 hour. If you didn't request this, you can safely ignore this email — your password stays unchanged.</p>
<p style="font-size:13px;color:#555">— RackOps Support · <a href="mailto:support@rackops.in">support@rackops.in</a></p>
</body></html>`, hn, hl, hl, hl)
	return text, htmlBody
}
