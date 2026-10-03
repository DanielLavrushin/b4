---
sidebar_position: 4
title: Security
---

## Authentication

By default the web interface is open without a password. To restrict access, set a username and password:

1. Go to **Settings -> Core -> Web server**
2. Fill in the **Username** and **Password** fields
3. Save the settings

After that, opening the web interface requires credentials.

:::warning Access from outside
Without authentication, anyone who reaches the web interface port gets full management access. The default bind address `0.0.0.0` listens on every address, IPv6 included, so on a host whose firewall accepts incoming connections, such as a VPS without one, that is anyone on the internet. The web interface's **Expose to internet** switch cannot be turned on without a username and a password; see [Access from the internet](#expose-to-internet).
:::

:::danger Authentication without HTTPS
If authentication is enabled but HTTPS is **not configured**, the username and password are transmitted over the network **in plain text**. Anyone who can intercept traffic (for example, on public Wi-Fi) can see your credentials. Always enable HTTPS together with authentication, especially if b4 is reachable outside the local network.
:::

## HTTPS

To enable HTTPS:

1. Prepare certificate and key files (`.crt`/`.pem` and `.key`/`.pem`)
2. In the web server settings enter the file paths:
   - **TLS Certificate** - path to the certificate file (`.crt` or `.pem`)
   - **TLS Key** - path to the key file (`.key` or `.pem`)
3. Save and restart

For a self-signed certificate (suitable for a local network):

```bash
openssl req -x509 -newkey rsa:2048 -keyout server.key -out server.crt -days 365 -nodes -subj "/CN=b4"
```

Copy the files to the configuration directory (for example, `/etc/b4/`) and point the settings at them.

After HTTPS is enabled, the web interface is available over `https://`.

:::warning Key requirements
The key must be an unencrypted PEM file. A passphrase-protected key (`ENCRYPTED PRIVATE KEY`, or `Proc-Type: 4,ENCRYPTED` in the header) is rejected when the settings are saved; the passphrase is removed with `openssl pkey -in key.pem -out key-plain.pem`. Saving also fails when a file is missing or the certificate does not match the key.

If the files become unusable later (moved, deleted, replaced with a protected key), b4 still starts, records an error in the log, lists it under **Needs attention** on the [dashboard](../dashboard.md#needs-attention), and serves the web interface over plain HTTP until the pair is fixed.
:::

:::info The Telegram WEB proxy needs more than this
The [Telegram Desktop WEB proxy](../telegram/web-proxy.md) is served by this same web server unless it has a relay port of its own, on a hostname of its own and ahead of the authentication above. Telegram Desktop requires a publicly trusted certificate on port 443 for that hostname, so a self-signed pair is not enough there, and requests to it never see the web interface's credentials.
:::

## Access from the internet {#expose-to-internet}

The web interface, the MTProto proxy, the relay port of the Telegram Desktop WEB proxy and the SOCKS5 proxy each have an **Expose to internet** switch next to their port field, off by default. With it on, b4 adds a firewall rule on the host it runs on that accepts TCP connections to that port from any source address, so a firewall that drops incoming connections, as the WAN side of a router and many VPS images do, lets them through. The switches apply on save, with no service restart.

| Listener | Where | Config field | Opens the port only when |
| --- | --- | --- | --- |
| Web interface | Settings, Core, **Web Server** | `system.web_server.expose` | A username and a password are both set |
| MTProto proxy | Settings, Telegram, **MTProto Proxy** | `system.mtproto.expose` | The proxy is enabled |
| WEB proxy relay port | Settings, Telegram, **Telegram Desktop WEB proxy** | `system.mtproto.web_proxy.expose` | The proxy and the WEB carrier are enabled, and **Relay port** holds a port |
| SOCKS5 proxy | Settings, Core, **SOCKS5 Proxy** | `system.socks5.expose` | The proxy is enabled and has credentials or an allowed sources list, and the web interface has a username and a password or has been off (port `0`) since the last start |

### The rule

A listener on `0.0.0.0`, the default, or on `::` accepts IPv4 and IPv6 connections alike, so the rule is added for both families. A listener bound to one address gets a rule for that destination address and its family only, and one bound to a loopback address gets none, since nothing outside the host can reach it.

b4 opens a port only while its own listener holds it. A listener that did not start, for example because another program had taken the port first, gets no rule, with the reason `not_listening`, which the MTProto share dialog shows as a warning. b4 looks at the listeners again at every check of its rules, so a listener that stops later, such as a WEB proxy relay port, loses its rule as well.

The rule goes wherever the host's firewall can drop the connection, whichever **Firewall engine** b4 uses for its own rules:

- **iptables.** A chain `B4_EXPOSE` in the `filter` table holds one `ACCEPT` per exposed port, and a single jump in `INPUT` leads to it. The jump goes right below the last ban-list rule in `INPUT`, so an address banned there stays banned on b4's ports, and to the top of `INPUT` when there is none. A ban-list rule is a jump to a fail2ban `f2b-*` chain or to the chain of CrowdSec or sshguard, a `DROP` or `REJECT` that matches an ipset of source addresses, or ufw's `ufw-before-input` jump, behind which ufw keeps its deny rules and fail2ban's ufw bans. A ban tool that adds its rule below b4's jump later moves the jump down at the next check. IPv4 rules go through `iptables` and IPv6 rules through `ip6tables`, in the nf_tables variant and in the legacy one alike, wherever that variant already has a `filter` table. b4 does not load the legacy kernel modules to create one.
- **nftables.** An `accept` is inserted at the top of every filter chain on the input hook in a table b4 does not own, such as `inet fw4 input` on OpenWrt 22.03 and later and `inet filter input` from `/etc/nftables.conf`, with a comment of the form `b4-expose:mtproto`. A chain whose policy is `accept` and whose priority is below `filter` gets no rule. That is where fail2ban's `f2b-table`, CrowdSec's `crowdsec` tables and banIP place their chains, so the addresses they ban stay banned on b4's ports, and fw4's `mangle_input` is such a chain as well. The `INPUT` chain of iptables-nft's own `filter` table gets its rule through the iptables tools instead, because a native nftables rule in it breaks those tools; a native chain in a table of the same name, such as `table ip filter { chain input ... }`, is treated like any other.

A host with no such chain at all has nothing that drops the connection, and b4 adds no rule there.

:::info Why the rule goes into the system's chains
In nftables an `accept` only ends the packet's path through one base chain. Every other base chain on the input hook still sees the packet, and a `drop` in any of them is final. An accept in a table of b4's own would leave the port closed behind the system firewall's drop.
:::

### Firewall reloads

Router firmware rebuilds its firewall on its own and takes b4's rule with it: OpenWrt's fw4 flushes its table on every reload, which an interface coming up triggers, ASUS firmware rebuilds the `filter` table on every firewall restart, and Keenetic NDMS removes the chains it does not own on every [rewrite](../install/keenetic.md#firewall-rewrites). b4 checks its rules at the **Firewall monitor interval** (`system.tables.monitor_interval`, 10 seconds by default) and on `SIGUSR1`, puts back the missing ones, and adds the rule to any input chain that has appeared since the previous check. With the interval at `0` the timed check is off, and only `SIGUSR1` makes b4 look for missing rules. The check runs whatever the packet engine: in TUN mode, and while the engine has failed to start, as well.

Turning the switch off, turning the listener off or stopping b4 removes the rule, and so does `b4 --clear-tables`; once shutdown has removed it, no check puts it back. A save removes the rules of the ports it closes before the listeners take the new settings. A rule that cannot be removed, for example while another program holds the xtables lock, is reported and removed at the next check. At start, b4 removes rules left behind by a run that ended without cleaning up, unless **Skip IPTables/NFTables setup** is on: b4 then does not touch the firewall, and `b4 --clear-tables` removes them.

### Off adds nothing

With the switch off, b4 adds no rule, and it does not block the port either. On a host whose firewall accepts incoming connections, a VPS with no firewall configured for example, every enabled listener on `0.0.0.0` is reachable from the internet over IPv4 and IPv6 whatever the switch says. A listener stays private through its bind address, a LAN address or `127.0.0.1`, or through the host's own firewall.

### Per listener

- **Web interface.** The same port serves the API, the [MCP endpoint](./mcp.md) and, when the WEB proxy has no relay port of its own, the relay, so all of them are exposed together. A save that turns the switch on without a username and a password is refused, and so is a save that clears either of them while the switch is on; a configuration file that has the switch on without them anyway gets no rule for the web interface. A connection opened while a login was required, or within two minutes after it was turned off, keeps requiring a session token for as long as it stays open, whatever the protocol, and a request refused on it closes the connection, so clearing the username or the password later does not let it carry on without one. Without [HTTPS](#https) the login and every session token cross the internet in plain text, which the settings page warns about. The web server binds its port once, at start, so the rule follows the port the running server listens on, and a changed port or bind address moves the rule with the next restart.
- **MTProto proxy.** The secrets control access. A connection that fails the fake-TLS handshake is answered as described under [What an unrecognised connection sees](../telegram/mtproto-proxy.md#what-an-unrecognised-connection-sees), and scans and probes count towards **Max Connections**.
- **WEB proxy relay port.** The switch is shown while **Relay port** holds a port, and also while it is on with the field empty, so that it can be turned off. With the field empty the relay is served on the web server's port and has no port of its own to open; it is then reachable from outside only through the web interface's switch, which exposes the interface along with it.
- **SOCKS5 proxy.** A proxy with neither credentials nor an allowed sources list relays traffic for anyone who reaches it, so a save that would expose it that way is refused, and a configuration file that has it that way anyway gets no rule. The rule accepts every source address; the allowed sources list is still applied by the proxy itself, at accept. The proxy does not restrict destinations: a client it accepts can connect to any address, the LAN and the router's own ports included, the web interface among them. While the web server is on, exposing the proxy therefore also needs a username and a password on the web interface: a save that would expose the proxy while the interface has none is refused, and so is a save that clears either of them while the proxy is exposed; a configuration file in that state gets no rule for the proxy. SOCKS5 sends the username and password unencrypted. The rule covers TCP only: UDP ASSOCIATE relays each association on a port the kernel picks, so on a host that drops incoming connections UDP through the proxy does not work from outside.

### What the switch cannot open {#expose-limits}

The rule opens the port on the host b4 runs on and nowhere else.

- **Carrier-grade NAT.** A WAN address in `100.64.0.0/10` sits behind the provider's own NAT, and no connection from the internet reaches it over IPv4. A public IPv6 address on the WAN, where the provider assigns one, is not affected.
- **A router or modem in front.** A private WAN address (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`) means another device translates addresses ahead of b4, and the port also has to be forwarded on it. Some providers run carrier-grade NAT in private address space, where no forward is possible.
- **Cloud firewalls.** AWS security groups, Azure network security groups, Google Cloud firewall rules and Oracle Cloud security lists filter traffic before it reaches the machine, and each needs a rule of its own. A cloud machine often carries only a private address on its network card, with the provider mapping the public address to it; that mapping needs no forward, only the provider's rule.
- **A MikroTik container.** b4 changes the firewall inside the container, while RouterOS filters and translates what arrives from the WAN. A connection from outside reaches the container only through a `dst-nat` rule on RouterOS, described under [MikroTik](../install/mikrotik.md#access-from-the-wan).
- **A table that belongs to another program.** The kernel refuses changes from other programs to an nftables table that its creator marked as owned. firewalld 2.2 and later marks its table that way (`NftablesTableOwner=yes`, the default) where the kernel and nftables support it, from Linux 6.9 and nftables 1.1 on, as on Fedora 42, RHEL 10 and Debian 13. b4 recognises the refusal, `Operation not permitted`, tries that table again every 10 minutes rather than at every check, and reports it in the log and in `GET /api/system/addresses`; for firewalld the report names the commands that open the port in firewalld itself, `firewall-cmd --permanent --add-port=<port>/tcp` followed by `firewall-cmd --reload`, and the alternative, `NftablesTableOwner=no` in `/etc/firewalld/firewalld.conf`. Without the flag, firewalld's `filter_INPUT` chain takes b4's rule like any other input chain. The rule then sits at the top of `filter_INPUT`, above firewalld's zones, so an address blocked through a zone, a rich rule or fail2ban's firewalld actions can still reach the exposed ports.
- **Skip IPTables/NFTables setup.** With it on, b4 adds no exposure rule. The switches are greyed out, one that is already on can still be turned off, and turning the setting on removes the rules b4 had added.

The DNS-over-TCP listener (`system.dns.tcp_port`, 5453 by default) and the transparent-proxy listeners of proxy sets and of Telegram over WebSocket have no switch. They exist only as targets of b4's own redirect and diversion rules.

The switches cannot be changed over MCP, and an MCP write that would open a port while one of them is on is refused, such as turning the MTProto proxy on or changing its port with `system.mtproto.expose` on. See [MCP server](./mcp.md#changing-settings).

:::info Checking what is open
The log records every change on lines that start with `Expose:`: the ports opened and the chains that hold their rules, a switch that is on but opened nothing and the reason, a rule that could not be added or removed, and rules put back after a firewall reload. `GET /api/system/addresses` reports the same state: the exposed ports, the chains, the error for a chain where a rule could not be added or removed, and for a switch whose precondition is not met the reason, `no_auth`, `web_no_auth`, `open_relay`, `shared_port`, `loopback`, `invalid_bind` or `not_listening`. The MTProto share dialog reads that report to warn when the proxy port is not open.
:::
