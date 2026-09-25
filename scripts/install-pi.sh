#!/bin/sh
# Install Keystone on a Raspberry Pi and start it on boot.
# Usage:
#   sudo sh install-pi.sh ./keystone-linux-arm64
# Join first if this Pi does not have a profile yet:
#   sudo KEYSTONE_HOME=/var/lib/keystone ./keystone-linux-arm64 join \
#     --name pi --coordinator https://LAPTOP:7707 --token TOKEN --pin PIN --share /home/pi
set -eu

BIN="${1:-}"
if [ -z "$BIN" ] || [ ! -f "$BIN" ]; then
  echo "usage: sudo sh install-pi.sh ./keystone-linux-arm64" >&2
  exit 1
fi

install -d /var/lib/keystone /usr/local/bin
install -m 0755 "$BIN" /usr/local/bin/keystone

cat > /etc/systemd/system/keystone.service <<'EOF'
[Unit]
Description=Keystone mesh
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=KEYSTONE_HOME=/var/lib/keystone
ExecStart=/usr/local/bin/keystone up --no-browser
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now keystone.service
systemctl --no-pager --full status keystone.service || true
