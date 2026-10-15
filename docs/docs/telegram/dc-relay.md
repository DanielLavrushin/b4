---
sidebar_position: 6
title: DC Relay
description: Sending the MTProto proxy's direct connections to Telegram through a VPS with socat.
---

A DC Relay sends the MTProto proxy's direct connections to Telegram through a VPS. It is for b4 inside a censored network where Telegram's addresses and the WebSocket routes are both blocked. The VPS runs only `socat`, a TCP forwarder: no keys and no Telegram software.

## Requirements

- A VPS outside the censored network that b4 can reach.
- Five free ports on the VPS, one for each main data centre, open in its firewall.
- **Transport mode** set to **Direct TCP** or **Auto (WebSocket → TCP)** on the **Telegram upstream (shared)** card. **WebSocket only** makes no direct connections, so the relay has no effect there.

The relay serves the [MTProto proxy](./mtproto-proxy.md) only. [Telegram over WebSocket](./websocket-bridge.md) does not use it.

## Setup

1. Install `socat` on the VPS:

   ```bash
   apt install -y socat
   ```

2. In b4, open **Settings > Telegram** and enter **DC Relay** on the **Telegram upstream (shared)** card: the VPS address with a base port, such as `my-vps.com:7007`.
3. Click the **?** button beside the field. The **DC Relay socat setup** dialog shows a ready `socat` command for each data centre.
4. Click **Copy all** and run the commands on the VPS.
5. Open the ports that the dialog lists on its last line in the VPS firewall.
6. To keep the commands running after a reboot, start them from `/etc/rc.local` or a systemd unit.
7. Click **Save Changes** in b4.

![The DC Relay socat setup dialog](/img/telegram/20260826233006.png)

Each command forwards one VPS port, the base port plus the data centre's number minus one, to that data centre's port 443. Media data centre 203 shares the port of data centre 2 and needs no command of its own. With **Auto (WebSocket → TCP)**, b4 tries the relay first and the WebSocket routes after it. A WebSocket connection that b4 keeps open in advance is still used before the relay.

## IPv4 or IPv6

The dialog's **Address family** switch chooses whether the commands connect to Telegram over IPv4 or IPv6. IPv6 is for a VPS that reaches Telegram over IPv6 but not over IPv4. The switch changes only the Telegram side of each command:

```bash
# IPv4
socat TCP-LISTEN:7007,fork,reuseaddr TCP:149.154.175.50:443 &

# IPv6
socat TCP-LISTEN:7007,fork,reuseaddr TCP6:[2001:b28:f23d:f001::a]:443 &
```

The listening side follows the **DC Relay** address. An IPv6 address there is written in brackets, `[2001:db8::1]:7007`, and the commands then listen over IPv6 as well.

Media data centre 203 has no IPv6 address and goes through the command of data centre 2. When media stops loading after a switch to IPv6, that one command goes back to IPv4.

## Checking the result

**Test connection** on the **Telegram upstream (shared)** card tries data centre 2 over each configured route, the relay included. **Test direct TCP** tries it without the relay, which separates a relay problem from a Telegram one.

## When it does not work

When connections through the relay fail, `socat` is not running on the VPS, a port is closed in its firewall, or the relay address is wrong. The log line and the checks are under [Dialling fails](./troubleshooting.md#dialling-fails).
