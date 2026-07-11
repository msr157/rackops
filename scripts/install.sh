#!/bin/sh
# RackOps one-line installer — Linux and Windows/WSL2.
#   curl -fsSL https://github.com/msr157/rackops/releases/latest/download/install.sh | bash
#
# Downloads the RackOps server binary, installs it, and starts it. Re-running
# upgrades in place. Set RACKOPS_PORT to change the port (default 8080).
set -eu

REPO="msr157/rackops"
PORT="${RACKOPS_PORT:-8080}"

say() { printf '%s\n' "$*"; }
err() { printf 'error: %s\n' "$*" >&2; exit 1; }

# ---- platform ----
[ "$(uname -s)" = "Linux" ] || err "The RackOps server runs on Linux. On Windows, install WSL2 first, then run this inside it: https://github.com/$REPO#windows"

case "$(uname -m)" in
  x86_64|amd64)  ASSET="rackops-linux-amd64" ;;
  aarch64|arm64) ASSET="rackops-linux-arm64" ;;
  *) err "unsupported CPU architecture: $(uname -m)" ;;
esac

# ---- privilege helper (sudo prompts via /dev/tty even under `curl | bash`) ----
SUDO=""
if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1; then
  SUDO="sudo"
fi
is_root_capable() { [ "$(id -u)" -eq 0 ] || [ -n "$SUDO" ]; }

say ""
say "  ┌─────────────────────────────┐"
say "  │  RackOps installer          │"
say "  └─────────────────────────────┘"
say ""

# ---- download ----
URL="https://github.com/$REPO/releases/latest/download/$ASSET"
say "▸ Downloading RackOps ($ASSET)…"
TMP="$(mktemp)"
if command -v curl >/dev/null 2>&1; then
  curl -fSL --progress-bar "$URL" -o "$TMP" || err "download failed: $URL"
elif command -v wget >/dev/null 2>&1; then
  wget -q --show-progress "$URL" -O "$TMP" || err "download failed: $URL"
else
  err "need curl or wget installed"
fi
chmod +x "$TMP"

# ---- install ----
if is_root_capable; then
  DEST="/usr/local/bin/rackops"
  $SUDO install -m 0755 "$TMP" "$DEST"
  rm -f "$TMP"
else
  mkdir -p "$HOME/.local/bin"
  DEST="$HOME/.local/bin/rackops"
  install -m 0755 "$TMP" "$DEST"
  rm -f "$TMP"
  case ":$PATH:" in
    *":$HOME/.local/bin:"*) ;;
    *) say "  note: add \$HOME/.local/bin to your PATH to run 'rackops' directly" ;;
  esac
fi
say "▸ Installed: $DEST"

# ---- docker check ----
if ! command -v docker >/dev/null 2>&1; then
  say ""
  say "⚠  Docker isn't installed — RackOps needs it to run apps (telemetry still works)."
  say "     Linux:         curl -fsSL https://get.docker.com | sh"
  say "     Windows/WSL2:  install Docker Desktop, then enable it for your WSL distro"
  say "                    (Settings → Resources → WSL integration)"
fi

# ---- start ----
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ] && is_root_capable; then
  say "▸ Installing systemd service (starts on boot, restarts on crash)…"
  $SUDO sh -c "cat > /etc/systemd/system/rackops.service" <<UNIT
[Unit]
Description=RackOps control plane
After=network-online.target docker.service
Wants=network-online.target

[Service]
ExecStart=$DEST server --listen :$PORT
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
UNIT
  $SUDO systemctl daemon-reload
  $SUDO systemctl enable --now rackops >/dev/null 2>&1 || $SUDO systemctl restart rackops
else
  # No systemd (e.g. plain WSL2): run detached in the background.
  say "▸ Starting RackOps in the background…"
  pkill -f "$DEST server" 2>/dev/null || true
  mkdir -p "$HOME/.rackops"
  setsid "$DEST" server --listen ":$PORT" >"$HOME/.rackops/server.log" 2>&1 < /dev/null &
  say "     logs: $HOME/.rackops/server.log"
  say "     stop: pkill -f 'rackops server'"
fi

# ---- wait for it to answer ----
i=0
while [ "$i" -lt 20 ]; do
  if command -v curl >/dev/null 2>&1 && curl -fsS "http://localhost:$PORT/api/auth/status" >/dev/null 2>&1; then
    break
  fi
  i=$((i + 1)); sleep 1
done

say ""
say "✅  RackOps is running:  http://localhost:$PORT"
say ""
say "    Open that in your browser and create your owner account."
say "    Add more machines from Machines → Add machine."
say ""
