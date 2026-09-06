package cloud

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// BootstrapConfig contains no credential. The enrollment token is supplied
// separately and the resulting script must be stored encrypted by the caller.
type BootstrapConfig struct {
	ServerURL    string `json:"serverUrl"`
	OrgID        string `json:"orgId"`
	Architecture string `json:"architecture"`
	BinaryURL    string `json:"binaryUrl"`
	SHA256       string `json:"sha256"`
}

var bootstrapURLChars = regexp.MustCompile(`^[a-zA-Z0-9:/._\-\[\]]+$`)
var bootstrapOrg = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
var releaseBinary = regexp.MustCompile(`^https://github.com/msr157/rackops/releases/download/v[0-9][a-zA-Z0-9._-]*/rackops-linux-(amd64|arm64)$`)
var checksum = regexp.MustCompile(`^[0-9a-f]{64}$`)
var enrollmentProof = regexp.MustCompile(`^rkenroll_[0-9a-f]{64}$`)

func (c BootstrapConfig) Validate() error {
	u, err := url.Parse(c.ServerURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !bootstrapURLChars.MatchString(c.ServerURL) || !bootstrapOrg.MatchString(c.OrgID) {
		return errors.New("bootstrap requires a plain HTTPS control-plane URL and organization ID")
	}
	if (c.Architecture != "amd64" && c.Architecture != "arm64") || !releaseBinary.MatchString(c.BinaryURL) || !strings.HasSuffix(c.BinaryURL, "-"+c.Architecture) || !checksum.MatchString(c.SHA256) {
		return errors.New("bootstrap requires a version-pinned official Linux binary and SHA-256 checksum matching the architecture")
	}
	return nil
}

func RenderBootstrap(c BootstrapConfig, token string) (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	if !enrollmentProof.MatchString(token) {
		return "", errors.New("invalid scoped enrollment token")
	}
	machine := "x86_64"
	if c.Architecture == "arm64" {
		machine = "aarch64"
	}
	// Supported images must already contain systemd, curl, git, Docker and the
	// Compose plugin. Failing that check precedes installation or credential writes.
	script := fmt.Sprintf(`#!/bin/sh
set -eu
umask 077
[ "$(id -u)" = 0 ] || { echo 'RackOps bootstrap requires root'; exit 1; }
[ "$(uname -s)" = Linux ] && [ "$(uname -m)" = '%s' ] || { echo 'RackOps image architecture mismatch'; exit 1; }
for tool in curl sha256sum install systemctl docker git grep; do
  command -v "$tool" >/dev/null || { echo 'RackOps image prerequisites missing'; exit 1; }
done
docker info >/dev/null 2>&1 || { echo 'Docker is unavailable'; exit 1; }
docker compose version >/dev/null 2>&1 || { echo 'Docker Compose plugin is unavailable'; exit 1; }
install -d -m 0700 /var/lib/rackops-cloud
if [ -e /var/lib/rackops-cloud/bootstrap-complete ]; then exit 0; fi
download=$(mktemp /var/lib/rackops-cloud/.binary-XXXXXX)
trap 'rm -f "$download"' EXIT HUP INT TERM
curl --proto '=https' --proto-redir '=https' --tlsv1.2 --fail --silent --show-error --location --connect-timeout 15 --max-time 300 '%s' -o "$download"
printf '%%s  %%s\n' '%s' "$download" | sha256sum --check --status || { echo 'RackOps binary integrity check failed'; exit 1; }
chmod 0700 "$download"
"$download" agent --help 2>&1 | grep -q -- '-token-file' || { echo 'RackOps release does not support scoped cloud enrollment'; exit 1; }
install -m 0755 "$download" /usr/local/bin/rackops
cat > /var/lib/rackops-cloud/enrollment-token <<'RACKOPS_TOKEN'
%s
RACKOPS_TOKEN
chmod 0600 /var/lib/rackops-cloud/enrollment-token
cat > /etc/systemd/system/rackops-cloud-agent.service <<'RACKOPS_UNIT'
[Unit]
Description=RackOps cloud agent
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service
[Service]
Type=simple
ExecStart=/usr/local/bin/rackops agent --server %s --token-file /var/lib/rackops-cloud/enrollment-token --org %s --data-dir /var/lib/rackops-cloud
Restart=always
RestartSec=10
UMask=0077
[Install]
WantedBy=multi-user.target
RACKOPS_UNIT
systemctl daemon-reload
systemctl enable --now rackops-cloud-agent.service
touch /var/lib/rackops-cloud/bootstrap-complete
`, machine, c.BinaryURL, c.SHA256, token, c.ServerURL, c.OrgID)
	return script, nil
}

// WithBootstrap copies an adapter so its secret startup payload cannot affect
// another concurrent operation using the original client.
func WithBootstrap(p Provider, script string) (Provider, error) {
	if !strings.HasPrefix(script, "#!/bin/sh\n") || len(script) > 16*1024 {
		return nil, errors.New("invalid or oversized bootstrap payload")
	}
	switch original := p.(type) {
	case *AWS:
		copy := *original
		copy.bootstrap = script
		return &copy, nil
	case *Azure:
		copy := *original
		copy.bootstrap = script
		return &copy, nil
	case *GCP:
		copy := *original
		copy.bootstrap = script
		return &copy, nil
	}
	return nil, errors.New("provider does not support cloud bootstrap")
}
