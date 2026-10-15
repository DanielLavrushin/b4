---
sidebar_position: 5
title: Securing the web interface
description: A login, HTTPS, and what the Expose to internet switches open and cannot open.
---

By default the web interface opens without a password for anyone who reaches its port. A login and HTTPS protect it, and the **Expose to internet** switches decide whether it, or a proxy of b4, can be reached from the internet.

## Requirements

- Access to the web interface.
- For HTTPS, a certificate and its private key as PEM files on the router.

## Setting a login {#authentication}

1. Open **Settings > System > Web Server**.
2. Under **Authentication**, fill in **Username** and **Password**.
3. Click **Save Changes**.

The web interface asks for the login from then on. The login applies only while both fields are filled. A session lasts until b4 restarts or until it goes unused for 24 hours. A new password does not end the sessions already open, a restart does. After 5 failed logins within 15 minutes, logins from that address are refused for 5 minutes. The fields are described under [Web Server](./system.md#web-server).

## Turning on HTTPS {#https}

Without HTTPS the login, the password and every session cross the network unencrypted.

1. Copy the certificate and the key to the router, for example into the directory of the configuration file.
2. Open **Settings > System > Web Server**.
3. Enter their paths in **TLS Certificate** and **TLS Key**.
4. Click **Save Changes**, then **Restart Service**.
5. Open the web interface at the same address with `https://`.

A self-signed certificate is enough on a local network:

```bash
openssl req -x509 -newkey rsa:2048 -keyout server.key -out server.crt -days 365 -nodes -subj "/CN=b4"
```

The key must be an unencrypted PEM file. A save is refused when a file is missing, the key has a passphrase or the certificate does not match the key. `openssl pkey -in key.pem -out key-plain.pem` removes the passphrase. b4 reads the files when it starts, so a renewed certificate takes effect after a restart.

## Access from the internet {#expose-to-internet}

Each listener in the table below has an **Expose to internet** switch next to its port, off by default. With it on, b4 adds a firewall rule on the host it runs on that accepts TCP connections to that port from any address, IPv4 and IPv6. b4 puts the rule back after the router reloads its firewall, and removes it when the switch is turned off or b4 stops. The switches apply on save.

| Listener | Switch | Opens the port only when |
| --- | --- | --- |
| Web interface | **Settings > System > Web Server** | A username and a password are set |
| MTProto proxy | **Settings > Telegram > MTProto Proxy** | The proxy is on |
| WEB proxy relay port | **Settings > Telegram > Telegram Desktop WEB proxy** | The MTProto proxy and its WEB proxy are on, and **Relay port** holds a port |
| SOCKS5 proxy | **Settings > Core > SOCKS5** | The proxy is on with a login or **Allowed Sources**, and the web interface, if it runs, has a login |

To open the web interface:

1. Set a login and turn on HTTPS, as above.
2. Turn on **Expose to internet** under **Settings > System > Web Server**.
3. Click **Save Changes**.

The web interface port also serves the API, the [MCP server](./mcp.md) and, without a relay port of its own, the WEB proxy relay, so the switch exposes them together. A changed port moves the rule at the next restart.

A SOCKS5 client can connect to any address, the router's own ports and the web interface included. That is why exposing the proxy needs a login on the web interface as well. SOCKS5 sends its own login unencrypted. The rule covers TCP only, so on a host that drops incoming connections, UDP through the proxy does not work from outside.

With a switch off, b4 adds no rule, and it does not block the port either. On a host that accepts incoming connections, such as a VPS without a firewall, every listener on `0.0.0.0` is reachable from the internet whatever the switch says. A listener stays private through its **Bind Address**, a LAN address or `127.0.0.1`, or through the host's own firewall.

:::warning Without a login
Anyone who reaches the web interface without a login gets full control of b4. b4 refuses to expose the web interface without a username and a password, but on a host without a firewall the default `0.0.0.0` is reachable from the internet anyway.
:::

## What the switch cannot open {#expose-limits}

The rule opens the port on the host b4 runs on and nowhere else.

| Situation | What it takes |
| --- | --- |
| Carrier-grade NAT: a WAN address in `100.64.0.0/10` | No connection from the internet reaches it over IPv4. A public IPv6 address on the WAN is not affected. |
| A router or modem in front: a private WAN address such as `192.168.x.x` | A port forward on that device as well, where the provider allows one. |
| A cloud firewall: AWS security groups, Azure network security groups, Google Cloud firewall rules, Oracle Cloud security lists | A rule in the provider's firewall as well. |
| b4 in a MikroTik container | A `dst-nat` rule on RouterOS, see [MikroTik](../install/mikrotik.md#access-from-the-wan). |
| firewalld that owns its nftables table, as on Fedora 42, RHEL 10 and Debian 13 | Opening the port in firewalld: `firewall-cmd --permanent --add-port=<port>/tcp`, then `firewall-cmd --reload`. The log names these commands. |
| **Skip IPTables/NFTables Setup** on | Nothing: b4 adds no rule, and the switches are greyed out. |

How the rules are placed is described under [Packet capture](../internals/capture.md#expose-to-internet-rules).

## Checking the result

- The web interface asks for the login in a private browser window.
- With HTTPS, the address opens with `https://`.
- The log records every change on a line starting with `Expose:`. It names the ports opened, a switch that is on but opened nothing, and rules put back after a firewall reload.
- For the MTProto proxy, the **Share MTProto connection** dialog warns when the port is not open.

## When it does not work

- **The login is refused after a few attempts.** The address is locked out for 5 minutes.
- **The password is forgotten.** b4 keeps only its hash. With b4 stopped, removing the web server's username and password from the [configuration file](../advanced/config.md#paths-system) and starting b4 again turns the login off.
- **The interface still opens over HTTP.** The dashboard shows **Web server TLS is not usable, serving plain HTTP instead** with the reason, such as a moved or replaced file.
- **The port does not open from outside.** The `Expose:` lines in the log give the reason, and [What the switch cannot open](#expose-limits) lists the causes outside the router.
