#!/bin/sh
# Single-node k0s control plane in Docker for exploring Systemdoc's Kubernetes
# support. No root is needed: k0s runs in a privileged container, its API is
# published on localhost only, and `down` removes everything it created.
#
#   playground/k0s.sh up        start k0s, install kubectl if missing, deploy samples
#   playground/k0s.sh status    node, pods and where the kubeconfig lives
#   playground/k0s.sh down      remove the container, its volume and the kubeconfig
set -eu

NAME=${K0S_NAME:-systemdoc-k0s}
# The latest tag on Docker Hub is stale; pin a current release and override with K0S_IMAGE.
IMAGE=${K0S_IMAGE:-docker.io/k0sproject/k0s:v1.36.4-k0s.0}
PORT=${K0S_PORT:-6443}
KUBECONFIG_PATH=${K0S_KUBECONFIG:-$HOME/.kube/systemdoc-k0s.yaml}
DEFAULT_KUBECONFIG=$HOME/.kube/config
DIR=$(cd "$(dirname "$0")" && pwd)
export KUBECONFIG="$KUBECONFIG_PATH"

log() { printf '%s\n' "$*"; }

ensure_kubectl() {
	if command -v kubectl >/dev/null 2>&1; then
		return
	fi
	version=$(docker exec "$NAME" k0s version | sed 's/+k0s.*//')
	arch=$(uname -m)
	case "$arch" in
	x86_64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	esac
	target="$HOME/.local/bin/kubectl"
	log "Installing kubectl $version to $target (matching the k0s release)"
	mkdir -p "$HOME/.local/bin"
	curl -fsSL -o "$target.tmp" "https://dl.k8s.io/release/$version/bin/linux/$arch/kubectl"
	chmod 755 "$target.tmp"
	mv "$target.tmp" "$target"
	command -v kubectl >/dev/null 2>&1 || log "Add $HOME/.local/bin to PATH so Systemdoc can find kubectl."
}

write_kubeconfig() {
	mkdir -p "$HOME/.kube"
	umask 077
	docker exec "$NAME" k0s kubeconfig admin |
		sed "s#server: https://.*:6443#server: https://127.0.0.1:$PORT#" >"$KUBECONFIG_PATH"
	# Systemdoc's kubectl probe follows the default kubeconfig; link it only
	# when nothing else lives there, so an existing configuration is never touched.
	if [ ! -e "$DEFAULT_KUBECONFIG" ]; then
		ln -s "$KUBECONFIG_PATH" "$DEFAULT_KUBECONFIG"
	fi
}

up() {
	if docker inspect "$NAME" >/dev/null 2>&1; then
		docker start "$NAME" >/dev/null
	else
		log "Starting k0s ($IMAGE) as $NAME with the API on 127.0.0.1:$PORT"
		docker run -d --name "$NAME" --hostname "$NAME" --privileged --cgroupns=host \
			-v "$NAME-data:/var/lib/k0s" -p "127.0.0.1:$PORT:6443" \
			"$IMAGE" k0s controller --single >/dev/null
	fi
	log "Waiting for the k0s API server…"
	attempts=0
	until docker exec "$NAME" k0s kubeconfig admin >/dev/null 2>&1; do
		attempts=$((attempts + 1))
		if [ "$attempts" -gt 60 ]; then
			log "k0s did not produce an admin kubeconfig; see: docker logs $NAME"
			exit 1
		fi
		sleep 2
	done
	write_kubeconfig
	ensure_kubectl
	attempts=0
	until kubectl get nodes 2>/dev/null | grep -q ' Ready'; do
		attempts=$((attempts + 1))
		if [ "$attempts" -gt 90 ]; then
			log "The k0s node did not become Ready; see: kubectl get nodes; docker logs $NAME"
			exit 1
		fi
		sleep 2
	done
	kubectl apply -f "$DIR/k8s-workloads.yaml"
	log
	log "k0s is ready. kubeconfig: $KUBECONFIG_PATH"
	if [ -L "$DEFAULT_KUBECONFIG" ] && [ "$(readlink "$DEFAULT_KUBECONFIG")" = "$KUBECONFIG_PATH" ]; then
		log "Linked as $DEFAULT_KUBECONFIG, so kubectl and Systemdoc find it without KUBECONFIG."
	else
		log "Run: export KUBECONFIG=$KUBECONFIG_PATH"
	fi
	log "Then: ./bin/systemdoc --docker   (pods appear beside the $NAME container; metrics arrive after about a minute)"
}

status() {
	docker ps --filter "name=^$NAME\$" --format 'container {{.Names}} · {{.Status}}' || true
	log "kubeconfig: $KUBECONFIG_PATH"
	kubectl get nodes -o wide 2>/dev/null || log "API not reachable"
	kubectl get pods --all-namespaces 2>/dev/null || true
}

down() {
	docker rm -f "$NAME" >/dev/null 2>&1 && log "Removed container $NAME" || true
	docker volume rm "$NAME-data" >/dev/null 2>&1 && log "Removed volume $NAME-data" || true
	if [ -L "$DEFAULT_KUBECONFIG" ] && [ "$(readlink "$DEFAULT_KUBECONFIG")" = "$KUBECONFIG_PATH" ]; then
		rm -f "$DEFAULT_KUBECONFIG"
		log "Removed link $DEFAULT_KUBECONFIG"
	fi
	rm -f "$KUBECONFIG_PATH" && log "Removed $KUBECONFIG_PATH"
	log "kubectl in $HOME/.local/bin is left in place; delete it yourself if it was installed by this script."
}

case "${1:-}" in
up) up ;;
status) status ;;
down) down ;;
*)
	log "usage: $0 up|status|down"
	exit 2
	;;
esac
