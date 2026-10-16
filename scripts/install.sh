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
# The host is firewalled: only SSH, 80 and 443 answer, and the swarm's own ports
# only to the nodes of the cluster. What the host already listened on before the
# install stays reachable (and is listed at the end). FIREWALL=off leaves the host
# as it was; FIREWALL_ALLOW="10050/tcp,53/udp" keeps more ports reachable.
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
FIREWALL=${FIREWALL:-on}
FIREWALL_ALLOW=${FIREWALL_ALLOW:-}
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
# Encrypted from the start: an overlay cannot be encrypted afterwards, only
# recreated, and on one node it costs nothing. A second server then talks over
# IPsec, which needs ESP between the nodes (the firewall allows it for them).
# Some images switch the ESP module off; an encrypted overlay then carries
# nothing between servers, so it is created plain there and the end says so.
ESP=yes
modprobe esp4 >/dev/null 2>&1 || grep -qw '^esp4' /proc/modules || ESP=no
if docker network inspect "$NET" >/dev/null 2>&1; then
	# The option is a flag: Docker stores it with an empty value, so ask whether
	# the key is there, not what it says.
	if ! docker network inspect "$NET" --format '{{json .Options}}' 2>/dev/null | grep -q '"encrypted"'; then
		NET_PLAIN=yes
	elif [ "$ESP" = no ]; then
		NET_BROKEN=yes
	fi
elif [ "$ESP" = no ]; then
	run docker network create --driver overlay --attachable "$NET"
	NET_PLAIN=yes
else
	run docker network create --driver overlay --opt encrypted --attachable "$NET"
fi
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

# --- Firewall ---
# What this host listens on besides SSH, 80 and 443, before the cluster does: it
# stays reachable, so that an install does not break what a server already did.
# The list is written down at the first install and used again after that, so
# that running this script again does not open what has appeared since.
FW_SSH=
FW_KEEP=
FW_NOTE=
if [ "$FIREWALL" = off ]; then
	FW_NOTE="FIREWALL=off: the host's firewall is left as it was"
elif ! command -v ss >/dev/null 2>&1; then
	FIREWALL=off
	FW_NOTE="no ss on this host, so the firewall was not set up (FIREWALL=off)"
else
	# The SSH port or ports, from sshd itself; 22 is always allowed as well.
	FW_SSH=$(ss -H -ltnp 2>/dev/null | awk '/"sshd"/ { n = split($4, a, ":"); print a[n] }' | sort -un | tr '\n' ',' | sed 's/,$//')
	if [ -f "$CONFIG/firewall-keep" ]; then
		FW_KEEP=$(cat "$CONFIG/firewall-keep")
	else
		FW_SKIP="22 80 443 2377 7946 4789 68 546 $(echo "$FW_SSH" | tr ',' ' ')"
		FW_KEEP=$(ss -H -ltnu 2>/dev/null | awk -v skip="$FW_SKIP" '
			BEGIN { n = split(skip, s, " "); for (i = 1; i <= n; i++) bad[s[i]] = 1 }
			{
				local = $5
				if (local ~ /^127\./ || local ~ /^\[::1\]/ || local ~ /%lo:/) next
				m = split(local, a, ":"); port = a[m]
				key = port "/" $1
				if (!(port in bad) && !(key in seen)) { seen[key] = 1; printf "%s%s", sep, key; sep = "," }
			}')
		echo "$FW_KEEP" >"$CONFIG/firewall-keep"
		chmod 600 "$CONFIG/firewall-keep"
	fi
	if [ -n "$FIREWALL_ALLOW" ]; then
		FW_KEEP=${FW_KEEP:+$FW_KEEP,}$(echo "$FIREWALL_ALLOW" | tr ' ' ',')
	fi
fi

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

if [ "$FIREWALL" = off ]; then
	FW_PANEL_ENV=""
	FW_SERVICE=""
	FW_NETWORK=""
else
	FW_PANEL_ENV="      PANEL_FIREWALL_SSH: \"$FW_SSH\"
      PANEL_FIREWALL_KEEP: \"$FW_KEEP\""
	# The host's network namespace and NET_ADMIN, no Docker socket, and nothing
	# listening: it asks the panel by its public address, since a service on the
	# host network is not on the overlay (ADR 9).
	FW_SERVICE="
  firewall:
    image: $IMAGE
    environment:
      PANEL_MODE: firewall
      PANEL_MODULE_SECRET: \"$SECRET\"
      PANEL_CORE_URL: \"$SCHEME://$DOMAIN\"
      PANEL_NODE: \"{{.Node.Hostname}}\"
    cap_add: [NET_ADMIN]
    networks: [hostnet]
    deploy:
      mode: global"
	FW_NETWORK="  hostnet:
    external: true
    name: host"
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
$FW_PANEL_ENV
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
$FW_SERVICE

networks:
  $NET:
    external: true
$FW_NETWORK

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

# The firewall is a service of its own with NET_ADMIN; a Docker that refuses
# that leaves the host as it was, which is worth saying rather than assuming.
FW_STATE=
if [ "$FIREWALL" != off ]; then
	i=0
	while [ "$i" -lt 15 ]; do
		[ "$(docker service ls --filter "name=${STACK}_firewall" --format '{{.Replicas}}' 2>/dev/null)" = 1/1 ] && { FW_STATE=ok; break; }
		i=$((i + 1))
		sleep 2
	done
fi

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

# "Running" is not "in force": the service asks the panel by its public address,
# and that can be a domain that does not resolve yet.
if [ "$FW_STATE" = ok ]; then
	FW_STATE=pending
	i=0
	while [ "$i" -lt 30 ]; do
		if docker service logs "${STACK}_firewall" 2>&1 | grep -q 'firewall: applied'; then
			FW_STATE=applied
			break
		fi
		i=$((i + 1))
		sleep 2
	done
fi

# The stack is deployed and the password is already in its compose file, so a
# timeout still shows it: the user would otherwise have a panel they cannot sign in to.
summary() {
	printf '  Address   %s%s://%s%s\n' "$BLUE" "$SCHEME" "$DOMAIN" "$OFF"
	if [ "$FIREWALL" = off ]; then
		echo "  Firewall  ${FW_NOTE:-off}"
	elif [ "$FW_STATE" = pending ]; then
		echo "  Firewall  started, but it has not applied its rules yet: it asks $SCHEME://$DOMAIN (docker service logs ${STACK}_firewall)"
	elif [ "$FW_STATE" = applied ]; then
		echo "  Firewall  closed except SSH${FW_SSH:+ ($FW_SSH)}, 80 and 443; the swarm's ports are for the cluster's nodes"
		[ -z "$FW_KEEP" ] || echo "            still reachable, as before the install: $FW_KEEP"
		echo "            to add a server: Nodes, Add a server (it asks for the server's address)"
	else
		echo "  Firewall  NOT in force: its service did not start (docker service ps ${STACK}_firewall --no-trunc)"
	fi
	if [ "${NET_BROKEN:-}" = yes ]; then
		echo "  Network   $NET is encrypted but this kernel has no IPsec (esp4): a second server will not reach it."
		echo "            recreate it without --opt encrypted before adding one"
	elif [ "${NET_PLAIN:-}" = yes ] && [ "$ESP" = no ]; then
		echo "  Network   $NET is not encrypted: this kernel has no IPsec (esp4). Traffic between servers"
		echo "            travels in the clear, so join servers over a private network or WireGuard (ADVERTISE_ADDR)"
	elif [ "${NET_PLAIN:-}" = yes ]; then
		echo "  Network   $NET was created earlier and is not encrypted; recreate it to encrypt traffic between servers"
	fi
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
