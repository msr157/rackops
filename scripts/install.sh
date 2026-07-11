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

# ============================================================================
# Prerequisites — RackOps scans the machine and installs anything missing, so
# the user never has to set up Docker, compose, or git by hand. Requirements:
#   • docker (or podman) ...... run app containers               [auto-install]
#   • docker compose plugin ... multi-service templates/stacks   [auto-install]
#   • git ..................... deploy apps straight from a repo  [auto-install]
# ============================================================================

# detect_pkg finds the host package manager (for git / compose-plugin installs).
PKG=""
detect_pkg() {
  [ -n "$PKG" ] && return 0
  for m in apt-get dnf yum apk pacman zypper; do
    if command -v "$m" >/dev/null 2>&1; then PKG="$m"; return 0; fi
  done
  return 1
}

# pkg_install installs one or more packages with the detected package manager.
pkg_install() {
  detect_pkg || return 1
  case "$PKG" in
    apt-get) $SUDO apt-get update -y >/dev/null 2>&1; $SUDO env DEBIAN_FRONTEND=noninteractive apt-get install -y "$@" >/dev/null 2>&1 ;;
    dnf)     $SUDO dnf install -y "$@" >/dev/null 2>&1 ;;
    yum)     $SUDO yum install -y "$@" >/dev/null 2>&1 ;;
    apk)     $SUDO apk add --no-cache "$@" >/dev/null 2>&1 ;;
    pacman)  $SUDO pacman -Sy --noconfirm "$@" >/dev/null 2>&1 ;;
    zypper)  $SUDO zypper --non-interactive install "$@" >/dev/null 2>&1 ;;
    *) return 1 ;;
  esac
}

runtime_ready() {
  # docker (daemon reachable) or podman (daemonless) counts as ready.
  if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then return 0; fi
  if command -v podman >/dev/null 2>&1 && podman info >/dev/null 2>&1; then return 0; fi
  return 1
}

start_docker() {
  if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    $SUDO systemctl enable --now docker >/dev/null 2>&1 || $SUDO systemctl start docker >/dev/null 2>&1 || true
  elif ! docker info >/dev/null 2>&1; then
    # No systemd (e.g. plain WSL2): start the daemon in the background.
    say "     starting the Docker daemon (no systemd detected)…"
    $SUDO sh -c 'nohup dockerd >/var/log/rackops-dockerd.log 2>&1 &' || true
    j=0; while [ "$j" -lt 15 ]; do docker info >/dev/null 2>&1 && break; j=$((j + 1)); sleep 1; done
  fi
}

install_docker() {
  if ! is_root_capable; then
    say "  ⚠ docker — can't install without root. Install it, then re-run:"
    say "      curl -fsSL https://get.docker.com | sh"
    return 1
  fi
  if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
    say "  ⚠ docker — need curl or wget to install it."
    return 1
  fi
  say "  ▸ docker — installing Docker Engine (this can take a minute)…"
  # The official convenience script handles every major distro and pulls in the
  # compose + buildx plugins; it's non-interactive, so safe under `curl | bash`.
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL https://get.docker.com | $SUDO sh >/dev/null 2>&1 || { say "  ⚠ docker — install failed (try: curl -fsSL https://get.docker.com | sh)"; return 1; }
  else
    wget -qO- https://get.docker.com | $SUDO sh >/dev/null 2>&1 || { say "  ⚠ docker — install failed (try: curl -fsSL https://get.docker.com | sh)"; return 1; }
  fi
  start_docker
  # Let the invoking (non-root) user run docker without sudo next time.
  if [ "$(id -u)" -ne 0 ] && command -v usermod >/dev/null 2>&1; then
    $SUDO usermod -aG docker "$(id -un)" >/dev/null 2>&1 || true
  fi
  return 0
}

say ""
say "▸ Checking prerequisites…"

# --- git (deploy from a git repo) ---
if command -v git >/dev/null 2>&1; then
  say "  ✓ git"
elif is_root_capable && pkg_install git && command -v git >/dev/null 2>&1; then
  say "  ✓ git — installed"
else
  say "  ⚠ git — not installed (git-repo deploys will be unavailable; image & template deploys still work)"
fi

# --- container runtime (docker/podman) ---
if runtime_ready; then
  say "  ✓ container runtime ($(command -v docker >/dev/null 2>&1 && echo docker || echo podman))"
else
  if command -v docker >/dev/null 2>&1 || command -v podman >/dev/null 2>&1; then
    # Installed but not running (common on WSL2): just start it.
    say "  ▸ container runtime — installed but not running, starting it…"
    start_docker
  else
    install_docker || true
  fi
  if runtime_ready; then
    say "  ✓ container runtime — ready"
  else
    say "  ⚠ container runtime — not ready. RackOps still starts (telemetry works),"
    say "      but app deploys need it. On Windows/WSL2 install Docker Desktop and"
    say "      enable WSL integration, or start the daemon with:  sudo dockerd &"
  fi
fi

# --- docker compose plugin (multi-service templates & compose stacks) ---
if command -v docker >/dev/null 2>&1; then
  if docker compose version >/dev/null 2>&1; then
    say "  ✓ docker compose"
  elif is_root_capable && pkg_install docker-compose-plugin && docker compose version >/dev/null 2>&1; then
    say "  ✓ docker compose — installed"
  else
    say "  ⚠ docker compose — not available (multi-service templates won't deploy;"
    say "      single-container apps still work). Fix: install the docker-compose-plugin package."
  fi
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
  # Docker's socket is root:docker. A freshly added group only applies to new
  # logins, so this same-session process can't reach it yet — run RackOps under
  # sudo (matching the systemd path, which runs as root) so app deploys work
  # immediately. If docker is already reachable as this user, no sudo needed.
  RUN_AS=""
  if [ -n "$SUDO" ] && ! docker info >/dev/null 2>&1 && $SUDO docker info >/dev/null 2>&1; then
    RUN_AS="$SUDO"
  fi
  setsid $RUN_AS "$DEST" server --listen ":$PORT" >"$HOME/.rackops/server.log" 2>&1 < /dev/null &
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
