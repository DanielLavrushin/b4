---
sidebar_position: 1
title: Command-line flags
description: The flags b4 accepts at startup and the configuration setting each one overrides.
---

b4 accepts command-line flags, and they take precedence over the values in the [configuration file](./config.md).

A flag overrides the setting for one run only. When b4 saves its configuration, a setting that came from a flag keeps the value the file already had.

## General

| Flag | Description | Default |
| --- | --- | --- |
| `--config` | Path to the configuration file | Discovered, `/etc/b4/b4.json` on most systems |
| `--verbose` | Log level: `debug`, `trace`, `info`, `silent` | `info` |
| `-v`, `--version` | Print the version and exit | - |
| `--clear-tables` | Remove b4's iptables/nftables rules and exit | - |

## Queue and processing

| Flag | Description | Setting | Default |
| --- | --- | --- | --- |
| `--queue-num` | The first netfilter queue number | `queue.start_num` | `537` |
| `--threads` | Number of worker threads | `queue.threads` | `4` |
| `--mark` | The queue mark, see [Packet marks](../guides/marks.md) | `queue.mark` | `32768` |
| `--ipv4` | Process IPv4 traffic | `queue.ipv4` | `true` |
| `--ipv6` | Process IPv6 traffic | `queue.ipv6` | `false` |

`--ipv6` is the **Enable IPv6 Support** switch under **Settings > Core > Packet Engine**. It decides whether b4 processes IPv6, not whether the router uses it. With it off, IPv6 connections pass the router without any set applying to them, see [The `queue` section](./config.md#the-queue-section).

## Firewall

| Flag | Description | Setting | Default |
| --- | --- | --- | --- |
| `--skip-tables` | Do not create iptables/nftables rules at startup | `system.tables.skip_setup` | `false` |
| `--tables-monitor-interval` | How often to check and restore the rules, in seconds. With NFQUEUE `0` turns the check off; in TUN mode the interval is at least 10, and `0` does not turn it off | `system.tables.monitor_interval` | `10` |

## Logging

| Flag | Description | Setting | Default |
| --- | --- | --- | --- |
| `-i`, `--instaflush` | Flush log writes immediately | `system.logging.instaflush` | `true` |
| `--syslog` | Send log lines to syslog as well | `system.logging.syslog` | `false` |
| `--log-dir` | Directory for b4's log files. Empty turns file logging off | `system.logging.directory` | `/var/log/b4` |

## Web server

| Flag | Description | Setting | Default |
| --- | --- | --- | --- |
| `--web-port` | Port of the web interface. `0` turns it off | `system.web_server.port` | `7000` |

## Environment variables {#environment-variables}

| Variable | Description | Setting |
| --- | --- | --- |
| `GOMEMLIMIT` | The memory limit of the Go runtime. While it is set, b4 does not apply [Memory Limit](../settings/system.md#service-settings) | `system.memory_limit` |

`GOMEMLIMIT` takes the Go runtime's format, not the field's: a byte count such as `268435456` or `256MiB`, or `off`. With a value such as `256m`, b4 does not start.

## Examples

A custom configuration file and debug logging:

```bash
b4 --config /opt/etc/b4/b4.json --verbose debug
```

Removing b4's firewall rules:

```bash
b4 --clear-tables
```

Running without automatic firewall setup, with the rules managed by hand:

```bash
b4 --skip-tables
```

IPv6 processing for this run only, with the configuration file left as it is:

```bash
b4 --ipv6
```
