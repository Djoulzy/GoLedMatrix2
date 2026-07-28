#!/bin/sh

set -eu

usage() {
	echo "usage: $0 USER@HOST [CONFIG_FILE]" >&2
	echo "example: $0 pi@raspberrypi.local server.toml" >&2
}

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	usage
	exit 2
fi

target=$1
config_path=${2:-config.toml}
ssh_port=${RPI_SSH_PORT:-22}
health_url=${RPI_HEALTH_URL-http://127.0.0.1:8080/healthz}
remote_path=${RPI_REMOTE_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}

case "$target" in
	""|-*|*[!A-Za-z0-9_.@:-]*)
		echo "invalid SSH target: $target" >&2
		exit 2
		;;
esac
case "$ssh_port" in
	""|*[!0-9]*)
		echo "RPI_SSH_PORT must be numeric" >&2
		exit 2
		;;
esac
if [ "$ssh_port" -lt 1 ] || [ "$ssh_port" -gt 65535 ]; then
	echo "RPI_SSH_PORT must be between 1 and 65535" >&2
	exit 2
fi
case "$health_url" in
	*[!A-Za-z0-9:/._?=%-]*)
		echo "RPI_HEALTH_URL contains unsupported characters" >&2
		exit 2
		;;
esac
case "$remote_path" in
	""|*[!A-Za-z0-9_./:-]*)
		echo "RPI_REMOTE_PATH contains unsupported characters" >&2
		exit 2
		;;
esac
if [ ! -f "$config_path" ]; then
	echo "configuration file not found: $config_path" >&2
	exit 2
fi
for command in ssh rsync; do
	if ! command -v "$command" >/dev/null 2>&1; then
		echo "required local command not found: $command" >&2
		exit 1
	fi
done

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
config_path=$(CDPATH= cd -- "$(dirname -- "$config_path")" && pwd)/$(basename -- "$config_path")

echo "Checking Raspberry Pi build prerequisites..."
missing_commands=$(ssh -p "$ssh_port" "$target" \
	"PATH='$remote_path'; export PATH; for tool in go make g++ systemctl rsync curl sudo; do command -v \"\$tool\" >/dev/null 2>&1 || printf '%s\n' \"\$tool\"; done")
if [ -n "$missing_commands" ]; then
	echo "missing commands on $target:" >&2
	printf '%s\n' "$missing_commands" | while IFS= read -r command; do
		echo "  - $command" >&2
	done
	echo "install the missing packages or set RPI_REMOTE_PATH; see README.md, section Deployment" >&2
	exit 1
fi
remote_go_version=$(ssh -p "$ssh_port" "$target" \
	"PATH='$remote_path'; export PATH; go env GOVERSION")
go_version=${remote_go_version#go}
go_major=${go_version%%.*}
go_remainder=${go_version#*.}
go_minor=${go_remainder%%.*}
case "$go_major.$go_minor" in
	*[!0-9.]*|.*|*.)
		echo "unable to determine the Raspberry Pi Go version: $remote_go_version" >&2
		exit 1
		;;
esac
if [ "$go_major" -lt 1 ] || { [ "$go_major" -eq 1 ] && [ "$go_minor" -lt 22 ]; }; then
	echo "Go 1.22 or newer is required on the Raspberry Pi; found $remote_go_version" >&2
	exit 1
fi

remote_stage=$(ssh -p "$ssh_port" "$target" "mktemp -d /tmp/goledmatrix2-deploy.XXXXXX")
case "$remote_stage" in
	/tmp/goledmatrix2-deploy.*) ;;
	*)
		echo "unexpected remote staging path: $remote_stage" >&2
		exit 1
		;;
esac

cleanup() {
	ssh -p "$ssh_port" "$target" "rm -rf -- '$remote_stage'" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM

echo "Copying sources to $target..."
rsync -az \
	-e "ssh -p $ssh_port" \
	--exclude '/.git/' \
	--exclude '/bin/' \
	--exclude '/config.toml' \
	--exclude '/server.toml' \
	--exclude '/.env' \
	"$project_root/" "$target:$remote_stage/"
rsync -az -e "ssh -p $ssh_port" \
	"$config_path" "$target:$remote_stage/deploy/server.toml"

echo "Building the native library and server on the Raspberry Pi..."
ssh -p "$ssh_port" "$target" \
	"PATH='$remote_path'; export PATH; cd '$remote_stage' && make native build-rpi && ./bin/ledmatrix-server -config deploy/server.toml -check-config"

echo "Installing and restarting the systemd service..."
ssh -t -p "$ssh_port" "$target" \
	"cd '$remote_stage' && sudo ./deploy/install-rpi.sh deploy/server.toml"

if [ -n "$health_url" ]; then
	echo "Checking $health_url on the Raspberry Pi..."
	ssh -p "$ssh_port" "$target" \
		"PATH='$remote_path'; export PATH; attempt=0; until curl --fail --silent --show-error '$health_url' >/dev/null; do attempt=\$((attempt + 1)); [ \"\$attempt\" -ge 10 ] && exit 1; sleep 1; done"
fi

trap - EXIT HUP INT TERM
cleanup

echo "Deployment completed successfully on $target."
