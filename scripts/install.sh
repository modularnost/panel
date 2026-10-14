#!/bin/sh
# Install modularnost on a fresh server: Docker, a one-node Swarm, Traefik with
# a Let's Encrypt certificate, and the panel behind it.
#
#   sh install.sh                                       asks for domain and email
#   DOMAIN=panel.example.com EMAIL=you@example.com sh install.sh   asks nothing
#
# Safe to re-run: an existing swarm, network or stack is reused, not recreated.
# Leave EMAIL empty to skip TLS and serve plain HTTP — for an internal network
# where Let's Encrypt cannot reach the host.
#
# The screen gets one line per step; the chatty output of Docker goes to
# /var/log/modularnost-install.log, and the tail of it is shown on failure.
set -eu

DOMAIN=${DOMAIN:-}
EMAIL_GIVEN=${EMAIL+yes}
EMAIL=${EMAIL:-}
VERSION=${PANEL_VERSION:-latest}
IMAGE=${PANEL_IMAGE:-ghcr.io/modularnost/panel:$VERSION}
STACK=${PANEL_STACK:-panel}
NET=${PANEL_NETWORK:-modularnost}
CONFIG=${PANEL_CONFIG:-/etc/modularnost}
ADVERTISE=${ADVERTISE_ADDR:-}
LOG=${PANEL_INSTALL_LOG:-/var/log/modularnost-install.log}
TOTAL=5
N=0

# Blue marks what the reader will want to copy. Only on a terminal, and not when
# NO_COLOR is set (https://no-color.org).
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	BLUE=$(printf '\033[34m')
	OFF=$(printf '\033[0m')
else
	BLUE=
	OFF=
fi

die() { echo "error: $*" >&2; exit 1; }
step() { N=$((N + 1)); echo "[$N/$TOTAL] $*"; }
warn() { echo "      warning: $*"; }

# Runs a command with its output in the log. A failure shows the end of the log,
# because that is where the reason is.
run() {
	"$@" >>"$LOG" 2>&1 || {
		tail -n 15 "$LOG" >&2
		die "'$*' failed; the full output is in $LOG"
	}
}

[ "$(id -u)" = 0 ] || die "run as root"
command -v curl >/dev/null 2>&1 || die "curl is required"
: >"$LOG"

# Read from the terminal, not stdin: under `curl ... | sh` stdin is the script.
# No terminal (CI, a pipe with no tty) means the variables must be set already.
ask() {
	[ -e /dev/tty ] || return 1
	printf '%s' "$1" >/dev/tty
	IFS= read -r REPLY </dev/tty || return 1
	echo "$REPLY"
}

# `curl | sudo sh` gives the script a pty of its own and forwards no keyboard
# input to it when stdin is a pipe: the questions below would show what is typed
# and never receive it. Run as root without sudo, or download the file first.
if [ -n "${SUDO_USER:-}" ] && [ ! -t 0 ] && { [ -z "$DOMAIN" ] || [ -z "$EMAIL_GIVEN" ]; }; then
	echo "note: if typing gets no response, press Ctrl+C and run it as root without sudo," >&2
	echo "      or: curl -fsSL <url> -o install.sh && sudo sh install.sh" >&2
fi

# A typo here ends up in a Traefik rule and an ACME order, and shows up three
# minutes later as a certificate that never arrives: refuse what is not a host name.
while :; do
	if [ -z "$DOMAIN" ]; then
		DOMAIN=$(ask "domain for the panel (e.g. panel.example.com): ") ||
			die "DOMAIN is required, e.g. DOMAIN=panel.example.com sh install.sh"
	fi
	case $DOMAIN in
	"") echo "the panel needs a domain — Traefik routes by host name" >&2 ;;
	*[!A-Za-z0-9.-]*) echo "not a valid host name: $DOMAIN (ASCII letters, digits, dots and hyphens only)" >&2 ;;
	*) break ;;
	esac
	DOMAIN=
done
if [ -z "$EMAIL_GIVEN" ]; then
	EMAIL=$(ask "email for the Let's Encrypt certificate (empty = plain HTTP): ") || EMAIL=
fi

if [ -n "$EMAIL" ]; then
	SCHEME=https
	PORT=443
else
	SCHEME=http
	PORT=80
fi

# --- DNS ---
# Let's Encrypt validates the domain from outside, and a wrong record burns
# attempts against its rate limits. A domain that resolves nowhere cannot work;
# one that resolves to another address may still be right (NAT, a proxy).
step "Checking DNS for $DOMAIN"
if [ -z "${SKIP_DNS_CHECK:-}" ] && command -v getent >/dev/null 2>&1; then
	RESOLVED=$(getent ahostsv4 "$DOMAIN" 2>/dev/null | awk '{print $1}' | sort -u)
	if [ -z "$RESOLVED" ]; then
		[ -z "$EMAIL" ] ||
			die "$DOMAIN does not resolve: create an A record for this server first (SKIP_DNS_CHECK=1 to ignore)"
		warn "$DOMAIN does not resolve from this server"
	else
		LOCAL=$(ip -4 -o addr show 2>/dev/null | awk '{print $4}' | cut -d/ -f1)
		MATCH=no
		for ip in $RESOLVED; do
			if echo "$LOCAL" | grep -qx "$ip"; then MATCH=yes; fi
		done
		if [ -n "$LOCAL" ] && [ "$MATCH" = no ]; then
			warn "$DOMAIN points to $(echo $RESOLVED), which is not an address of this server"
			warn "fine behind NAT or a proxy; otherwise the certificate will not be issued"
		fi
	fi
fi

# --- Docker ---
if command -v docker >/dev/null 2>&1; then
	step "Docker is already installed"
else
	step "Installing Docker"
	GET_DOCKER=$(mktemp)
	run curl -fsSL https://get.docker.com -o "$GET_DOCKER"
	run sh "$GET_DOCKER"
fi
docker info >/dev/null 2>&1 || die "the Docker daemon is not running"

# --- Swarm ---
step "Preparing the swarm and the $NET network"
if [ "$(docker info --format '{{.Swarm.LocalNodeState}}')" != active ]; then
	if [ -z "$ADVERTISE" ]; then
		# Whatever address this host uses to reach the internet is the one other
		# nodes will be told to connect to. Taken from the word after "src": its
		# position shifts when the route has no gateway ("via"), as on many VPS.
		ADVERTISE=$(ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([^ ]*\).*/\1/p')
	fi
	[ -n "$ADVERTISE" ] || die "could not detect an address; set ADVERTISE_ADDR"
	run docker swarm init --advertise-addr "$ADVERTISE"
fi
docker network inspect "$NET" >/dev/null 2>&1 ||
	run docker network create --driver overlay --attachable "$NET"
# Traefik reaches the panel from this network, so its address is the one whose
# X-Forwarded-For the panel may believe; nothing outside the network is trusted.
NET_SUBNETS=$(docker network inspect "$NET" --format '{{range .IPAM.Config}}{{.Subnet}} {{end}}' | tr -s ' ' ',' | sed 's/,$//')

# --- Secrets ---
# The module secret must survive a re-run: the agents authenticate with it.
mkdir -p "$CONFIG"
chmod 700 "$CONFIG"
if [ ! -f "$CONFIG/secret" ]; then
	head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' >"$CONFIG/secret"
	chmod 600 "$CONFIG/secret"
fi
SECRET=$(cat "$CONFIG/secret")

# The admin password only matters on the very first start; after that the
# database has users and the panel ignores it.
FIRST_RUN=no
docker volume inspect "${STACK}_panel-data" >/dev/null 2>&1 || FIRST_RUN=yes
ADMIN_PASSWORD=${PANEL_ADMIN_PASSWORD:-$(head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')}

# --- Compose ---
# Written here rather than downloaded: one file to review, and the domain has to
# be substituted anyway. stack.yml in the repository is the simpler variant that
# publishes port 8080 directly.
if [ -n "$EMAIL" ]; then
	ENTRYPOINT=websecure
	TLS_LABELS="        - traefik.http.routers.panel.tls=true
        - traefik.http.routers.panel.tls.certresolver=le"
	ACME_ARGS="      - --certificatesresolvers.le.acme.email=$EMAIL
      - --certificatesresolvers.le.acme.storage=/acme/acme.json
      - --certificatesresolvers.le.acme.httpchallenge.entrypoint=web
      - --entrypoints.web.http.redirections.entrypoint.to=websecure"
else
	ENTRYPOINT=web
	TLS_LABELS=""
	ACME_ARGS=""
fi

cat >"$CONFIG/stack.yml" <<COMPOSE
services:
  traefik:
    image: traefik:v3
    command:
      - --providers.swarm=true
      - --providers.swarm.network=$NET
      - --providers.swarm.exposedbydefault=false
      - --entrypoints.web.address=:80
      - --entrypoints.websecure.address=:443
$ACME_ARGS
    # host mode: through the ingress mesh every client looks like the mesh's
    # own address, which defeats anything that keys on the client IP.
    ports:
      - {target: 80, published: 80, mode: host}
      - {target: 443, published: 443, mode: host}
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - acme:/acme
    networks: [$NET]
    deploy:
      placement:
        constraints: [node.role == manager]

  panel:
    image: $IMAGE
    environment:
      PANEL_ADMIN_PASSWORD: "$ADMIN_PASSWORD"
      PANEL_MODULE_SECRET: "$SECRET"
      PANEL_AGENT_SERVICE: ${STACK}_agent
      PANEL_STACK_FILE: /config/stack.yml
      PANEL_TRUSTED_PROXIES: "$NET_SUBNETS"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - panel-data:/data
      # So that an update from the panel can keep this file in step.
      - $CONFIG:/config
    networks: [$NET]
    deploy:
      replicas: 1
      placement:
        constraints: [node.role == manager]
      labels:
        - traefik.enable=true
        - traefik.http.routers.panel.rule=Host(\`$DOMAIN\`)
        - traefik.http.routers.panel.entrypoints=$ENTRYPOINT
$TLS_LABELS
        - traefik.http.services.panel.loadbalancer.server.port=8080

  agent:
    image: $IMAGE
    environment:
      PANEL_MODE: agent
      PANEL_MODULE_SECRET: "$SECRET"
      PANEL_NODE: "{{.Node.Hostname}}"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    networks: [$NET]
    deploy:
      mode: global

networks:
  $NET:
    external: true

volumes:
  panel-data:
  acme:
COMPOSE
chmod 600 "$CONFIG/stack.yml"

step "Deploying the panel from $IMAGE"
run docker stack deploy --detach=true -c "$CONFIG/stack.yml" "$STACK"

# --- Wait ---
# Asked on this host with the domain pinned to localhost: it does not depend on
# the record or on NAT hairpinning, and over https a valid certificate has to
# have been issued before the answer comes — which is what "ready" means here.
# wait_for_panel polls for up to $1 tries, two seconds apart.
wait_for_panel() {
	i=0
	while [ "$i" -lt "$1" ]; do
		if curl -fsS --max-time 5 -o /dev/null --resolve "$DOMAIN:$PORT:127.0.0.1" \
			"$SCHEME://$DOMAIN/healthz" >>"$LOG" 2>&1; then
			return 0
		fi
		i=$((i + 1))
		sleep 2
	done
	return 1
}

acme_failed() {
	docker service logs --tail 50 "${STACK}_traefik" 2>&1 | grep -q "Unable to obtain ACME certificate"
}

step "Waiting for the panel to answer (up to 3 minutes)"
READY=no
ACME_FAILED=no
if wait_for_panel 90; then READY=yes; fi
# Traefik asks Let's Encrypt once and does not try again by itself. A reset
# right after Docker was installed went away within the minute, so one more
# try is worth it; Let's Encrypt limits failed validations, so only one.
if [ "$READY" != yes ] && [ "$SCHEME" = https ] && acme_failed; then
	ACME_FAILED=yes
	warn "Traefik could not get a certificate; asking it to try once more"
	run docker service update --force --detach=true "${STACK}_traefik"
	if wait_for_panel 30; then READY=yes; fi
fi

# The stack is deployed and the password is already in its compose file, so a
# timeout still shows it: the user would otherwise have a panel they cannot sign in to.
summary() {
	printf '  Address   %s%s://%s%s\n' "$BLUE" "$SCHEME" "$DOMAIN" "$OFF"
	if [ "$FIRST_RUN" = yes ]; then
		printf '  Login     %sadmin%s\n' "$BLUE" "$OFF"
		printf '  Password  %s%s%s   (shown once; change it on the Users page)\n' "$BLUE" "$ADMIN_PASSWORD" "$OFF"
	else
		echo "  Existing installation updated; your users are unchanged."
	fi
	echo
	echo "  Logs      docker service logs ${STACK}_panel"
	echo "  Remove    curl -fsSL https://raw.githubusercontent.com/modularnost/panel/main/scripts/uninstall.sh | sh"
}

if [ "$READY" != yes ]; then
	echo >&2
	echo "The panel did not answer. The last lines of Traefik's log:" >&2
	docker service logs --tail 10 "${STACK}_traefik" 2>&1 | sed 's/^/    /' >&2 || true
	echo >&2
	if [ "$ACME_FAILED" = yes ] || { [ "$SCHEME" = https ] && acme_failed; }; then
		echo "Traefik could not get a certificate from Let's Encrypt. That is the network" >&2
		echo "between this host and Let's Encrypt, not the panel. Once it works, run:" >&2
		echo "    docker service update --force ${STACK}_traefik" >&2
		echo >&2
	fi
	echo "It may still come up, a certificate can take longer. Sign-in details:" >&2
	echo >&2
	summary >&2
	echo >&2
	echo "More: docker service ps ${STACK}_panel --no-trunc" >&2
	echo "      full install log: $LOG" >&2
	exit 1
fi

echo
echo "The panel is ready."
echo
summary
