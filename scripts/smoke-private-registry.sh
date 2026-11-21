#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

registry=swarm-registry
host=$registry:5000
image=$host/smoke:current
service=registry-smoke_app
url=http://127.0.0.1:8090
cluster() { sh scripts/dev-cluster.sh "$@"; }
api() { curl --fail-with-body -sS --max-time 30 -u admin:dev-password -H 'Content-Type: application/json' "$@"; }

docker info >/dev/null
for name in swarm-mgr swarm-w1 swarm-w2 "$registry"; do
	if docker container inspect "$name" >/dev/null 2>&1; then
		echo "Stop the existing lab before running the smoke test: $name exists" >&2
		exit 1
	fi
done
if docker network inspect swarmlab >/dev/null 2>&1; then
	echo "Stop the existing lab before running the smoke test: swarmlab exists" >&2
	exit 1
fi

tmp=$(mktemp -d)
cleanup() {
	status=$?
	trap - EXIT
	if [ "$status" -ne 0 ]; then
		cluster mgr node ls >&2 || true
		cluster mgr service ps --no-trunc "$service" >&2 || true
		cluster mgr service ps --no-trunc panel_panel >&2 || true
		cluster mgr service logs --tail 50 panel_panel >&2 || true
		docker logs --tail 30 "$registry" >&2 || true
	fi
	docker rm -f -v "$registry" >/dev/null 2>&1 || true
	cluster down
	rm -f "$tmp/cert.pem" "$tmp/key.pem" "$tmp/htpasswd" "$tmp/pull.log"
	rmdir "$tmp"
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cluster up
echo "==> starting the authenticated TLS registry"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=$registry" \
	-addext "subjectAltName=DNS:$registry" -keyout "$tmp/key.pem" -out "$tmp/cert.pem" 2>/dev/null
docker run --rm httpd:2.4-alpine htpasswd -Bbn smoke dev-registry-token > "$tmp/htpasswd"
docker run -d --name "$registry" --network swarmlab --mount "type=bind,source=$tmp,target=/auth,readonly" \
	-e REGISTRY_HTTP_TLS_CERTIFICATE=/auth/cert.pem -e REGISTRY_HTTP_TLS_KEY=/auth/key.pem \
	-e REGISTRY_AUTH=htpasswd -e REGISTRY_AUTH_HTPASSWD_REALM=smoke -e REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd \
	registry:2 >/dev/null
for node in mgr w1; do
	dir="/etc/docker/certs.d/$host"
	docker exec "swarm-$node" mkdir -p "$dir"
	docker cp "$tmp/cert.pem" "swarm-$node:$dir/ca.crt"
done

wait_for() {
	label=$1; shift
	attempts=0
	until "$@"; do
		attempts=$((attempts + 1))
		[ "$attempts" -lt 60 ] || { echo "Timed out waiting for $label" >&2; return 1; }
		sleep 2
	done
}

registry_ready() {
	printf '%s\n' dev-registry-token | docker exec -i swarm-mgr docker login -u smoke --password-stdin "$host" >/dev/null 2>&1
}
publish() {
	version=$1
	docker exec -i swarm-mgr docker build -t "$image" - >&2 <<EOF
FROM busybox:1.37
RUN echo $version > /smoke-version
CMD ["sleep", "3600"]
EOF
	registry_ready || return 1
	cluster mgr image push "$image" >&2
	cluster mgr logout "$host" >&2
	cluster mgr image inspect --format '{{index .RepoDigests 0}}' "$image"
}
running() {
	expected=$1
	digest=$2
	container=$(cluster w1 ps -q --filter "label=com.docker.swarm.service.name=$service") || return 1
	[ -n "$container" ] || return 1
	[ "$(cluster w1 exec "$container" cat /smoke-version 2>/dev/null)" = "$expected" ] || return 1
	id=$(cluster w1 inspect --format '{{.Image}}' "$container") || return 1
	cluster w1 image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$id" | grep -Fxq "$digest"
}
registry_configured() {
	api "$url/api/v1/registries" 2>/dev/null | jq -e '.configured' >/dev/null
}

wait_for "registry login" registry_ready
first=$(publish first)
if cluster w1 image pull "$image" > "$tmp/pull.log" 2>&1 || ! grep -Eiq 'unauthorized|authentication required|denied' "$tmp/pull.log"; then
	echo "The clean worker did not get an authentication error" >&2
	cat "$tmp/pull.log" >&2
	rm -f "$tmp/pull.log"
	exit 1
fi
rm -f "$tmp/pull.log"

echo "==> enabling saved credentials in the test panel"
openssl rand -hex 32 | docker exec -i swarm-mgr sh -c 'umask 077; cat > /registry-key'
cluster mgr service update -d --mount-add type=bind,source=/registry-key,target=/registry-key,readonly \
	--env-add PANEL_REGISTRY_KEY_FILE=/registry-key panel_panel >/dev/null
wait_for "panel registry settings" registry_configured
api -X PUT "$url/api/v1/registries/$host" -d '{"username":"smoke","token":"dev-registry-token"}'
compose=$(cat <<EOF
version: "3.8"
services:
  app:
    image: $image
    deploy:
      placement:
        constraints: [node.hostname == swarm-w1]
EOF
)
jq -n --arg compose "$compose" --arg host "$host" '{compose:$compose,registry_host:$host}' |
	api -X PUT "$url/api/v1/stacks/registry-smoke" -d @-
wait_for "the private image on w1" running first "$first"

echo "==> refreshing the same tag through a webhook"
second=$(publish second)
[ "$first" != "$second" ] || { echo "Expected a new registry digest" >&2; exit 1; }
hook=$(api -X POST "$url/api/v1/webhooks" -d '{"action":"stack_redeploy","stack":"registry-smoke"}' | jq -er '.path')
curl --fail-with-body -sS --max-time 30 -X POST "$url$hook"
wait_for "the new private image on w1" running second "$second"
echo "Private registry deploy and webhook redeploy passed on w1."
