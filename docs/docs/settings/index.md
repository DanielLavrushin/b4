---
sidebar_position: 5
title: Settings
---

b4 settings are split across several tabs:

- [Core](./core) - network, queue, features, logging, proxies, devices
- [Geo data](./geodata) - GeoSite and GeoIP databases
- [Security](./security) - authentication, HTTPS
- [Payloads](./payloads) - generation and management of TLS payloads for faking
- [MCP server](./mcp) - letting an external AI application read b4's state
- [Discovery](./discovery) - timeouts, DNS servers, reference domain
- [Backup](./backup) - backup and restore

Changes are applied after clicking the save button. Some parameters (MTProto, queue) require a service restart. The [SOCKS5 proxy](./core#socks5-proxy) does not: it rebinds its own listener and applies credentials and the [allowed sources](./core#allowed-sources) list on save.
