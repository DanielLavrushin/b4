---
sidebar_position: 5
title: Settings
---

b4 settings are split across several tabs:

- [Core](./core) - network, queue, features, logging, proxies, devices
- [Telegram](./mtproto) - the Telegram over WebSocket switch, the MTProto proxy and its secrets, the shared Telegram upstream and the WEB proxy
- [Geo data](./geodata) - GeoSite and GeoIP databases
- [Security](./security) - authentication, HTTPS
- [Payloads](./payloads) - generation and management of TLS payloads for faking
- [MCP server](./mcp) - letting an external AI application read b4's state
- [Discovery](./discovery) - timeouts, DNS servers, reference domain
- [Backup](./backup) - backup and restore

The **Integrations** tab also holds the connection to the community hub, described under [Community Hub](../community/index.md#switching-it-on).

Changes are applied after clicking the save button. Core settings, the queue among them, require a service restart. The Telegram tab does not: b4 restarts the MTProto proxy itself when a field that needs it changes. Neither does the [SOCKS5 proxy](./core#socks5-proxy), which rebinds its own listener and applies credentials and the [allowed sources](./core#allowed-sources) list on save.
