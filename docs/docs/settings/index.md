---
sidebar_position: 5
title: Settings
---

b4 settings are split across several tabs:

- [Core](./core) - network, queue, features, logging, proxies, devices
- [Capture engine](./engine) - the NFQUEUE and TUN ingestion modes, what TUN needs and what it cannot do
- [MTProto](./mtproto) - the Telegram proxy, its secrets, the shared Telegram upstream and the WEB proxy
- [Geo data](./geodata) - GeoSite and GeoIP databases
- [Security](./security) - authentication, HTTPS
- [Payloads](./payloads) - generation and management of TLS payloads for faking
- [MCP server](./mcp) - letting an external AI application read b4's state
- [Discovery](./discovery) - timeouts, DNS servers, reference domain
- [Backup](./backup) - backup and restore

Changes are applied after clicking the save button. Core settings, the queue among them, require a service restart. MTProto does not: b4 restarts the proxy itself when a field that needs it changes. Neither does the [SOCKS5 proxy](./core#socks5-proxy), which rebinds its own listener and applies credentials and the [allowed sources](./core#allowed-sources) list on save.
