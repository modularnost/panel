#!/bin/sh
# Remove modularnost from a server.
#
#   sh uninstall.sh              remove the panel stack, keep the data
#   sh uninstall.sh --purge      also delete the database and the certificates
#
# The swarm is never touched: other stacks may be running on it. If you want the
# node out of its swarm, the last line of this script tells you the command.
set -eu

STACK=${PANEL_STACK:-panel}
NET=${PANEL_NETWORK:-modularnost}
CONFIG=${PANEL_CONFIG:-/etc/modularnost}
PURGE=no

for arg in "$@"; do
	case "$arg" in
	--purge) PURGE=yes ;;
	*) echo "unknown option: $arg" >&2; exit 1 ;;
	esac
done

die() { echo "error: $*" >&2; exit 1; }
say() { echo "==> $*"; }

[ "$(id -u)" = 0 ] || die "run as root"

# The firewall's rules are in the kernel and outlive its service: without this
# the host would stay closed to everything but SSH, 80, 443 and the nodes. The
# image to run it from is the one the service used.
FW_IMAGE=$(docker service inspect "${STACK}_firewall" --format '{{.Spec.TaskTemplate.ContainerSpec.Image}}' 2>/dev/null | sed 's/@sha256:.*//' || true)

if docker stack ls --format '{{.Name}}' | grep -qx "$STACK"; then
	say "removing the $STACK stack"
	docker stack rm "$STACK" >/dev/null
	# Swarm removes services asynchronously; the volumes stay attached until it
	# is done, so wait rather than fail on the next step.
	printf '==> waiting for its services to stop '
	while docker service ls --filter "label=com.docker.stack.namespace=$STACK" -q | grep -q .; do
		printf .
		sleep 2
	done
	echo " done"
else
	say "no $STACK stack here"
fi

if [ -n "$FW_IMAGE" ]; then
	say "removing the firewall chain from this host"
	docker run --rm --network host --cap-add NET_ADMIN -e PANEL_MODE=firewall-clear "$FW_IMAGE" >/dev/null 2>&1 ||
		echo "could not remove it; on this host: iptables -D INPUT -j MODULARNOST; iptables -F MODULARNOST; iptables -X MODULARNOST (and ip6tables)" >&2
	say "other nodes of the cluster keep their chain until the same is done there: docker run --rm --network host --cap-add NET_ADMIN -e PANEL_MODE=firewall-clear $FW_IMAGE"
fi

# A volume stays busy until the stopped task containers are reaped, which
# happens a moment after the service is gone. Retry rather than report a
# deletion that did not happen.
remove_volume() {
	[ -n "$(docker volume ls -q --filter "name=^$1\$")" ] || return 0
	i=0
	while [ "$i" -lt 15 ]; do
		docker volume rm "$1" >/dev/null 2>&1 && return 0
		i=$((i + 1))
		sleep 2
	done
	echo "could not remove volume $1 — something still uses it" >&2
	return 1
}

if [ "$PURGE" = yes ]; then
	say "deleting data: users, webhooks, deploy history, compose files, certificates"
	failed=0
	remove_volume "${STACK}_panel-data" || failed=1
	remove_volume "${STACK}_acme" || failed=1
	rm -rf "$CONFIG"
	docker network rm "$NET" >/dev/null 2>&1 || true
	[ "$failed" = 0 ] || die "some data was left behind, see above"
else
	say "kept: volumes ${STACK}_panel-data and ${STACK}_acme, and $CONFIG"
	say "re-running install.sh restores the panel with its users intact"
	say "to delete them: sh uninstall.sh --purge"
fi

echo
say "the swarm was left alone. To take this node out of it:"
say "  docker swarm leave --force"
