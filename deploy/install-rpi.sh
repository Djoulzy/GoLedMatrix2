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
if [ ! -x /usr/bin/systemctl ] || [ ! -x /usr/bin/sudo ] || ! command -v visudo >/dev/null 2>&1; then
	echo "systemctl, sudo and visudo are required for safe remote power-off" >&2
	exit 1
fi
# Validate before installing any privileged rule. The driver keeps its normal
# privilege drop; only the fixed power-off command gets NOPASSWD permission.
visudo -cf deploy/goledmatrix2-poweroff.sudoers

install -d -m 0755 /opt/goledmatrix2
install -d -m 0755 /etc/goledmatrix2
install -m 0755 bin/ledmatrix-server /opt/goledmatrix2/ledmatrix-server
install -m 0644 "$config_path" /etc/goledmatrix2/server.toml
install -m 0644 deploy/goledmatrix.service /etc/systemd/system/goledmatrix.service
install -d -o root -g root -m 0755 /etc/sudoers.d
install -o root -g root -m 0440 deploy/goledmatrix2-poweroff.sudoers /etc/sudoers.d/goledmatrix2-poweroff

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
