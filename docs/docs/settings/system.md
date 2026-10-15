---
sidebar_position: 2
title: System
description: Restart, diagnostics, logging, the web server and backups.
---

The **System** tab holds the settings of the service itself, on the sub-tabs **Service**, **Web Server** and **Backup**. Some fields apply only after a restart, see [Saving and restarting](./index.md#saving).

## Service {#service}

The sub-tab has two cards, **Service** and **Logging**.

![The Service sub-tab with the Service and Logging cards](/img/system/20261003230500.png)

### Restart Service {#restart}

**Restart Service** restarts b4 after a confirmation. The web interface is unavailable for a few seconds and reloads once b4 answers again.

b4 restarts through its service manager, such as systemd or an init script, when it has one. Otherwise, in Docker for example, it restarts itself in place, and the container does not restart. The **Service Manager** row of [System Info](#system-info) shows which way applies.

### System Info {#system-info}

**System Info** opens the **System Diagnostics** dialog, a report on the router and on b4.

| Section | Contents |
| --- | --- |
| **System** | Host name, distribution, kernel, architecture, CPU and memory, and a **Container** row when b4 runs in one |
| **B4** | Version, build, **Service Manager**, process ID, memory use and uptime |
| **Process** | Memory, threads and open files of the b4 process |
| **Paths** | The b4 binary, the configuration file, the data directory, the error log and the geodata files |
| **Geodata** | The GeoSite and GeoIP databases and the ASN cache |
| **Engine** | NFQUEUE or TUN, the start error when the engine failed, and with TUN its device and capture |
| **Firewall** | The firewall backend, the routing sets installed, **Flow Offloading**, how often b4 restored its rules, and the rules of b4 |
| **Network** | The interfaces with their state and addresses |
| **Kernel Modules**, **Kernel Capabilities** | Whether the modules b4 uses are present, and the packages that provide a missing one |
| **Firewall Tools (at least one needed)**, **Required Tools**, **Optional Tools** | Which commands b4 found, with their versions |
| **Storage** | Free space and read-write state of the usual mount points |

When **Flow Offloading** shows that offloading bypasses b4, the router moves connections to a fast path before b4 has seen the packets it needs, and b4 does not see that traffic.

**Copy JSON** copies the whole report, for a [bug report](https://github.com/DanielLavrushin/b4/issues) for example. It is not masked: it holds the host name, the addresses and MAC addresses of the interfaces, the firewall rules and the proxy address of each proxy set. [What can be private](../troubleshooting/reporting.md#what-can-be-private) lists what each part of a bug report holds.

### Service settings {#service-settings}

| Field | What it does | Default |
| --- | --- | --- |
| **Language** | The language of the web interface, English or Русский. | English |
| **Time Zone** | The time zone of the timestamps in the log. **Auto (system default)** uses the zone of the system. | **Auto (system default)** |
| **Memory Limit** | A soft cap on the memory b4 uses: `auto` or empty for half of the system memory, a size such as `128MiB`, `256m` or `1g`, or `off`. Near the cap b4 frees memory more often, and going over it does not stop b4. | `auto` |
| **Update mirrors** | Comma-separated `https` addresses tried before the built-in mirrors when a download from GitHub fails, see [Downloading b4 when GitHub is blocked](../advanced/update-mirrors.md). | Empty |

A **Memory Limit** below 32 MiB is raised to 32 MiB. The same floor applies to `auto` on a router with less than 64 MiB of memory.

### Logging {#logging}

The **Logging** card sets how much b4 logs and where the log goes.

| Field | What it does | Default |
| --- | --- | --- |
| **Log Level** | How much b4 logs. **Error** logs errors and warnings, **Info** adds the main events, **Trace** adds the details of packet and DNS processing, and **Debug** adds debug output. | **Info** |
| **Log Directory** | The directory for `errors.log`, which keeps errors and start-up warnings, and `update.log`, which keeps the last update started from the web interface. Empty turns file logging off. | `/var/log/b4` |
| **Instant Flush** | Writes every log line at once instead of in batches. | On |
| **Syslog** | Sends the log to the system log as well, tagged `b4`. | Off |

**Info** is the usual level, and **Trace** or **Debug** the level for diagnosing a problem. At **Error**, the [Logs](../logs.md) page shows only errors and warnings. The [Traffic](../connections.md) page does not depend on the level, except for DNS queries, which it shows from **Info** on. The command-line flags `--log-dir`, `--instaflush` and `--syslog` override these fields, see [Command-line flags](../advanced/cli.md#logging).

## Web Server {#web-server}

The sub-tab configures the server of the web interface, which also carries the API and the [MCP server](./mcp.md). [Securing the web interface](./security.md) shows how to set up the login, HTTPS and access from the internet.

![The Web Server sub-tab](/img/system/20261003230600.png)

| Field | What it does | Default |
| --- | --- | --- |
| **Bind Address** | The address to listen on. `0.0.0.0` is every address, IPv4 and IPv6. `127.0.0.1` is the router only. | `0.0.0.0` |
| **Port** | The port of the web interface. A port that another program or another b4 listener uses is refused on save. | `7000` |
| **Expose to internet** | Adds a firewall rule that opens the port to any address. Needs a username and a password, see [Access from the internet](./security.md#expose-to-internet). | Off |
| **TLS Certificate** | The path to the certificate file, `.crt` or `.pem`. With both TLS fields empty, the interface is served over plain HTTP. | Empty |
| **TLS Key** | The path to the private key file, `.key` or `.pem`, without a passphrase. Set together with the certificate. | Empty |

### Authentication {#authentication}

| Field | What it does | Default |
| --- | --- | --- |
| **Username** | The login name. Empty turns the login off and deletes the stored password. | Empty |
| **Password** | The password, stored only as a hash. A blank field keeps the stored password. | Empty |

The login applies only while both fields are set. Without a TLS certificate, the login and the session cross the network unencrypted. The card warns in both cases.

## Backup {#backup}

The sub-tab saves, restores and resets the configuration.

![The Backup sub-tab with its four cards](/img/system/20261003230700.png)

### Download Backup {#download-backup}

**Download Backup** saves a `.tar.gz` archive of the configuration directory. Besides the configuration, it holds the history of [Discovery](../discovery.md#history) and of the [DPI Detector](../detector.md#history) and the [payloads](./payloads.md). The GeoSite and GeoIP databases, the vendor list of device filtering and the Community Hub author key stay out. The author key moves to another router with its recovery code, see [Integrations](./integrations.md#community-hub).

:::warning The archive holds secrets
The archive holds the configuration as it is, with the password hash, the tokens and the secrets, and the stored AI API keys. The [safe copy](#safe-copy) is the file for sharing.
:::

### Restore Backup {#restore-backup}

**Upload & Restore** takes a `.tar.gz` backup of up to 50 MiB and writes its files into the configuration directory. A file in the archive replaces the current one, and other files stay. b4 keeps no copy of the configuration it replaces, so a fresh backup first preserves it.

The **Restart B4 Service** dialog opens after the restore. The restored configuration takes effect with the restart. A save before the restart writes the old configuration over it.

### Download Configuration {#download-configuration}

**Download Configuration** saves the configuration b4 runs with as one JSON file, in the format of the [configuration file](../advanced/config.md).

| Button | Content |
| --- | --- |
| **Download safe copy** | The configuration with credentials, secrets and the host names of the router and of the owner's relays masked, for sharing when asking for help. Sets, with their domains and proxy addresses, stay as they are |
| **Download as is** | The full configuration, with the password hash and every token and secret |

#### Safe copy {#safe-copy}

The safe copy removes the web interface password and puts `[redacted]` in place of:

- usernames, passwords, tokens and secrets;
- the host names of the router and of the owner's relays;
- credentials and paths in URLs, and private DNS-over-HTTPS addresses;
- the update mirrors.

Sets with their targets, strategies and proxy addresses, devices, interfaces and ports stay as they are.

b4 refuses to save a configuration that still holds `[redacted]` in a masked field. A configuration file with it does not load. b4 then starts with default settings and keeps only the port and bind address of the web server, so the web interface opens without a login. The next save replaces the file.

### Reset Config to Defaults {#reset}

**Reset** returns the configuration to its defaults after a confirmation. It keeps:

- the sets;
- the web server settings, with the login, the language and the MCP server;
- the geodata settings.

Everything else returns to its default: the **Core** tab, the rest of **System**, the **Discovery** and **Telegram** tabs, IPinfo, the AI assistant, the Community Hub and the dashboard layout. Files beside the configuration, such as the histories, the payloads and the stored AI keys, stay. Fields that need a restart take effect after the next one.
