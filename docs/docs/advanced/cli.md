---
sidebar_position: 1
title: CLI parameters
---

b4 accepts command-line parameters, and they take precedence over the values in the configuration file.

A value given on the command line is not written back into the file. When b4 saves its configuration, a setting that came from a flag is stored with the value the file already had, so a flag stays a run-time override rather than a permanent change.

## General

| Flag | Description | Default |
| --- | --- | --- |
| `--config` | Path to the configuration file | Discovered, `/etc/b4/b4.json` on most systems |
| `--verbose` | Log level: `debug`, `trace`, `info`, `silent` | `info` |
| `-v`, `--version` | Print the version and exit | - |
| `--clear-tables` | Remove b4's iptables/nftables rules and exit | - |

## Queue and processing

| Flag | Description | Default |
| --- | --- | --- |
| `--queue-num` | Netfilter queue number | `537` |
| `--threads` | Number of worker threads | `4` |
| `--mark` | Packet mark used in the firewall rules | `32768` |
| `--ipv4` | Process IPv4 traffic (`queue.ipv4`) | `true` |
| `--ipv6` | Process IPv6 traffic (`queue.ipv6`) | `false` |

### About `--ipv6`

`--ipv6` is the command-line form of the **IPv6 support** switch in [Settings -> Core](../settings/core#protocols), stored as `queue.ipv6`. It says which address families **b4** works on. It does not enable or disable IPv6 on the router, and the network keeps using IPv6 either way.

With it off, which is the default:

- b4 binds the packet queue to IPv4 only, so it never sees an IPv6 packet.
- No IPv6 firewall rules are written for any set, in any mode. Routing, blocking and the QUIC refusal a proxy set installs are all IPv4-only.
- A site a set targets that also answers over IPv6 is reached over IPv6 by the client and bypasses the set entirely. b4 writes a warning to the log when the host has a working global IPv6 address and this is off.
- To narrow that gap, b4 strips IPv6 addresses out of DNS answers for domains a set matched, so clients resolving through the router stay on the IPv4 path. See [The IPv4 fallback](../dns#the-ipv4-fallback).

The queue's address families are bound when the service starts, so changing this takes full effect only after a restart.

## Firewall

| Flag | Description | Default |
| --- | --- | --- |
| `--skip-tables` | Do not create iptables/nftables rules at startup | `false` |
| `--tables-monitor-interval` | How often to check and restore the rules, in seconds. `0` disables it | `10` |

## Logging

| Flag | Description | Default |
| --- | --- | --- |
| `-i`, `--instaflush` | Flush log writes immediately | `true` |
| `--syslog` | Send log lines to syslog as well | `false` |
| `--log-dir` | Directory for b4's log files. Empty disables file logging | `/var/log/b4` |

## Web server

| Flag | Description | Default |
| --- | --- | --- |
| `--web-port` | Port for the web interface. `0` disables it | `7000` |

## Examples

Run with a custom configuration file and debug logging:

```bash
b4 --config /opt/etc/b4/b4.json --verbose debug
```

Clear the firewall rules:

```bash
b4 --clear-tables
```

Run without automatic firewall setup, managing the rules by hand:

```bash
b4 --skip-tables
```

Run with IPv6 processing on for this run only, leaving the configuration file alone:

```bash
b4 --ipv6
```
