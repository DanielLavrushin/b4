---
sidebar_position: 2
title: Configuration file
---

b4 keeps its configuration in a single JSON file. The default is `/etc/b4/b4.json`, and `--config` points it somewhere else.

## Where it lives

| Platform | Path |
| --- | --- |
| Linux | `/etc/b4/b4.json` |
| OpenWRT (with extroot or USB storage) | `/opt/etc/b4/b4.json` |
| OpenWRT (without USB storage) | `/etc/b4/b4.json` |
| ASUS Merlin | `/opt/etc/b4/b4.json` |
| Keenetic | `/opt/etc/b4/b4.json` |
| Docker | `/etc/b4/b4.json`, inside the container |

Without `--config`, b4 looks for `b4.json` and `config.json` under `/etc/b4` and `/opt/etc/b4`, and falls back to `/etc/b4/b4.json`. The path it settled on is written to the log at startup.

## Only what differs from the defaults is stored

The file is sparse. A setting that still holds its built-in default is left out of it entirely, so a fresh installation produces a very short file, and a section you have never touched is simply absent. That is not a sign of a missing setting, and adding it back by hand with its default value changes nothing.

The same applies through the API and the web interface: what you read back is what differs from the defaults, not the full effective configuration.

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
      "masquerade": { "enabled": false, "interfaces": [] }
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
      "allowed_sources": ["192.168.1.0/24", "127.0.0.1/32"]
    },
    "mtproto": { "enabled": false, "port": 3128, "bind_address": "0.0.0.0" },
    "checker": {
      "discovery_timeout": 5,
      "config_propagate_ms": 1500,
      "reference_domain": "yandex.ru",
      "validation_tries": 1
    },
    "geo": { "sitedat_path": "", "ipdat_path": "", "sitedat_url": "", "ipdat_url": "" },
    "timezone": ""
  },
  "sets": []
}
```

The sample is trimmed to the sections worth recognising. Every section holds more keys than are shown, and none of them appear in a real file until they differ from the default.

`mtproto` holds more than the three keys above. `secrets` is an array of named entries, each with `id`, `name`, `secret` and `enabled`; a file written before configuration version 50 carried a single `mtproto.secret` string, which the migration moves into that array as an entry named `default`. `web_proxy` is an object with `enabled` and `hostname`, and both of its fields are zero-valued by default, so a working relay is the only reason they appear in a file at all. See [Settings, MTProto](../settings/mtproto.md).

`socks5.allowed_sources` is one of those keys: it is absent while the list is empty, which is the default and means the proxy accepts a connection from any source. Each entry is an IP address or a CIDR range, and `0.0.0.0/0`, `::/0` and malformed entries are refused when the file is loaded or saved. It gates which addresses reach the proxy, the way a firewall rule does, and is not a substitute for the username and password. See [Allowed sources](../settings/core#allowed-sources).

## The `queue` section

| Key | Meaning | Default |
| --- | --- | --- |
| `start_num` | Netfilter queue number the workers bind to | `537` |
| `threads` | Number of worker threads | `4` |
| `mark` | Packet mark b4 puts on traffic it has already handled | `32768` |
| `ipv4` | Process IPv4 traffic | `true` |
| `ipv6` | Process IPv6 traffic | `false` |
| `tcp_conn_bytes_limit` | Global ceiling on how many TCP packets per connection are analysed | `19` |
| `udp_conn_bytes_limit` | Global ceiling on how many UDP packets per connection are analysed | `8` |
| `interfaces` | Interfaces to attach the rules to. Empty means all of them | `[]` |

### `queue.ipv6`

`queue.ipv6` is the **IPv6 support** switch from [Settings -> Core](../settings/core#protocols), and `--ipv6` on the command line sets the same thing for one run. It says which address families **b4** processes, not what the router does with IPv6.

Off, which is the default, means b4 binds its queue to IPv4 only and writes no IPv6 firewall rules for any set. Bypass strategies, routing, blocking and a proxy set's QUIC refusal then exist on IPv4 alone, so a destination that also answers over IPv6 is reachable there with nothing in the way. b4 logs a warning when the host has a working global IPv6 address while this is off, and it strips IPv6 addresses out of DNS answers for matched domains to keep clients on the protected IPv4 path. See [The IPv4 fallback](../dns#the-ipv4-fallback).

The address families are bound when the service starts, so a change here needs a restart to take full effect.

## The `sets` section

Each set is one object in the `sets` array, carrying its whole configuration. Its keys line up with the tabs of the set editor:

- `targets` - domains, IPs, GeoSite and GeoIP categories, source devices
- `tcp` - general TCP settings, desync, window, incoming, RST protection
- `fragmentation` - the fragmentation method and its parameters
- `faking` - SNI faking, SYN fakes, mutation
- `udp` - the QUIC filter, the port filter and the UDP action mode
- `dns` - the set's resolver, DoH URL and pins
- `routing` - routing mode, output interface, upstream proxy or blocking
- `escalate` - which set to escalate to when this one stops working

:::tip Import and export
To move a set between devices, use the **Import/Export** tab of the set editor. It shows the same JSON and takes a pasted one back.
:::

## Editing it by hand

`version` at the top is the configuration format version. b4 reads it to decide what needs migrating, so leave it alone when editing anything else.

:::warning
Editing by hand works, but the web interface validates values and applies migrations on upgrade. After a manual edit, restart b4 so the change is picked up.
:::

## Migrations

When the format changes between releases, b4 migrates the file on startup: new fields arrive with their defaults and renamed ones are carried over. Before it touches anything it writes a backup next to the file, named after the version it is migrating from, for example `b4.json.v51.bak`.

A file whose `version` is higher than the running binary understands is loaded as it is, with a warning: settings that binary does not know about are dropped the next time the configuration is saved.
