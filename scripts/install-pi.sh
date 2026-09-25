#!/bin/sh
# Install Keystone on a Raspberry Pi, start it on boot, and add a desktop icon.
# Usage, from the folder that contains the program (and, if you have it, keystone.png):
#   sudo sh install-pi.sh ./keystone-linux-arm64
#
# Join first when this Pi has no profile yet:
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

cat > /usr/local/bin/keystone-launch <<'EOF'
#!/bin/sh
WEB="http://127.0.0.1:8731/"
if ! curl -fsS -o /dev/null --max-time 2 "$WEB" 2>/dev/null; then
  if command -v systemctl >/dev/null 2>&1; then
    systemctl start keystone >/dev/null 2>&1 || true
    i=0
    while [ "$i" -lt 20 ]; do
      curl -fsS -o /dev/null --max-time 1 "$WEB" 2>/dev/null && break
      i=$((i + 1))
      sleep 0.25
    done
  fi
fi
if command -v xdg-open >/dev/null 2>&1; then
  xdg-open "$WEB"
elif command -v gio >/dev/null 2>&1; then
  gio open "$WEB"
else
  echo "Open $WEB in a browser on this Pi." >&2
fi
EOF
chmod 0755 /usr/local/bin/keystone-launch

ICON_SRC=""
for candidate in \
  "$(dirname "$BIN")/keystone.png" \
  "$(dirname "$0")/keystone.png" \
  "$(dirname "$0")/../assets/keystone.png"
do
  if [ -f "$candidate" ]; then
    ICON_SRC="$candidate"
    break
  fi
done
if [ -z "$ICON_SRC" ] && command -v curl >/dev/null 2>&1; then
  tmp="$(mktemp)"
  if curl -fsSL -o "$tmp" "https://github.com/danieldonelon/keystone/releases/download/v0.1.1/keystone.png"; then
    ICON_SRC="$tmp"
  else
    rm -f "$tmp"
    ICON_SRC=""
  fi
fi
if [ -n "$ICON_SRC" ]; then
  install -d /usr/share/icons/hicolor/256x256/apps
  install -m 0644 "$ICON_SRC" /usr/share/icons/hicolor/256x256/apps/keystone.png
  command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -f /usr/share/icons/hicolor >/dev/null 2>&1 || true
fi

install -d /usr/share/applications
cat > /usr/share/applications/keystone.desktop <<'EOF'
[Desktop Entry]
Version=1.0
Type=Application
Name=Keystone
GenericName=Private mesh
Comment=Open the Keystone window
Exec=/usr/local/bin/keystone-launch
Icon=keystone
Terminal=false
Categories=Network;
StartupNotify=true
EOF

install_desktop() {
  home="$1"
  desk="$home/Desktop"
  [ -d "$desk" ] || return 0
  cp /usr/share/applications/keystone.desktop "$desk/Keystone.desktop"
  chmod 0755 "$desk/Keystone.desktop"
  owner="$(stat -c %U "$home" 2>/dev/null || true)"
  if [ -n "$owner" ] && [ "$owner" != "root" ]; then
    chown "$owner:$owner" "$desk/Keystone.desktop" || true
    if command -v sudo >/dev/null 2>&1 && command -v gio >/dev/null 2>&1; then
      sudo -u "$owner" gio set "$desk/Keystone.desktop" metadata::trusted true >/dev/null 2>&1 || true
    fi
  fi
}

if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
  sudo_home="$(getent passwd "$SUDO_USER" | cut -d: -f6)"
  [ -n "$sudo_home" ] && install_desktop "$sudo_home"
fi
for home in /home/*; do
  [ -d "$home" ] && install_desktop "$home"
done

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
echo "Desktop icon installed. Open Keystone from the menu or the desktop."
