# Keystone

Keystone is a private network for computers you own. It connects a Windows laptop and Linux machines such as a Raspberry Pi, and it keeps working while ExpressVPN is connected.

It is a separate program from Tailscale. It does not join a Tailscale network and it does not use Tailscale’s servers. The tunnel is WireGuard, run inside Keystone, and it only carries traffic between your Keystone machines.

## What you can do

- See your machines and whether they are reachable
- Browse, upload, and download files
- View and control a screen
  - On Windows, Keystone shares the desktop itself
  - On a Raspberry Pi, Keystone connects to `wayvnc` or `x11vnc` when one of those is available

## Why ExpressVPN can stay on

ExpressVPN installs broad routes (`0.0.0.0/1` and `128.0.0.0/1`) so normal apps leave through the VPN. Keystone does not change those routes and does not send the rest of your traffic around the VPN.

Keystone’s own UDP socket is pinned to the physical adapter (Wi-Fi or Ethernet). On Linux it is marked and routed with a policy rule to that same adapter. Mesh packets therefore use your real network path, while browsers and everything else continue to use ExpressVPN.

Check it with:

```
keystone doctor
```

You should see two different public addresses while ExpressVPN is connected: one for ordinary UDP, and one for the socket Keystone pins. If they are the same, add Keystone to ExpressVPN’s split tunneling exclusions. That is the fallback when a VPN driver redirects by program rather than by route.

The mesh addresses are `10.91.0.0/16`. That range stays out of the way of the addresses ExpressVPN uses inside its own tunnel.

## First computer

On the Windows laptop:

```
keystone init --name laptop
keystone up
```

Or double-click the Keystone icon on the desktop. The icon starts Keystone when it is stopped, and opens the window when it is already running. Put the icon back with:

```
powershell -ExecutionPolicy Bypass -File scripts\install-windows-shortcut.ps1
```

A browser opens at `http://127.0.0.1:8731`. The page shows the `keystone join` command for your other computers. The shared folder defaults to your home directory. Change it in the Files view. Light mode and Dark mode are in the header.

Leave this computer running. It is the coordinator the others register with.

The full guide is [docs/Keystone-User-Manual.pdf](docs/Keystone-User-Manual.pdf).

## Repository

```
cmd/keystone/     the keystone command
internal/         mesh, files, screen viewer, and the local window
assets/           desktop icon, black with a MojoSoMint mint outline
deploy/           systemd unit and the Pi desktop entry
docs/             Keystone-User-Manual.pdf
scripts/          Windows shortcut, Pi installer, icon and manual builders
```

Built programs stay out of git. Release [v0.1.1](https://github.com/danieldonelon/keystone/releases/tag/v0.1.1) carries the programs, the desktop icon, and the user manual.

## Raspberry Pi

64-bit Raspberry Pi OS (Pi 3, 4, 5, and Zero 2 W):

```sh
curl -fL -o keystone-linux-arm64 \
  https://github.com/danieldonelon/keystone/releases/download/v0.1.1/keystone-linux-arm64
curl -fL -o keystone.png \
  https://github.com/danieldonelon/keystone/releases/download/v0.1.1/keystone.png
curl -fL -o install-pi.sh \
  https://raw.githubusercontent.com/danieldonelon/keystone/v0.1.1/scripts/install-pi.sh
chmod +x keystone-linux-arm64
sudo KEYSTONE_HOME=/var/lib/keystone ./keystone-linux-arm64 join --name pi \
  --coordinator https://192.168.0.130:7707 --token TOKEN --pin PIN --share /home/pi
sudo sh install-pi.sh ./keystone-linux-arm64
```

The installer also puts a Keystone icon on the Pi desktop and in the application menu. Use `keystone-linux-armv7` instead on a 32-bit Pi. The coordinator address, token, and pin come from `keystone init` on the laptop, and they are shown again in the Keystone window.

To build on the Pi from source instead:

```sh
git clone https://github.com/danieldonelon/keystone.git
cd keystone
sudo apt install golang-go
go build -o keystone ./cmd/keystone
```

For the screen, on the Pi desktop:

```
sudo apt install wayvnc
```

Keystone will use a VNC server that is already listening on `127.0.0.1:5900`, and it will try to start `wayvnc` or `x11vnc` if it can see a desktop session.

## Away from home

Same-LAN use works while ExpressVPN is on, because your LAN route is more specific than the VPN routes.

To reach a Pi from somewhere else, forward these ports on the coordinator’s router to that computer:

- TCP `7707` so machines can register
- UDP `7708` for the relay
- UDP `51830` for the direct WireGuard path

If a direct path does not come up, Keystone falls back to relaying through the coordinator. The coordinator itself has to be reachable.

## Ports

| Port | Use |
| --- | --- |
| 8731 | Keystone window on this computer only |
| 7707 | Coordinator (TLS, certificate pin) |
| 7708 | Discovery and relay |
| 51830 | WireGuard |
| 7760 | File transfer, inside the mesh only |
| 5900 | Screen, inside the mesh only |

Windows will ask the first time whether Keystone may listen. Allow it on private networks.

## Security

Joining requires the token and the certificate pin from `keystone init`. File transfer and screen sharing are served only on the mesh, and the window is bound to `127.0.0.1`. The profile, including the private key, is stored in `%APPDATA%\Keystone` on Windows and `~/.config/keystone` on Linux (`/var/lib/keystone` for the Pi service).

WireGuard’s userspace library is included under its MIT license. Keystone’s own code is MIT; see `LICENSE`.
