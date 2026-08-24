#!/bin/sh
# A throwaway multi-node Swarm for testing: three Docker-in-Docker daemons on
# one machine, joined into a real cluster with a real overlay network.
#
#   scripts/dev-cluster.sh up      build images, start the cluster, deploy the panel
#   scripts/dev-cluster.sh down    remove everything
#   scripts/dev-cluster.sh <node> <docker args...>   run docker on one node
#
# The panel lands on http://localhost:8090 (login admin / dev-password).
# Requires a working Docker with privileged containers.
set -eu

NODES="mgr w1 w2"
NET=swarmlab
DIND=docker:28-dind
PORT=8090
SECRET=dev-shared-secret
PASSWORD=dev-password
IMAGES="modularnost:latest metrics-module:latest"
# Set PANEL_IMAGE to test a published image instead: nothing is built, and the
# nodes pull it themselves, which is what a real cluster does.
PANEL_IMAGE=${PANEL_IMAGE:-}

n() { echo "swarm-$1"; }
on() { node=$1; shift; docker exec "$(n "$node")" docker "$@"; }

up() {
	if [ -z "$PANEL_IMAGE" ]; then
		echo "==> building images"
		docker build -q --build-arg VERSION=dev-cluster -t modularnost:latest . >/dev/null
		docker build -q -t metrics-module:latest examples/metrics-module >/dev/null
	else
		echo "==> using $PANEL_IMAGE, nodes will pull it themselves"
	fi

	docker network create "$NET" >/dev/null 2>&1 || true

	for node in $NODES; do
		echo "==> starting $(n "$node")"
		# The manager publishes the panel port: Swarm's routing mesh takes it
		# from there to whichever node actually runs the panel.
		ports=""
		[ "$node" = mgr ] && ports="-p $PORT:8080"
		# shellcheck disable=SC2086
		docker run -d --privileged --name "$(n "$node")" --hostname "$(n "$node")" \
			--network "$NET" $ports \
			-e DOCKER_TLS_CERTDIR= \
			"$DIND" --host=tcp://0.0.0.0:2375 --tls=false >/dev/null
	done

	for node in $NODES; do
		printf '==> waiting for %s ' "$(n "$node")"
		until docker exec "$(n "$node")" docker info >/dev/null 2>&1; do printf .; sleep 1; done
		echo " ok"
	done

	echo "==> forming the swarm"
	# Swarm wants an IP here, not a name.
	mgr_ip=$(docker inspect -f "{{(index .NetworkSettings.Networks \"$NET\").IPAddress}}" "$(n mgr)")
	on mgr swarm init --advertise-addr "$mgr_ip" >/dev/null
	token=$(on mgr swarm join-token -q worker | tr -d '\r')
	for node in w1 w2; do
		on "$node" swarm join --token "$token" "$mgr_ip:2377" >/dev/null
	done

	if [ -z "$PANEL_IMAGE" ]; then
		echo "==> loading images into every node (no registry here)"
		for img in $IMAGES; do
			for node in $NODES; do
				docker save "$img" | docker exec -i "$(n "$node")" docker load >/dev/null
			done
		done
	fi

	echo "==> deploying the panel"
	docker cp stack.yml "$(n mgr)":/stack.yml
	docker exec -e PANEL_ADMIN_PASSWORD="$PASSWORD" -e PANEL_MODULE_SECRET="$SECRET" \
		-e PANEL_IMAGE="${PANEL_IMAGE:-modularnost:latest}" \
		"$(n mgr)" docker stack deploy -c /stack.yml panel

	echo
	on mgr node ls
	echo
	echo "panel: http://localhost:$PORT  (admin / $PASSWORD)"
	echo "run docker on a node: scripts/dev-cluster.sh mgr service ls"
}

down() {
	for node in $NODES; do docker rm -f "$(n "$node")" >/dev/null 2>&1 || true; done
	docker network rm "$NET" >/dev/null 2>&1 || true
	echo "cluster removed"
}

case "${1:-}" in
up) up ;;
down) down ;;
mgr | w1 | w2) node=$1; shift; on "$node" "$@" ;;
*) echo "usage: $0 up|down|<mgr|w1|w2> <docker args...>" >&2; exit 1 ;;
esac
