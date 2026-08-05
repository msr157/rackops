#!/bin/sh
# RackOps installer — Linux, macOS, and Windows/WSL2.
#
#   curl -fsSL https://github.com/msr157/rackops/releases/latest/download/install.sh | sudo bash
#
# Downloads the RackOps server binary, installs everything it needs (Docker,
# compose, git), and starts it. Re-running the same command upgrades in place.
# Set RACKOPS_PORT to change the port (default 8080).
set -eu

REPO="msr157/rackops"
PORT="${RACKOPS_PORT:-8080}"
BIN="/usr/local/bin/rackops"
LOG="$(mktemp /tmp/rackops-install.XXXXXX.log)"

# ---------------------------------------------------------------------------
# Palette — RackOps design language: phosphor amber on near-black, LED status.
# Colors only when stdout is a real terminal (and the user hasn't opted out).
# ---------------------------------------------------------------------------
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ] && [ "${TERM:-dumb}" != "dumb" ]; then
  TTY=1
  AMBER="$(printf '\033[38;5;214m')"   # brand accent
  GREEN="$(printf '\033[38;5;77m')"    # ok LED
  RED="$(printf '\033[38;5;203m')"     # fail LED
  YELLOW="$(printf '\033[38;5;179m')"  # warn LED
  DIM="$(printf '\033[2m')"
  BOLD="$(printf '\033[1m')"
  R="$(printf '\033[0m')"              # reset
else
  TTY=0
  AMBER=""; GREEN=""; RED=""; YELLOW=""; DIM=""; BOLD=""; R=""
fi

say()  { printf '%s\n' "$*"; }
ok()   { printf '  %s●%s %s\n' "$GREEN" "$R" "$*"; }
warn() { printf '  %s●%s %s\n' "$YELLOW" "$R" "$*"; }
info() { printf '  %s○%s %s\n' "$DIM" "$R" "$*"; }

# section prints a dim rule with a bold title:  ── Title ─────────────────
section() {
  printf '\n%s──%s %s%s%s %s' "$DIM" "$R" "$BOLD" "$1" "$R" "$DIM"
  i=$((44 - ${#1}))
  while [ "$i" -gt 0 ]; do printf '─'; i=$((i - 1)); done
  printf '%s\n' "$R"
}

# die prints a red LED, an explanation, and the log tail if it has content.
die() {
  printf '  %s●%s %s\n' "$RED" "$R" "$*" >&2
  if [ -s "$LOG" ]; then
    printf '\n%s  ── last output ─────────────────────────────%s\n' "$DIM" "$R" >&2
    tail -6 "$LOG" | sed 's/^/    /' >&2
  fi
  printf '\n' >&2
  exit 1
}

# step runs a command behind an amber spinner:  ⠹ message…
# On success the spinner line is replaced by the caller's ok() line; on
# failure the command's output tail is shown. Falls back to plain lines when
# output isn't a terminal (CI, logs).
step() {
  msg="$1"; shift
  : >"$LOG"
  if [ "$TTY" = 1 ]; then
    "$@" >>"$LOG" 2>&1 &
    pid=$!
    while kill -0 "$pid" 2>/dev/null; do
      for f in '⠋' '⠙' '⠹' '⠸' '⠼' '⠴' '⠦' '⠧' '⠇' '⠏'; do
        kill -0 "$pid" 2>/dev/null || break
        printf '\r  %s%s%s %s ' "$AMBER" "$f" "$R" "$msg"
        sleep 0.08 2>/dev/null || sleep 1
      done
    done
    rc=0; wait "$pid" || rc=$?
    printf '\r\033[K'
  else
    printf '  … %s\n' "$msg"
    rc=0; "$@" >>"$LOG" 2>&1 || rc=$?
  fi
  return "$rc"
}

# ---------------------------------------------------------------------------
# Banner — a rack unit with amber LEDs. The brand, in 80 columns or fewer.
# ---------------------------------------------------------------------------
say ""
say "   ${DIM}┌──────────────┐${R}"
say "   ${DIM}│${R} ≡≡≡≡≡≡≡≡   ${AMBER}●${R} ${DIM}│${R}    ${BOLD}${AMBER}R A C K O P S${R}"
say "   ${DIM}├──────────────┤${R}"
say "   ${DIM}│${R} ≡≡≡≡≡≡≡≡   ${AMBER}●${R} ${DIM}│${R}    ${DIM}every machine. one control plane.${R}"
say "   ${DIM}├──────────────┤${R}"
say "   ${DIM}│${R} ≡≡≡≡≡≡≡≡   ${GREEN}●${R} ${DIM}│${R}    ${DIM}github.com/$REPO${R}"
say "   ${DIM}└──────────────┘${R}"

# ---------------------------------------------------------------------------
# System checks
# ---------------------------------------------------------------------------
section "System"

if [ "$(id -u)" -ne 0 ]; then
  printf '  %s●%s This installer needs root — it installs Docker, a system\n' "$RED" "$R"
  printf '    service, and a binary in /usr/local/bin.\n\n'
  printf '    Run it with %ssudo%s:\n\n' "$BOLD" "$R"
  printf '    %scurl -fsSL https://github.com/%s/releases/latest/download/install.sh | sudo bash%s\n\n' "$AMBER" "$REPO" "$R"
  exit 1
fi
ok "running as root"

case "$(uname -s)" in
  Linux)  OS="linux" ;;
  Darwin) OS="darwin" ;;
  *) die "RackOps runs on Linux and macOS. On Windows, install WSL2 first, then run this inside it: https://github.com/$REPO#windows" ;;
esac

case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) die "unsupported CPU architecture: $(uname -m)" ;;
esac
ASSET="rackops-$OS-$ARCH"
ok "$OS/$ARCH"

if command -v curl >/dev/null 2>&1; then
  FETCH="curl -fsSL"
elif command -v wget >/dev/null 2>&1; then
  FETCH="wget -qO-"
else
  die "need curl or wget installed"
fi

# ---------------------------------------------------------------------------
# Download & install the binary
# ---------------------------------------------------------------------------
section "Download"

# Resolve the release tag from the /latest redirect (cosmetic — best effort).
VERSION="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" 2>/dev/null | sed 's|.*/tag/||')" || VERSION=""

URL="https://github.com/$REPO/releases/latest/download/$ASSET"
TMP="$(mktemp)"
dl() {
  if command -v curl >/dev/null 2>&1; then curl -fSL "$URL" -o "$TMP"; else wget -q "$URL" -O "$TMP"; fi
}
step "downloading rackops ${VERSION:+$VERSION }($ASSET)…" dl || die "download failed: $URL"
chmod +x "$TMP"
install -m 0755 "$TMP" "$BIN"
rm -f "$TMP"
SIZE="$(du -h "$BIN" 2>/dev/null | cut -f1)"
ok "rackops ${VERSION:+$VERSION }→ $BIN ${DIM}(${SIZE:-?})${R}"

# ---------------------------------------------------------------------------
# Prerequisites — scan the machine, install whatever's missing.
#   git ................. deploy apps straight from a repo
#   docker (or podman) .. run app containers
#   docker compose ...... multi-service templates & stacks
# ---------------------------------------------------------------------------
section "Prerequisites"

PKG=""
detect_pkg() {
  [ -n "$PKG" ] && return 0
  for m in apt-get dnf yum apk pacman zypper; do
    if command -v "$m" >/dev/null 2>&1; then PKG="$m"; return 0; fi
  done
  return 1
}
pkg_install() {
  detect_pkg || return 1
  case "$PKG" in
    apt-get) apt-get update -y; env DEBIAN_FRONTEND=noninteractive apt-get install -y "$@" ;;
    dnf)     dnf install -y "$@" ;;
    yum)     yum install -y "$@" ;;
    apk)     apk add --no-cache "$@" ;;
    pacman)  pacman -Sy --noconfirm "$@" ;;
    zypper)  zypper --non-interactive install "$@" ;;
    *) return 1 ;;
  esac
}

runtime_ready() {
  if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then return 0; fi
  if command -v podman >/dev/null 2>&1 && podman info >/dev/null 2>&1; then return 0; fi
  return 1
}

start_docker() {
  if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    systemctl enable --now docker >/dev/null 2>&1 || systemctl start docker >/dev/null 2>&1 || true
  elif command -v docker >/dev/null 2>&1 && ! docker info >/dev/null 2>&1; then
    # No systemd (e.g. plain WSL2): run the daemon in the background.
    nohup dockerd >/var/log/rackops-dockerd.log 2>&1 &
    j=0; while [ "$j" -lt 15 ]; do docker info >/dev/null 2>&1 && break; j=$((j + 1)); sleep 1; done
  fi
}

install_docker() {
  # The official convenience script handles every major distro and brings the
  # compose + buildx plugins with it; it's non-interactive, so pipe-safe.
  $FETCH https://get.docker.com | sh
  start_docker
}

# git — repo deploys
if [ "$OS" = "darwin" ]; then
  # command -v finds Apple's /usr/bin/git shim even without the developer
  # tools; xcode-select -p is the real test. Never auto-install here — it
  # opens a GUI dialog, which a piped root shell can't drive.
  if xcode-select -p >/dev/null 2>&1 && command -v git >/dev/null 2>&1; then
    ok "git"
  else
    warn "git — install the Xcode command line tools: ${AMBER}xcode-select --install${R} ${DIM}(images & templates still work)${R}"
  fi
elif command -v git >/dev/null 2>&1; then
  ok "git"
elif step "installing git…" pkg_install git && command -v git >/dev/null 2>&1; then
  ok "git ${DIM}— installed${R}"
else
  warn "git — couldn't install; git-repo deploys unavailable ${DIM}(images & templates still work)${R}"
fi

# container runtime — the engine that runs your apps
if runtime_ready; then
  RT="docker"; command -v docker >/dev/null 2>&1 || RT="podman"
  ok "container runtime ${DIM}($RT)${R}"
elif command -v docker >/dev/null 2>&1 || command -v podman >/dev/null 2>&1; then
  if [ "$OS" = "darwin" ]; then
    warn "runtime installed but not running — start Docker Desktop (or ${AMBER}colima start${R}), then it's picked up automatically"
  else
    step "starting the container runtime…" start_docker || true
    if runtime_ready; then
      ok "container runtime ${DIM}— started${R}"
    else
      warn "runtime installed but not running — on WSL2, enable Docker Desktop's WSL integration"
    fi
  fi
elif [ "$OS" = "darwin" ]; then
  # No headless install path on macOS: get.docker.com is Linux-only and
  # Homebrew refuses to run as root. Point at the two sane options instead.
  warn "Docker not found — RackOps still starts (telemetry works), but app deploys need it"
  say  "    Install Docker Desktop: ${AMBER}https://www.docker.com/products/docker-desktop${R}"
  say  "    or, with Homebrew:      ${AMBER}brew install colima docker && colima start${R}"
else
  if step "installing Docker Engine… ${DIM}(takes a minute)${R}" install_docker && runtime_ready; then
    ok "docker ${DIM}— installed & running${R}"
    # Let the human who ran sudo use docker without sudo next time.
    if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ] && command -v usermod >/dev/null 2>&1; then
      usermod -aG docker "$SUDO_USER" >/dev/null 2>&1 || true
    fi
  else
    warn "Docker couldn't be set up — RackOps still starts (telemetry works), but"
    say  "    app deploys need it. Fix: ${AMBER}curl -fsSL https://get.docker.com | sh${R}"
  fi
fi

# compose plugin — multi-service templates (Immich, WordPress, Plausible…)
if command -v docker >/dev/null 2>&1; then
  if docker compose version >/dev/null 2>&1; then
    ok "docker compose"
  elif step "installing the compose plugin…" pkg_install docker-compose-plugin && docker compose version >/dev/null 2>&1; then
    ok "docker compose ${DIM}— installed${R}"
  else
    warn "compose plugin missing — multi-service templates won't deploy ${DIM}(single apps fine)${R}"
  fi
fi

# ---------------------------------------------------------------------------
# Launch
# ---------------------------------------------------------------------------
section "Launch"

if [ "$OS" = "darwin" ]; then
  # launchd is macOS's systemd: RunAtLoad starts it on boot, KeepAlive
  # restarts it on crash. bootout first so a re-run (upgrade) reloads the
  # daemon instead of failing on "service already bootstrapped".
  PLIST="/Library/LaunchDaemons/in.rackops.server.plist"
  cat > "$PLIST" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>in.rackops.server</string>
  <key>ProgramArguments</key>
  <array>
    <string>$BIN</string>
    <string>server</string>
    <string>--listen</string>
    <string>:$PORT</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/var/log/rackops.log</string>
  <key>StandardErrorPath</key><string>/var/log/rackops.log</string>
</dict>
</plist>
PLIST
  launchctl bootout system/in.rackops.server >/dev/null 2>&1 || true
  launchctl bootstrap system "$PLIST" || die "launchctl couldn't start the service — check /var/log/rackops.log"
  ok "launchd service ${DIM}— starts on boot, restarts on crash (logs: /var/log/rackops.log)${R}"
elif command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  cat > /etc/systemd/system/rackops.service <<UNIT
[Unit]
Description=RackOps control plane
After=network-online.target docker.service
Wants=network-online.target

[Service]
ExecStart=$BIN server --listen :$PORT
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl enable --now rackops >/dev/null 2>&1 || systemctl restart rackops
  ok "systemd service ${DIM}— starts on boot, restarts on crash${R}"
else
  # No systemd (e.g. plain WSL2): run detached in the background.
  pkill -f "$BIN server" 2>/dev/null || true
  mkdir -p /root/.rackops
  setsid env HOME=/root "$BIN" server --listen ":$PORT" >/root/.rackops/server.log 2>&1 < /dev/null &
  ok "running in the background ${DIM}— logs: /root/.rackops/server.log${R}"
fi

up() {
  i=0
  while [ "$i" -lt 30 ]; do
    if $FETCH "http://localhost:$PORT/api/auth/status" >/dev/null 2>&1; then return 0; fi
    i=$((i + 1)); sleep 1
  done
  return 1
}
if step "waiting for the control plane…" up; then
  ok "control plane is answering"
else
  warn "not answering yet — give it a few seconds, then open the URL below"
fi

# ---------------------------------------------------------------------------
# Done — the payoff card.
# ---------------------------------------------------------------------------
if [ "$OS" = "darwin" ]; then
  # hostname -I is a Linux-ism; ask the primary interfaces directly.
  LAN_IP="$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null)" || LAN_IP=""
else
  LAN_IP="$(hostname -I 2>/dev/null | awk '{print $1}')" || LAN_IP=""
fi

say ""
say "  ${DIM}╭──────────────────────────────────────────────╮${R}"
say "  ${DIM}│${R}                                              ${DIM}│${R}"
say "  ${DIM}│${R}   ${GREEN}●${R}  ${BOLD}RackOps is live${R}                         ${DIM}│${R}"
say "  ${DIM}│${R}                                              ${DIM}│${R}"
printf '  %s│%s      %s%-40s%s%s│%s\n' "$DIM" "$R" "$AMBER" "http://localhost:$PORT" "$R" "$DIM" "$R"
if [ -n "$LAN_IP" ]; then
  # Labelled "network", not "LAN": on a VPS this is the public IP and the
  # only address that works from the operator's own browser.
  printf '  %s│%s      %s%-40s%s%s│%s\n' "$DIM" "$R" "$DIM" "http://$LAN_IP:$PORT  (network)" "$R" "$DIM" "$R"
fi
say "  ${DIM}│${R}                                              ${DIM}│${R}"
say "  ${DIM}│${R}   Open it in your browser and create your    ${DIM}│${R}"
say "  ${DIM}│${R}   owner account. Add more machines from      ${DIM}│${R}"
say "  ${DIM}│${R}   Machines ${DIM}→${R} Add machine.                    ${DIM}│${R}"
say "  ${DIM}│${R}                                              ${DIM}│${R}"
say "  ${DIM}╰──────────────────────────────────────────────╯${R}"
say "     ${DIM}update later → re-run this same command${R}"
say ""
rm -f "$LOG"
