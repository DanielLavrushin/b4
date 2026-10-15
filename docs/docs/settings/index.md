---
sidebar_position: 5
title: Settings
description: The tabs of the Settings page, and how changes are saved and applied.
---

The **Settings** page holds the configuration of b4 itself. Sets have their own page and editor.

![The settings header with the tabs and the sub-tabs of Core](/img/settings/20261003230000.png)

| Tab | What it holds |
| --- | --- |
| [Core](./core.md) | How traffic reaches b4 and what b4 does with it, on the sub-tabs **Packet Engine**, **Devices**, **Firewall**, **DNS** and **SOCKS5** |
| [System](./system.md) | The service itself, on the sub-tabs **Service** (restart, **System Info**, language, logging), **Web Server** and **Backup** |
| [Geodat Settings](./geodata.md) | The GeoSite and GeoIP databases: sources, downloads and updates |
| [Discovery](./discovery.md) | Discovery and the watchdog |
| [Telegram](./mtproto.md) | Telegram over WebSocket, the MTProto proxy and the WEB proxy |
| [Integrations](./integrations.md) | The IPinfo token, the AI assistant, the MCP server and the Community Hub |
| [Payloads](./payloads.md) | Payloads for faking |

The login, HTTPS and the **Expose to internet** switches are covered in [Securing the web interface](./security.md).

## Saving and restarting {#saving}

The buttons in the page header act on every tab at once:

- **Save Changes** saves the changes of every tab.
- **Discard Changes** drops the unsaved changes after a confirmation.
- **Reload** loads the configuration from b4 again, which drops unsaved changes as well.

While a change is unsaved, **Modified** shows next to the **Configuration** title, and the tab with the change shows a dot.

Most changes apply on save. Some fields on **Core** and **System**, such as **Ingestion mode** or the web server **Port**, apply only after b4 restarts. A sub-tab with such a change shows a restart icon with the tooltip **Applies after a restart**, and the header shows **Some changes require a B4 restart to take effect**. The message that confirms the save then offers **Restart Service**.
