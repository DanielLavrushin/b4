---
sidebar_position: 5
title: Settings
---

The settings page is split into tabs, and the **Core** and **System** tabs into sub-tabs.

![The settings header with the tabs and the sub-tabs of Core](/img/settings/20261003230000.png)

- [Core](./core) - how b4 handles traffic, on the sub-tabs **Packet Engine** (ingestion mode, IPv4 and IPv6, the queue, IP block detection), **Devices** (device filtering), **Firewall** (firewall rules, MSS clamping, NAT Masquerade, DSCP), **DNS** (DNS over TCP and timeouts) and **SOCKS5** (the built-in SOCKS5 proxy)
- [System](./system) - the service itself, on the sub-tabs **Service** (restart, System Info, interface language, time zone, memory limit, update mirrors, logging), **Web Server** (port, TLS, authentication, access from the internet) and **Backup** (backup and restore, configuration download, reset to defaults)
- [Geodat Settings](./geodata) - sources, download and updates of the GeoSite and GeoIP databases
- [Discovery](./discovery) - the timeouts, DNS servers and reference domain of Discovery, and the watchdog
- [Telegram](./mtproto) - the Telegram over WebSocket switch, the MTProto proxy and its secrets, the shared Telegram upstream and the WEB proxy
- **Integrations** - the IPinfo token, the AI assistant, the [MCP server](./mcp) and the connection to the [Community Hub](../community/index.md#switching-it-on)
- [Payloads](./payloads) - generation, upload and management of payloads for faking

Authentication and HTTPS of the web interface, and the **Expose to internet** switches of the web server, the SOCKS5 proxy and the listeners on the **Telegram** tab, are described under [Security](./security).

## Saving and restarting {#saving}

The buttons in the page header act on every tab at once. **Save Changes** writes the changes of every tab, **Discard Changes** drops the unsaved changes after a confirmation, and **Reload** loads the configuration from b4 again, which drops them as well.

While a change is unsaved, a **Modified** chip shows next to the **Configuration** title. A tab or sub-tab with unsaved changes shows a dot. On the sub-tabs of **Core** and **System**, a restart icon with the tooltip **Applies after a restart** takes the place of the dot while a field from the second column of the table has changed. While such a change is unsaved, the header shows **Some changes require a B4 restart to take effect**, and the message that confirms the save carries a **Restart Service** button.

| Sub-tab | Restart icon on a change to | Applies on save |
| --- | --- | --- |
| [Core, Packet Engine](./core#packet-engine) | **Ingestion mode** and the **TUN settings**, **Enable IPv4 Support**, **Enable IPv6 Support**, **Queue Start Number**, **Worker Threads**, **Packet Mark** | **Capture Interfaces**, **Force IPv4 for matched domains**, **TCP Connection Packets Limit**, **UDP Connection Packets Limit**, **IP Block Detection** |
| [Core, Devices](./core#device-filtering) | **Enable Device Filtering**, **Exclude Selected Devices (Blacklist)**, the selection in **Available Devices** | **Vendor Lookup**, device names, the **MSS** column, **Manual Devices** |
| [Core, Firewall](./core#firewall) | **Skip IPTables/NFTables Setup**, **Firewall Engine**, **Firewall Monitor Interval (seconds)** | **Global MSS Clamping**, **NAT Masquerade**, **DSCP** |
| [Core, DNS](./core#dns) | - | Every field |
| [Core, SOCKS5](./core#socks5-proxy) | - | Every field |
| [System, Service](./system#service) | **Instant Flush**, **Syslog**, **Memory Limit** | **Language**, **Time Zone**, **Update mirrors**, **Log Level**, **Log Directory** |
| [System, Web Server](./system#web-server) | **Port**, **Bind Address**, **TLS Certificate**, **TLS Key** | **Username**, **Password**, **Expose to internet** |

While the packet engine is not running, a saved firewall change on these sub-tabs takes effect when b4 restarts.

The [Backup](./system#backup) sub-tab has buttons only, no fields.

The **Telegram** tab applies its changes on save. b4 restarts the MTProto proxy itself when a field that needs it changes.

The **Discovery** tab applies its changes on save as well, and a Discovery run already in progress keeps the settings it started with.
