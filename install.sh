#!/usr/bin/env bash
# Clachan installer, updater, and uninstaller for Linux.
#
# Install or update:
#   curl -fsSL https://github.com/SHOWTEX/ClaChan/releases/latest/download/install.sh | sudo bash
#
# Uninstall (keeps the database and configuration):
#   curl -fsSL https://github.com/SHOWTEX/ClaChan/releases/latest/download/install.sh | sudo bash -s -- --uninstall
#
# For a fork or a local test, pass --repo owner/repo or set CLACHAN_REPO.
#
# The target must be a systemd Linux host (Debian/Ubuntu/Raspberry Pi OS).
set -euo pipefail

REPO="${CLACHAN_REPO:-SHOWTEX/ClaChan}"
PREFIX="${CLACHAN_PREFIX:-/opt/clachan}"
CONFIG_DIR="${CLACHAN_CONFIG_DIR:-/etc/clachan}"
DATA_DIR="${CLACHAN_DATA_DIR:-$PREFIX/data}"
SERVICE_NAME="${CLACHAN_SERVICE:-clachan}"
SERVICE_USER="${CLACHAN_USER:-clachan}"
RELEASE="${CLACHAN_RELEASE:-latest}"
PORT="${CLACHAN_PORT:-7654}"

MODE="install"
NO_START=0
PURGE=0
ASSUME_YES=0

usage() {
	cat <<'EOF'
Clachan installer for Linux

Usage:
  install.sh [options]

Options:
  --update             Update an existing installation.
  --uninstall          Stop and remove the service and binary.
  --purge              With --uninstall, also delete config, data, and user.
  --no-start           Install or update without starting the service.
  --yes, -y            Answer yes to confirmation prompts.
  --repo OWNER/REPO    GitHub repository to download from.
  --release VERSION    Release tag to install (default: latest).
  --port PORT           Port for a new configuration (default: 7654).
  -h, --help           Show this help.

Environment:
  CLACHAN_REPO, CLACHAN_RELEASE, CLACHAN_API_KEY, CLACHAN_PORT
  CLACHAN_PREFIX, CLACHAN_CONFIG_DIR, CLACHAN_DATA_DIR
  CLACHAN_SERVICE, CLACHAN_USER
EOF
}

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

while [ "$#" -gt 0 ]; do
	case "$1" in
	--update)
		MODE="update"
		;;
	--uninstall)
		MODE="uninstall"
		;;
	--purge)
		PURGE=1
		;;
	--no-start)
		NO_START=1
		;;
	--yes | -y)
		ASSUME_YES=1
		;;
	--repo)
		[ "$#" -ge 2 ] || die "--repo needs a value"
		REPO="$2"
		shift
		;;
	--repo=*)
		REPO="${1#*=}"
		;;
	--release)
		[ "$#" -ge 2 ] || die "--release needs a value"
		RELEASE="$2"
		shift
		;;
	--release=*)
		RELEASE="${1#*=}"
		;;
	--port)
		[ "$#" -ge 2 ] || die "--port needs a value"
		PORT="$2"
		shift
		;;
	--port=*)
		PORT="${1#*=}"
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		die "unknown option: $1 (use --help)"
		;;
	esac
	shift
done

[ "$(id -u)" -eq 0 ] || die "run as root: curl ... | sudo bash"
[ "$(uname -s)" = "Linux" ] || die "this installer is for Linux"
if [ "$PURGE" -eq 1 ] && [ "$MODE" != "uninstall" ]; then
	die "--purge is only valid with --uninstall"
fi
case "$PORT" in
'' | *[!0-9]*) die "invalid port: $PORT" ;;
esac
[ "$PORT" -ge 1 ] && [ "$PORT" -le 65535 ] || die "invalid port: $PORT"
if [ "$MODE" = "uninstall" ] && [ "$PURGE" -eq 0 ] && [ "$ASSUME_YES" -eq 0 ]; then
	if [ -r /dev/tty ]; then
		printf 'Remove the Clachan service and binary, keeping data? [Y/n] ' >/dev/tty
		answer=""
		IFS= read -r answer < /dev/tty || true
		case "$answer" in
		n | N | no | NO) die "aborted" ;;
		esac
	else
		die "--yes is required when no terminal is available"
	fi
fi

SERVICE_FILE="/etc/systemd/system/$SERVICE_NAME.service"
CONFIG_FILE="$CONFIG_DIR/clachan.env"
EXE="$PREFIX/clachan"

uninstall() {
	if [ "$PURGE" -eq 1 ] && [ "$ASSUME_YES" -ne 1 ]; then
		if [ -r /dev/tty ]; then
			printf 'Also delete %s and %s, and remove user %s? [y/N] ' "$CONFIG_DIR" "$DATA_DIR" "$SERVICE_USER" >/dev/tty
			answer=""
			IFS= read -r answer < /dev/tty || true
			case "$answer" in
			y | Y | yes | YES) ;;
			*) die "aborted" ;;
			esac
		else
			die "--yes is required when no terminal is available"
		fi
	fi

	systemctl disable --now "$SERVICE_NAME" >/dev/null 2>&1 || true
	rm -f "$SERVICE_FILE"
	systemctl daemon-reload >/dev/null 2>&1 || true
	rm -f "$EXE"

	if [ "$PURGE" -eq 1 ]; then
		rm -rf "$PREFIX" "$CONFIG_DIR" "$DATA_DIR"
		userdel "$SERVICE_USER" >/dev/null 2>&1 || true
		groupdel "$SERVICE_USER" >/dev/null 2>&1 || true
		printf 'Removed Clachan, its configuration, its data, and the %s user.\n' "$SERVICE_USER"
		return
	fi

	printf 'Removed the Clachan service and binary.\n'
	printf 'Kept:\n  %s\n  %s\n' "$CONFIG_FILE" "$DATA_DIR"
	printf 'Use --uninstall --purge to delete those as well.\n'
}

if [ "$MODE" = "uninstall" ]; then
	uninstall
	exit 0
fi

for cmd in curl systemctl install groupadd useradd getent awk sed; do
	command -v "$cmd" >/dev/null 2>&1 || die "missing required command: $cmd"
done
[ -d /run/systemd/system ] || die "systemd is required; see the manual deployment section in README.md"
if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$@"; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$@"; }
else
	die "missing sha256sum or shasum"
fi

port_in_use() {
	local port="$1"
	if command -v ss >/dev/null 2>&1; then
		ss -H -ltn "sport = :$port" 2>/dev/null | grep -q .
		return $?
	fi
	if command -v lsof >/dev/null 2>&1; then
		lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1
		return $?
	fi
	return 1
}

case "$REPO" in
*/*) ;;
*) die "repository must look like OWNER/REPO; pass --repo OWNER/REPO" ;;
esac

case "$(uname -m)" in
x86_64 | amd64) ARCH="amd64" ;;
aarch64 | arm64) ARCH="arm64" ;;
*) die "unsupported architecture: $(uname -m) (supported: amd64, arm64)" ;;
esac
ASSET="clachan-linux-$ARCH"
PROJECT_URL="https://github.com/$REPO"

if [ -n "${CLACHAN_BASE_URL:-}" ]; then
	BASE="${CLACHAN_BASE_URL%/}"
elif [ "$RELEASE" = "latest" ]; then
	BASE="https://github.com/$REPO/releases/latest/download"
else
	TAG="$RELEASE"
	case "$TAG" in v*) ;; *) TAG="v$TAG" ;; esac
	BASE="https://github.com/$REPO/releases/download/$TAG"
fi
LATEST_BASE="https://github.com/$REPO/releases/latest/download"

TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

fetch() {
	if [ "${BASE#https://}" != "$BASE" ]; then
		curl -fsSL --retry 3 --proto '=https' --tlsv1.2 -A clachan-installer -o "$2" "$1"
	else
		curl -fsSL --retry 3 -A clachan-installer -o "$2" "$1"
	fi
}

printf '==> Downloading %s\n' "$ASSET"
fetch "$BASE/$ASSET" "$TMP_DIR/$ASSET"
fetch "$BASE/checksums.txt" "$TMP_DIR/checksums.txt"

EXPECTED="$(awk -v name="$ASSET" '$2 == name { print $1; exit }' "$TMP_DIR/checksums.txt")"
[ -n "$EXPECTED" ] || die "no checksum found for $ASSET"
ACTUAL="$(sha256 "$TMP_DIR/$ASSET" | awk '{ print $1 }')"
[ "$ACTUAL" = "$EXPECTED" ] || die "checksum mismatch for $ASSET"
chmod 0755 "$TMP_DIR/$ASSET"
APP_VERSION="$("$TMP_DIR/$ASSET" --version 2>/dev/null)" || die "downloaded binary failed to run"

installed=0
[ -x "$EXE" ] && installed=1
if [ "$MODE" = "update" ] && [ "$installed" -ne 1 ]; then
	die "no existing installation at $PREFIX"
fi
if [ "$MODE" = "install" ] && [ "$installed" -eq 1 ]; then
	printf '==> Existing installation found; updating\n'
	MODE="update"
fi

if [ -f "$SERVICE_FILE" ]; then
	printf '==> Stopping existing service\n'
	systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
	systemctl reset-failed "$SERVICE_NAME" >/dev/null 2>&1 || true
fi

PORT_NOW="$PORT"
ADDR_LINE="$(grep -E '^CLACHAN_ADDR=' "$CONFIG_FILE" 2>/dev/null | tail -n 1 || true)"
if [ -n "$ADDR_LINE" ]; then
	ADDR="${ADDR_LINE#CLACHAN_ADDR=}"
	case "$ADDR" in
	:*) PORT_NOW="${ADDR#:}" ;;
	*:*) PORT_NOW="${ADDR##*:}" ;;
	esac
fi

if [ "$NO_START" -eq 0 ] && port_in_use "$PORT_NOW"; then
	printf '==> Port %s is already in use:\n' "$PORT_NOW" >&2
	if command -v ss >/dev/null 2>&1; then
		ss -ltnp "sport = :$PORT_NOW" >&2 || true
	elif command -v lsof >/dev/null 2>&1; then
		lsof -nP -iTCP:"$PORT_NOW" -sTCP:LISTEN >&2 || true
	fi
	if [ -f "$CONFIG_FILE" ]; then
		die "stop the conflicting service or change CLACHAN_ADDR in $CONFIG_FILE"
	fi
	die "stop the conflicting service or re-run with --port <free-port>"
fi

printf '==> Preparing service user and directories\n'
if ! getent group "$SERVICE_USER" >/dev/null 2>&1; then
	groupadd --system "$SERVICE_USER"
fi
if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
	useradd --system --gid "$SERVICE_USER" --home-dir "$PREFIX" --shell /usr/sbin/nologin "$SERVICE_USER"
fi
install -d -m 0755 -o "$SERVICE_USER" -g "$SERVICE_USER" "$PREFIX" "$DATA_DIR"
install -d -m 0750 -o root -g "$SERVICE_USER" "$CONFIG_DIR"

if [ ! -f "$CONFIG_FILE" ]; then
	printf '==> Creating %s\n' "$CONFIG_FILE"
	cat >"$CONFIG_FILE" <<EOF
# Clachan configuration. Edit this file, then:
#   sudo systemctl restart $SERVICE_NAME
OPENROUTER_API_KEY=
CLACHAN_ADDR=:$PORT
CLACHAN_DB=$DATA_DIR/clachan.db
OPENROUTER_REFERER=$PROJECT_URL
OPENROUTER_TITLE=Clachan
EOF
	chown root:"$SERVICE_USER" "$CONFIG_FILE"
	chmod 0640 "$CONFIG_FILE"
fi

if grep -qx 'OPENROUTER_REFERER=http://clachan.local' "$CONFIG_FILE" 2>/dev/null; then
	REF="$PROJECT_URL" awk 'BEGIN { ref = ENVIRON["REF"] } /^OPENROUTER_REFERER=/ { print "OPENROUTER_REFERER=" ref; next } { print }' "$CONFIG_FILE" >"$CONFIG_FILE.tmp"
	chown root:"$SERVICE_USER" "$CONFIG_FILE.tmp"
	chmod 0640 "$CONFIG_FILE.tmp"
	mv "$CONFIG_FILE.tmp" "$CONFIG_FILE"
fi

KEY="${CLACHAN_API_KEY:-}"
if [ -z "$KEY" ] && ! grep -Eq '^OPENROUTER_API_KEY=.+' "$CONFIG_FILE" 2>/dev/null; then
	if [ -r /dev/tty ]; then
		printf 'OpenRouter API key (leave blank to set later): ' >/dev/tty
		IFS= read -r KEY < /dev/tty || KEY=""
	fi
fi
if [ -n "$KEY" ]; then
	KEY="$KEY" awk 'BEGIN { key = ENVIRON["KEY"] } /^OPENROUTER_API_KEY=/ { print "OPENROUTER_API_KEY=" key; next } { print }' "$CONFIG_FILE" >"$CONFIG_FILE.tmp"
	chown root:"$SERVICE_USER" "$CONFIG_FILE.tmp"
	chmod 0640 "$CONFIG_FILE.tmp"
	mv "$CONFIG_FILE.tmp" "$CONFIG_FILE"
fi

printf '==> Installing %s\n' "$APP_VERSION"
install -m 0755 "$TMP_DIR/$ASSET" "$EXE.new"
mv -f "$EXE.new" "$EXE"

if [ -f "$SERVICE_FILE" ] && ! grep -q 'Managed by the Clachan installer' "$SERVICE_FILE"; then
	printf '==> Keeping existing service unit %s\n' "$SERVICE_FILE"
else
	cat >"$SERVICE_FILE" <<EOF
# Managed by the Clachan installer. Re-run the installer to update.
[Unit]
Description=Clachan
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
WorkingDirectory=$PREFIX
EnvironmentFile=$CONFIG_FILE
ExecStart=$EXE
Restart=on-failure
RestartSec=3
TimeoutStopSec=15
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$DATA_DIR

[Install]
WantedBy=multi-user.target
EOF
fi
systemctl daemon-reload

if [ "$NO_START" -eq 0 ]; then
	systemctl reset-failed "$SERVICE_NAME" >/dev/null 2>&1 || true
	printf '==> Starting service\n'
	systemctl enable "$SERVICE_NAME" >/dev/null 2>&1 || true
	systemctl restart "$SERVICE_NAME"
	healthy=0
	for _ in $(seq 1 40); do
		if curl -fsS --max-time 1 "http://127.0.0.1:$PORT_NOW/healthz" >/dev/null 2>&1; then
			healthy=1
			break
		fi
		sleep 0.25
	done
	if [ "$healthy" -ne 1 ]; then
		journalctl -u "$SERVICE_NAME" -n 40 --no-pager >&2 || true
		die "service did not become healthy"
	fi
fi

IP_ADDR="$(hostname -I 2>/dev/null | awk '{ print $1 }')"
[ -n "$IP_ADDR" ] || IP_ADDR="$(hostname)"

printf '\nInstalled: %s\n' "$APP_VERSION"
printf '  URL:     http://%s:%s\n' "$IP_ADDR" "$PORT_NOW"
printf '  Config:  %s\n' "$CONFIG_FILE"
printf '  Data:    %s\n' "$DATA_DIR"
if [ "$NO_START" -eq 0 ]; then
	printf '  Service: sudo systemctl status %s\n' "$SERVICE_NAME"
else
	printf '  Start:   sudo systemctl enable --now %s\n' "$SERVICE_NAME"
fi
if ! grep -Eq '^OPENROUTER_API_KEY=.+' "$CONFIG_FILE" 2>/dev/null; then
	printf '\nSet OPENROUTER_API_KEY in %s, then restart the service:\n' "$CONFIG_FILE"
	printf '  sudo systemctl restart %s\n' "$SERVICE_NAME"
fi
printf '\nUpdate:  curl -fsSL %s/install.sh | sudo bash\n' "$LATEST_BASE"
printf 'Uninstall: curl -fsSL %s/install.sh | sudo bash -s -- --uninstall\n' "$LATEST_BASE"
