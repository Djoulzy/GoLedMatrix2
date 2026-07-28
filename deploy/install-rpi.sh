#!/bin/sh

set -eu

config_path=${1:-}

if [ "$(id -u)" -ne 0 ]; then
	echo "install-rpi.sh must be run as root" >&2
	exit 1
fi
if [ ! -x bin/ledmatrix-server ]; then
	echo "bin/ledmatrix-server is missing; run make native build-rpi first" >&2
	exit 1
fi
if [ -z "$config_path" ] || [ ! -f "$config_path" ]; then
	echo "usage: sudo ./deploy/install-rpi.sh PATH_TO_SERVER_CONFIG" >&2
	exit 1
fi

install -d -m 0755 /opt/goledmatrix2
install -d -m 0755 /etc/goledmatrix2
install -m 0755 bin/ledmatrix-server /opt/goledmatrix2/ledmatrix-server
install -m 0644 "$config_path" /etc/goledmatrix2/server.toml
install -m 0644 deploy/goledmatrix.service /etc/systemd/system/goledmatrix.service

systemctl daemon-reload
systemctl enable goledmatrix.service
if ! systemctl restart goledmatrix.service; then
	journalctl -u goledmatrix.service -n 40 --no-pager >&2
	exit 1
fi

if ! systemctl is-active --quiet goledmatrix.service; then
	journalctl -u goledmatrix.service -n 40 --no-pager >&2
	exit 1
fi

echo "GoLedMatrix2 is installed and running."
