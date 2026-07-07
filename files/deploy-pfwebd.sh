#!/bin/ksh
#
# deploy-pfwebd.sh — install or upgrade pfwebd on an OpenBSD host.
#
# Idempotent: safe to re-run to upgrade the binary or refresh the config.
# Run as root (or via doas) from a checkout of the repository:
#
#   doas files/deploy-pfwebd.sh            # default 127.0.0.1:8080
#   doas files/deploy-pfwebd.sh -a 0.0.0.0:8080 -t
#
# Options:
#   -a ADDR   listen address host:port (default 127.0.0.1:8080)
#   -t        generate an initial write token and add it to the tokens file
#   -b        force rebuilding the binary from source (needs Go)
#   -h        show this help

set -eu

ADDR="127.0.0.1:8080"
GEN_TOKEN=0
FORCE_BUILD=0

usage() {
	sed -n '3,15p' "$0" | sed 's/^# \{0,1\}//'
}

while getopts "a:tbh" opt; do
	case "$opt" in
	a) ADDR="$OPTARG" ;;
	t) GEN_TOKEN=1 ;;
	b) FORCE_BUILD=1 ;;
	h) usage; exit 0 ;;
	*) usage; exit 1 ;;
	esac
done

if [ "$(id -u)" -ne 0 ]; then
	echo "error: must run as root (try: doas $0 ...)" >&2
	exit 1
fi

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)

DAEMON_USER="_pfwebd"
BIN="/usr/local/sbin/pfwebd"
WRAPPER="/usr/local/sbin/pfwebd-table"
STATE_DIR="/var/db/pfwebd"
CONF_DIR="/etc/pfwebd"
TOKENS="$CONF_DIR/tokens"
RULES_FILE="$STATE_DIR/rules.conf"
SRC_BIN="$REPO_ROOT/pfwebd"

echo "==> Locating / building the pfwebd binary"
if [ "$FORCE_BUILD" -eq 1 ] || [ ! -x "$SRC_BIN" ]; then
	if command -v go >/dev/null 2>&1; then
		# -buildvcs=false: running under doas, git refuses to stamp a repo
		# owned by another user (exit 128); we do not need VCS stamping here.
		( cd "$REPO_ROOT" && GOOS=openbsd GOARCH="$(uname -m)" go build -buildvcs=false -o pfwebd ./cmd/pfwebd )
		echo "    built $SRC_BIN"
	else
		echo "error: Go is not installed and no prebuilt binary at $SRC_BIN" >&2
		echo "       build it on another machine with:" >&2
		echo "         GOOS=openbsd GOARCH=$(uname -m) go build -o pfwebd ./cmd/pfwebd" >&2
		echo "       then copy it to $SRC_BIN and re-run this script." >&2
		exit 1
	fi
else
	echo "    using existing $SRC_BIN"
fi

echo "==> Creating the $DAEMON_USER user"
if id "$DAEMON_USER" >/dev/null 2>&1; then
	echo "    already exists"
else
	useradd -s /sbin/nologin -d /var/empty -L daemon "$DAEMON_USER"
fi

echo "==> Installing binaries"
install -o root -g bin -m 755 "$SRC_BIN" "$BIN"
install -o root -g bin -m 755 "$SCRIPT_DIR/pfwebd-table" "$WRAPPER"

echo "==> Authorizing pfctl commands in /etc/doas.conf"
MARKER="# --- pfwebd (managed by deploy-pfwebd.sh) ---"
if grep -qF "$MARKER" /etc/doas.conf 2>/dev/null; then
	echo "    pfwebd block already present, skipping"
else
	{ echo ""; echo "$MARKER"; cat "$SCRIPT_DIR/doas.conf"; } >> /etc/doas.conf
	echo "    appended entries from $SCRIPT_DIR/doas.conf"
fi

echo "==> Creating state and config directories"
install -d -o "$DAEMON_USER" -g "$DAEMON_USER" -m 700 "$STATE_DIR"
install -d -o root -g wheel -m 755 "$CONF_DIR"
if [ ! -e "$TOKENS" ]; then
	install -o "$DAEMON_USER" -g "$DAEMON_USER" -m 600 /dev/null "$TOKENS"
	echo "    created empty $TOKENS"
fi

if [ "$GEN_TOKEN" -eq 1 ]; then
	echo "==> Generating an initial write token"
	tok=$("$BIN" -gen-token | awk '/pfw_/ {print $1; exit}')
	hash=$(printf '%s' "$tok" | "$BIN" -hash-token)
	printf 'terraform  write  %s\n' "$hash" >> "$TOKENS"
	chown "$DAEMON_USER:$DAEMON_USER" "$TOKENS"
	chmod 600 "$TOKENS"
	echo "    added token 'terraform' (write) to $TOKENS"
	echo "    >>> store this token now, it is not saved anywhere: $tok"
fi

echo "==> Installing the rc.d service"
install -m 555 "$SCRIPT_DIR/rc.d/pfwebd" /etc/rc.d/pfwebd

echo "==> Enabling the daemon"
rcctl enable pfwebd
echo "==> Set flags for the daemon"
rcctl set pfwebd flags "-addr $ADDR -rules-file $RULES_FILE -tokens-file $TOKENS"
echo "==> Restarting the daemon"
rcctl -d restart pfwebd
echo "==> Check the daemon"
rcctl check pfwebd

echo "==> Checking /etc/pf.conf"
if ! grep -Eq 'table[[:space:]]*<blocklist>[[:space:]]*persist' /etc/pf.conf 2>/dev/null; then
	echo "" >> /etc/pf.conf
	echo "# pfwebd managed tables (added by deploy-pfwebd.sh)" >> /etc/pf.conf
	echo "table <blocklist> persist" >> /etc/pf.conf
	echo "table <allowlist> persist" >> /etc/pf.conf
	echo "    added 'table <blocklist> persist' and 'table <allowlist> persist' to /etc/pf.conf"
fi
if ! grep -q 'anchor "pfwebd"' /etc/pf.conf 2>/dev/null; then
	echo "" >> /etc/pf.conf
	echo "# pfwebd anchor (added by deploy-pfwebd.sh)" >> /etc/pf.conf
	echo 'anchor "pfwebd"' >> /etc/pf.conf
	echo "    added 'anchor \"pfwebd\"' to /etc/pf.conf"
fi

echo
echo "Done. pfwebd is listening on http://$ADDR"
echo "Reach it from the admin network via -addr or, better, behind relayd/httpd with TLS."
echo "Edit $TOKENS then 'rcctl reload pfwebd' to rotate tokens without a restart."
