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

die() { echo "error: $*" >&2; exit 1; }
say() { echo "==> $*"; }

[ "$(id -u)" = 0 ] || die "run as root"

# Read from the terminal, not stdin: under `curl ... | sh` stdin is the script.
# No terminal (CI, a pipe with no tty) means the variables must be set already.
ask() {
	[ -e /dev/tty ] || return 1
	printf '%s' "$1" >/dev/tty
	IFS= read -r REPLY </dev/tty || return 1
	echo "$REPLY"
}

while [ -z "$DOMAIN" ]; do
	DOMAIN=$(ask "domain for the panel (e.g. panel.example.com): ") ||
		die "DOMAIN is required, e.g. DOMAIN=panel.example.com sh install.sh"
	[ -n "$DOMAIN" ] || echo "the panel needs a domain — Traefik routes by host name" >&2
done
if [ -z "$EMAIL_GIVEN" ]; then
	EMAIL=$(ask "email for the Let's Encrypt certificate (empty = plain HTTP): ") || EMAIL=
fi

# --- Docker ---
if ! command -v docker >/dev/null 2>&1; then
	say "installing Docker"
	curl -fsSL https://get.docker.com | sh
fi
docker info >/dev/null 2>&1 || die "the Docker daemon is not running"

# --- Swarm ---
if [ "$(docker info --format '{{.Swarm.LocalNodeState}}')" != active ]; then
	if [ -z "$ADVERTISE" ]; then
		# Whatever address this host uses to reach the internet is the one other
		# nodes will be told to connect to.
		# Taken from the word after "src": its position shifts when the route
		# has no gateway ("via"), as on many VPS images.
		ADVERTISE=$(ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([^ ]*\).*/\1/p')
	fi
	[ -n "$ADVERTISE" ] || die "could not detect an address; set ADVERTISE_ADDR"
	say "initialising the swarm on $ADVERTISE"
	docker swarm init --advertise-addr "$ADVERTISE" >/dev/null
fi

docker network inspect "$NET" >/dev/null 2>&1 ||
	{ say "creating the $NET network"; docker network create --driver overlay --attachable "$NET" >/dev/null; }

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
	TLS_LABELS="      - traefik.http.routers.panel.tls=true
      - traefik.http.routers.panel.tls.certresolver=le"
	ACME_ARGS="      - --certificatesresolvers.le.acme.email=$EMAIL
      - --certificatesresolvers.le.acme.storage=/acme/acme.json
      - --certificatesresolvers.le.acme.httpchallenge.entrypoint=web
      - --entrypoints.web.http.redirections.entrypoint.to=websecure"
else
	ENTRYPOINT=web
	TLS_LABELS=""
	ACME_ARGS=""
	say "EMAIL is empty: serving plain HTTP, no certificate"
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
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - panel-data:/data
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

say "deploying the $STACK stack from $IMAGE"
docker stack deploy --detach=true -c "$CONFIG/stack.yml" "$STACK"

echo
if [ -n "$EMAIL" ]; then
	say "panel: https://$DOMAIN"
else
	say "panel: http://$DOMAIN"
fi
if [ "$FIRST_RUN" = yes ]; then
	say "sign in as admin with password: $ADMIN_PASSWORD"
	say "this is shown once — change it on the Users page"
else
	say "existing installation updated; your users are unchanged"
fi
echo
say "the first replicas take a moment; watch with: docker service ls"
