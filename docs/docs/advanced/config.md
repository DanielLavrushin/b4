---
sidebar_position: 2
title: Configuration file
description: Where b4 keeps its configuration, how the file is laid out, and which path stores each setting of the web interface.
---

b4 keeps its configuration in one JSON file, `/etc/b4/b4.json` by default. The `--config` [flag](./cli.md) points it elsewhere.

## Where it lives

| Platform | Path |
| --- | --- |
| Linux | `/etc/b4/b4.json` |
| OpenWRT (with extroot or USB storage) | `/opt/etc/b4/b4.json` |
| OpenWRT (without USB storage) | `/etc/b4/b4.json` |
| ASUS Merlin | `/opt/etc/b4/b4.json` |
| Keenetic | `/opt/etc/b4/b4.json` |
| Docker | `/etc/b4/b4.json`, inside the container |

Without `--config`, b4 looks for `b4.json` and `config.json` under `/etc/b4` and `/opt/etc/b4`, and falls back to `/etc/b4/b4.json`. The log names the path it settled on at startup.

The **Download Configuration** card under [Settings > System > Backup](../settings/system.md) saves the configuration in the form b4 writes to this file. Its [safe copy](../settings/system.md#safe-copy) masks credentials and private host names for sharing, and b4 refuses to load or save a configuration that still holds the safe copy's `[redacted]` placeholders.

## Only what differs from the defaults is stored

The file is sparse. A setting that holds its built-in default is left out, so a fresh installation writes a very short file, and a section whose settings were never changed is absent. A missing key is not a missing setting, and adding it back with its default value changes nothing. Saves from the web interface, the API and MCP write the file the same way.

## Structure

```json
{
  "version": 52,
  "queue": {
    "start_num": 537,
    "threads": 4,
    "mark": 32768,
    "ipv4": true,
    "ipv6": false,
    "tcp_conn_bytes_limit": 19,
    "udp_conn_bytes_limit": 8,
    "interfaces": [],
    "mss_clamp": { "enabled": false, "size": 88 },
    "devices": { "enabled": false, "vendor_lookup": false }
  },
  "system": {
    "tables": {
      "skip_setup": false,
      "monitor_interval": 10,
      "engine": "",
      "masquerade": { "enabled": false, "interfaces": [] },
      "dscp": { "enabled": false, "value": 0, "interfaces": [] }
    },
    "logging": {
      "level": 1,
      "directory": "/var/log/b4",
      "instaflush": true,
      "syslog": false
    },
    "web_server": {
      "port": 7000,
      "bind_address": "0.0.0.0",
      "expose": false,
      "tls_cert": "",
      "tls_key": "",
      "username": "",
      "password": "",
      "language": "en",
      "mcp": { "enabled": false, "allow_writes": false }
    },
    "dns": {
      "tcp_disabled": false,
      "tcp_port": 5453,
      "query_timeout_sec": 5,
      "keep_ipv6_answers": false
    },
    "socks5": {
      "enabled": false,
      "port": 1080,
      "bind_address": "0.0.0.0",
      "expose": false,
      "allowed_sources": ["192.168.1.0/24", "127.0.0.1/32"]
    },
    "mtproto": { "enabled": false, "port": 3128, "bind_address": "0.0.0.0", "expose": false },
    "checker": {
      "discovery_timeout": 5,
      "config_propagate_ms": 1500,
      "dns_server": ""
    },
    "geo": { "sitedat_path": "", "ipdat_path": "", "sitedat_url": "", "ipdat_url": "" },
    "timezone": ""
  },
  "sets": []
}
```

The sample shows some of the sections and some of their keys. A real file holds only the keys that differ from the default.

- `system.mtproto.secrets` is a list of named secrets, each with `id`, `name`, `secret` and `enabled`. A file written before configuration version 50 held a single `mtproto.secret` string, which the migration moves into the list as an entry named `default`.
- `system.socks5.allowed_sources` is absent while the list is empty, the default, and the proxy then accepts any source. Each entry is an IP address or a CIDR range. `0.0.0.0/0`, `::/0` and malformed entries are refused when the file is loaded or saved.

## The `queue` section

| Key | Meaning | Default |
| --- | --- | --- |
| `start_num` | The first NFQUEUE number | `537` |
| `threads` | Number of worker threads | `4` |
| `mark` | The queue mark, see [Packet marks](../guides/marks.md) | `32768` |
| `ipv4` | Process IPv4 traffic | `true` |
| `ipv6` | Process IPv6 traffic | `false` |
| `tcp_conn_bytes_limit` | How many packets at the start of a TCP connection b4 inspects | `19` |
| `udp_conn_bytes_limit` | The same for UDP | `8` |
| `interfaces` | **Capture Interfaces**, a filter on the interface a packet passes. Empty means all of them | `[]` |

`queue.ipv6` decides whether b4 processes IPv6, not whether the router uses it. Off, the default, b4 processes IPv4 only and writes no IPv6 firewall rules for any set, so a client that reaches a site over IPv6 bypasses the sets. b4 then removes IPv6 addresses from DNS answers for the domains sets match, see [DNS](../dns.md). The exception is NFQUEUE mode while an enabled set has `dns.strip_ech` on. In that mode b4 writes IPv6 rules for DNS answers and DNS over TCP, only to remove their [ECH keys](../dns.md#removing-ech-keys). A change takes full effect after a restart. `--ipv6` sets the same thing for one run.

## The `sets` section

Each set is one object in the `sets` array, carrying its whole configuration. Its keys follow the tabs of the set editor:

- `targets` - domains, IPs, GeoSite and GeoIP categories, ASNs, source devices
- `tcp` - general TCP settings, desync, window, incoming, RST protection
- `fragmentation` - the fragmentation method and its parameters
- `faking` - SNI faking, SYN fakes, mutation
- `udp` - the QUIC filter, the port filter, protocol selection and the UDP action mode
- `dns` - the set's resolver, DoH URL and pins
- `routing` - routing mode, output interface, upstream proxy or blocking
- `dscp` - the set's own DSCP value
- `discovery` - the set's Discovery addresses and its watchdog switch
- `escalate` - which set to escalate to when this one stops working

The **Import/Export** tab of the set editor shows a set as JSON and takes a pasted one back. The exported JSON leaves out values equal to the defaults and the settings of switched-off features. It also leaves out the set's id, source devices, watchdog switch and escalation settings, and, when the set has source devices, its `dscp`.

### A set's `dscp` {#set-dscp}

| Key | Meaning | Default |
| --- | --- | --- |
| `dscp.enabled` | **Enable per-set DSCP**: write the set's own value into the packets to the set's addresses, see [DSCP](../sets/routing.md#dscp) | `false` |
| `dscp.value` | The value written into the DSCP field, 0-63. `0` clears the field | `0` |

Each key is stored only while it differs from its default: the switch on with the value `0` is saved as `"dscp": {"enabled": true}`, a value kept with the switch off as `"dscp": {"value": 12}`. While `dscp.enabled` is `true`, a `dscp.value` outside 0-63 is refused, not clamped: the web interface refuses the save, and a file that holds one stops b4 at start. Neither key is writable over MCP or carried in a [shared set](../sets/sharing.md).

### A set's `udp.filter_l7` {#set-udp-filter-l7}

`udp.filter_l7` lists the protocols selected in **Protocols**: `discord` for **Discord voice setup**, `stun` for **STUN**, see [Selecting packets by protocol](../sets/udp.md#protocol-selection). An empty list is not stored. A set with the list needs a `udp.dport_filter`, no `targets.sni_domains`, `targets.ip`, `targets.geosite_categories`, `targets.geoip_categories` or `targets.asns`, and an empty `tcp.dport_filter`. A set that breaks this is refused: the web interface refuses the save, and a file that holds one stops b4 at start. An unknown entry stays in the list, matches no traffic and is logged as a warning. **(auto: STUN request)** is `"fake_payload_file": "@stun"` under `udp`. `udp.filter_l7` is writable over MCP, with `discord` and `stun` as its only entries, and is carried in a [shared set](../sets/sharing.md).

## Settings and their configuration paths {#settings-and-their-configuration-paths}

The tables list the fields of the web interface with the path that stores each one. Paths under the set editor are relative to one set object in the `sets` array, so `dns.strict` is `sets[].dns.strict`.

### Settings > Core {#paths-core}

| Sub-tab | Field | Path |
| --- | --- | --- |
| Packet Engine | **Ingestion mode** | `queue.mode` |
| Packet Engine | **Uplink interface** | `queue.tun.out_interface` |
| Packet Engine | **Uplink gateway** | `queue.tun.out_gateway` |
| Packet Engine | **TUN address** | `queue.tun.address` |
| Packet Engine | **TUN device name** | `queue.tun.device_name` |
| Packet Engine | **Capture Interfaces** | `queue.interfaces` |
| Packet Engine | **Retest interval (seconds)** | `system.ip_health.retest_interval_sec` |
| Packet Engine | **Enable IPv4 Support** | `queue.ipv4` |
| Packet Engine | **Enable IPv6 Support** | `queue.ipv6` |
| Packet Engine | **Force IPv4 for matched domains** | `system.dns.keep_ipv6_answers`, inverted: the switch on is `false` |
| Packet Engine | **Queue Start Number** | `queue.start_num` |
| Packet Engine | **Packet Mark** | `queue.mark` |
| Packet Engine | **Worker Threads** | `queue.threads` |
| Packet Engine | **TCP Connection Packets Limit** | `queue.tcp_conn_bytes_limit` |
| Packet Engine | **UDP Connection Packets Limit** | `queue.udp_conn_bytes_limit` |
| Devices | **Enable Device Filtering** | `queue.devices.enabled` |
| Devices | **Exclude Selected Devices (Blacklist)** | `queue.devices.wisb` |
| Devices | **Vendor Lookup** | `queue.devices.vendor_lookup` |
| Devices | The devices selected in **Available Devices** and added in **Manual Devices** | `queue.devices.devices` |
| Firewall | **Skip IPTables/NFTables Setup** | `system.tables.skip_setup` |
| Firewall | **Firewall Engine** | `system.tables.engine` |
| Firewall | **Firewall Monitor Interval (seconds)** | `system.tables.monitor_interval` |
| Firewall | **Enable Global MSS Clamping** | `queue.mss_clamp.enabled` |
| Firewall | **MSS Size** | `queue.mss_clamp.size` |
| Firewall | **Enable NAT Masquerade** | `system.tables.masquerade.enabled` |
| Firewall | The interface tags of NAT Masquerade | `system.tables.masquerade.interfaces` |
| Firewall | **Set DSCP** | `system.tables.dscp.enabled` |
| Firewall | **DSCP value** | `system.tables.dscp.value` |
| Firewall | The interface tags of DSCP | `system.tables.dscp.interfaces` |
| DNS | **Intercept DNS over TCP** | `system.dns.tcp_disabled`, inverted: the switch on is `false` |
| DNS | **Listener port** | `system.dns.tcp_port` |
| DNS | **Query timeout** | `system.dns.query_timeout_sec` |
| DNS | **Idle timeout** | `system.dns.tcp_idle_sec` |
| DNS | **Read/write timeout** | `system.dns.tcp_io_sec` |
| DNS | **Forward timeout** | `system.dns.tcp_dial_sec` |
| SOCKS5 | **Enable SOCKS5 Proxy** | `system.socks5.enabled` |
| SOCKS5 | **Bind Address** | `system.socks5.bind_address` |
| SOCKS5 | **Port** | `system.socks5.port` |
| SOCKS5 | **Expose to internet** | `system.socks5.expose` |
| SOCKS5 | **Username** | `system.socks5.username` |
| SOCKS5 | **Password** | `system.socks5.password` |
| SOCKS5 | **Allowed Sources** | `system.socks5.allowed_sources` |

### Settings > System {#paths-system}

| Sub-tab | Field | Path |
| --- | --- | --- |
| Service | **Language** | `system.web_server.language` |
| Service | **Time Zone** | `system.timezone` |
| Service | **Memory Limit** | `system.memory_limit` |
| Service | **Update mirrors** | `system.update.mirrors` |
| Service | **Log Level** | `system.logging.level` |
| Service | **Log Directory** | `system.logging.directory` |
| Service | **Instant Flush** | `system.logging.instaflush` |
| Service | **Syslog** | `system.logging.syslog` |
| Web Server | **Bind Address** | `system.web_server.bind_address` |
| Web Server | **Port** | `system.web_server.port` |
| Web Server | **Expose to internet** | `system.web_server.expose` |
| Web Server | **TLS Certificate** | `system.web_server.tls_cert` |
| Web Server | **TLS Key** | `system.web_server.tls_key` |
| Web Server | **Username** | `system.web_server.username` |
| Web Server | **Password** | `system.web_server.password`, stored as a hash |

### Settings > Geodat Settings {#paths-geodat}

| Card | Field | Path |
| --- | --- | --- |
| Geosite Database | The file that **Update** or **Upload** writes | `system.geo.sitedat_path` |
| Geosite Database | **Source** | `system.geo.sitedat_url` |
| GeoIP Database | The file that **Update** or **Upload** writes | `system.geo.ipdat_path` |
| GeoIP Database | **Source** | `system.geo.ipdat_url` |
| Auto-update | **Refresh on startup** | `system.geo.auto_update.on_startup` |
| Auto-update | **Schedule** | `system.geo.auto_update.interval`: `daily`, `weekly` or `monthly` |
| Auto-update | **Last run** | `system.geo.auto_update.last_run`, written by b4 |

### Settings > Discovery {#paths-discovery}

| Card | Field | Path |
| --- | --- | --- |
| Testing Configuration | **Discovery Timeout** | `system.checker.discovery_timeout` |
| Testing Configuration | **Config Propagation Delay** | `system.checker.config_propagate_ms` |
| Testing Configuration | **Trusted DNS server** | `system.checker.dns_server` |
| Watchdog | **Enable Watchdog** | `system.checker.watchdog.enabled` |
| Watchdog | **Check Interval** | `system.checker.watchdog.interval_sec` |
| Watchdog | **Failure Re-check Interval** | `system.checker.watchdog.failure_interval` |
| Watchdog | **Healing Cooldown** | `system.checker.watchdog.cooldown_sec` |
| Watchdog | **Check Timeout** | `system.checker.watchdog.timeout_sec` |
| Watchdog | **Max Retries** | `system.checker.watchdog.max_retries` |
| Watchdog | **Older per-domain list** | `system.checker.watchdog.domains` |

### Settings > Telegram {#paths-telegram}

| Card | Field | Path |
| --- | --- | --- |
| MTProto Proxy | **Enable MTProto Proxy** | `system.mtproto.enabled` |
| MTProto Proxy | **Bind Address** | `system.mtproto.bind_address` |
| MTProto Proxy | **Port** | `system.mtproto.port` |
| MTProto Proxy | **Expose to internet** | `system.mtproto.expose` |
| MTProto Proxy | **Fake SNI Domain** | `system.mtproto.fake_sni` |
| MTProto Proxy | **Max Connections** | `system.mtproto.max_connections` |
| MTProto Proxy | **TCP User Timeout (sec)** | `system.mtproto.tcp_user_timeout_sec` |
| MTProto Proxy | **Idle Timeout (sec)** | `system.mtproto.idle_timeout_sec` |
| Secrets | The list of secrets | `system.mtproto.secrets` |
| Telegram upstream (shared) | **Transport mode** | `system.mtproto.upstream_mode` |
| Telegram upstream (shared) | **DC Relay** | `system.mtproto.dc_relay` |
| Telegram upstream (shared) | **Cloudflare Worker domain** | `system.mtproto.cfworker_domain` |
| Telegram upstream (shared) | **Let sets process Worker connections** | `system.mtproto.cfworker_dpi` |
| Telegram upstream (shared) | **Custom WebSocket domain** | `system.mtproto.ws_custom_domain` |
| Telegram upstream (shared) | **Telegram WS edge IP (advanced)** | `system.mtproto.ws_endpoint_host` |
| Telegram upstream (shared) | **Fronting name for Telegram's WS edge (advanced)** | `system.mtproto.ws_front_sni` |
| Telegram upstream (shared) | **CF proxy fallback** | `system.mtproto.cfproxy_enabled` |
| Telegram upstream (shared) | **CF proxy domain list URL** | `system.mtproto.cfproxy_url` |
| Telegram upstream (shared) | **DC list fallback mirror** | `system.mtproto.dc_fallback_enabled` |
| Telegram upstream (shared) | **DC list mirror URL** | `system.mtproto.dc_fallback_url` |
| Telegram upstream (shared) | **Bridge Handshake Wait (sec)** | `system.mtproto.bridge_wait_sec` |
| Telegram over WebSocket | **Enable Telegram over WebSocket** | `system.mtproto.bridge.enabled` |
| Telegram Desktop WEB proxy | **Enable the WEB carrier** | `system.mtproto.web_proxy.enabled` |
| Telegram Desktop WEB proxy | **Relay hostname** | `system.mtproto.web_proxy.hostname` |
| Telegram Desktop WEB proxy | **Relay port** | `system.mtproto.web_proxy.port` |
| Telegram Desktop WEB proxy | **Expose to internet** | `system.mtproto.web_proxy.expose` |
| Telegram Desktop WEB proxy | **Certificate file** | `system.mtproto.web_proxy.tls_cert` |
| Telegram Desktop WEB proxy | **Private key file** | `system.mtproto.web_proxy.tls_key` |

### Settings > Integrations {#paths-integrations}

| Card | Field | Path |
| --- | --- | --- |
| IPinfo | **Access token** | `system.api.ipinfo_token` |
| AI Assistant | **Enable AI assistance** | `system.ai.enabled` |
| AI Assistant | The provider tabs | `system.ai.provider` |
| AI Assistant | **Model** | `system.ai.model` |
| AI Assistant | **Endpoint** | `system.ai.endpoint` |
| AI Assistant | **Max tokens** | `system.ai.max_tokens` |
| AI Assistant | **Temperature** | `system.ai.temperature` |
| AI Assistant | **Timeout (s)** | `system.ai.timeout_sec` |
| MCP server | **Enable MCP server** | `system.web_server.mcp.enabled` |
| MCP server | **Access token** | `system.web_server.mcp.token` |
| MCP server | **Allow configuration changes** | `system.web_server.mcp.allow_writes` |
| MCP server | **Allow active probes** | `system.web_server.mcp.allow_active_probes` |
| Community Hub | **Enable Community Hub** | `system.hub.enabled` |
| Community Hub | **Hub mirrors** | `system.hub.urls` |
| Community Hub | **Hub public key** | `system.hub.public_key` |

The AI API key is not part of this file. b4 keeps it in `ai_secrets.json`.

### Set editor {#paths-sets}

| Tab | Field | Path |
| --- | --- | --- |
| Targets | **IP Version Filter** | `targets.ip_version` |
| Targets | **Domain-only matching** | `targets.domain_only` |
| TCP > General | **Port Filter** | `tcp.dport_filter` |
| TCP > Faking | **Generated Payload** | `faking.payload_file`, a path such as `captures/tls_youtube_com.bin` |
| UDP | **QUIC Matching** | `udp.filter_quic` |
| UDP | **Port Filter** | `udp.dport_filter` |
| UDP | **Protocols** | `udp.filter_l7`, `discord` for **Discord voice setup**, `stun` for **STUN** |
| UDP | **Action Mode** | `udp.mode` |
| UDP | **Fake Packet Payload** | `udp.fake_payload_file`, `@stun` for **(auto: STUN request)** |
| DNS & Routing | **Enable DNS Redirect** | `dns.enabled` |
| DNS & Routing | **DNS Server IP** | `dns.target_dns` |
| DNS & Routing | **DNS-over-HTTPS URL** | `dns.doh_url` |
| DNS & Routing | **Fragment DNS Queries** | `dns.fragment_query` |
| DNS & Routing | **Fail closed when the resolver is unreachable** | `dns.strict` |
| DNS & Routing | **Remove ECH keys from DNS answers** | `dns.strip_ech` |
| DNS & Routing | **Pinned addresses** | `dns.pins`, each name mapped to its addresses |
| DNS & Routing | **Enable per-set DSCP** | `dscp.enabled` |
| DNS & Routing | **DSCP value** | `dscp.value` |
| Discovery | The list of addresses | `discovery.urls` |
| Discovery | **Keep this set working with the watchdog** | `discovery.watchdog` |

### Dashboard {#paths-dashboard}

| Control | Path |
| --- | --- |
| The layout saved by **Customize** | `ui.dashboard` |

### Settings without a field {#settings-without-a-field}

These settings exist only in the file.

| Path | What it does | Default |
| --- | --- | --- |
| `system.web_server.mcp.allowed_origins` | Browser origins the [MCP endpoint](./api.md#mcp-server) accepts besides b4's own address. A single `*` accepts any origin | Empty |
| `system.checker.watchdog.heal_validation_tries` | The number of fetches in a row a strategy has to pass during a heal's search | `3` |
| `system.checker.watchdog.verify_tries` | The number of tries each address gets in the check after a heal writes a strategy | `2` |
| `system.checker.discovery_flow_mark`, `system.checker.discovery_injected_mark` | The marks of Discovery's test traffic, see [Packet marks](../guides/marks.md) | The queue mark plus 1 and plus 2 |
| `queue.tun.route_table` | A fixed routing table for the TUN engine instead of a free one b4 picks, see [Packet marks](../guides/marks.md) | Picked by b4 |
| `sets[].routing.fwmark`, `sets[].routing.table` | A pinned mark and table for a set that routes traffic, used only when both are set, see [Packet marks](../guides/marks.md) | Picked by b4 |

## Editing it by hand

`version` at the top is the configuration format version. b4 reads it to decide what needs migrating, so it stays unchanged when anything else is edited.

:::warning
The values in the file are checked only when b4 starts, and a file that fails the check stops b4 with an `invalid configuration` error. b4 reads the file only at start, so a manual edit takes effect after a restart. Any save before then writes the running configuration over the file, and the manual edit is lost. Saves come from the web interface, MCP, a watchdog heal, a geodata download and an ASN prefix update.
:::

## Migrations

When the format changes between releases, b4 migrates the file at startup: new fields arrive with their defaults, and renamed ones are carried over. Before it changes anything, it writes a backup next to the file, named after the version it migrates from, for example `b4.json.v51.bak`.

A file that cannot be read at startup, for example one cut short by a power loss or left with a JSON syntax error, is copied next to itself as `b4.json.corrupt`. When an earlier copy with different content is there, the name is `b4.json.corrupt.1` and onward. The log names the copy. b4 then starts on the defaults, and the next save replaces the original file, so the unreadable settings remain only in that copy.

A file whose `version` is higher than the running binary understands is loaded as it is, with a warning. Settings that binary does not know are dropped at the next save.
