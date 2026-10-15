---
sidebar_position: 50
title: Reporting a problem
description: What to collect for a bug report, what in it can be private, and where to send it.
---

Problems with b4 are reported as [issues on GitHub](https://github.com/DanielLavrushin/b4/issues). The bug report form there asks for the b4 version, the platform, the System Info report and a log trace. A trace recorded while the problem happens shows what b4 did with the traffic.

## What to collect {#what-to-collect}

| Item | Where |
| --- | --- |
| The b4 version | At the bottom of the side menu |
| System Info | **System Info** under **Settings > System > Service**, then **Copy JSON** in the **System Diagnostics** dialog, see [System Info](../settings/system.md#system-info) |
| A log trace | The **Logs** page, see [Recording a trace](#recording-a-trace) |
| The set involved | **Copy JSON** on the set's **Import/Export** tab, see [Import/Export](../sets/index.md#import-and-export) |
| The configuration, when asked for | **Download safe copy** under **Settings > System > Backup**, see [Safe copy](../settings/system.md#safe-copy) |

The text of the report says what was expected and what happened instead, which set, device or app is involved, and how often the problem occurs: every time, only on some sites, or only after b4 has run for a while.

## Recording a trace {#recording-a-trace}

A trace holds only the lines of the current log level, so the level goes up first.

1. Under **Settings > System > Service**, set **Log Level** to **Debug** and click **Save Changes**.
2. On the **Logs** page, click **Start trace**.
3. Reproduce the problem, for example open the site that fails.
4. Click **Stop & save**. The browser downloads the trace file, and **Download last trace** downloads it again.
5. Set **Log Level** back to **Info**.

A trace stops by itself after 15 minutes. The file is described under [Recording a trace](../logs.md#recording-a-trace) on the Logs page.

## When the web interface does not open {#web-interface-does-not-open}

Over SSH, the installer prints the system, the installed version and the state of the kernel modules, see [Diagnostics](../install/update.md#diagnostics):

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --sysinfo
```

With file logging on, `errors.log` in the log directory, `/var/log/b4` by default, keeps b4's errors and start-up warnings. [Router platforms](./platforms.md) gives the log commands of OpenWrt and MikroTik.

## What can be private {#what-can-be-private}

Issues on GitHub are public. The items above hold details of the router and the network:

| Item | What it holds |
| --- | --- |
| System Info | The host name, the addresses and MAC addresses of the interfaces, the firewall rules, and the proxy address of each proxy set. It is not masked. |
| Log trace | The System Info report, the enabled sets with up to eight of their domains each, and the log lines. The lines name the sites the devices open, with the devices' IP and MAC addresses. A proxy set's proxy address is listed, its login is not. |
| Set JSON | The set's domains and addresses. For a proxy set, also the proxy's address, username and password. |
| Safe copy | The sets with their domains and the proxy address of each proxy set, the devices with their names and MAC addresses, the interfaces and the ports. Passwords, tokens, secrets and the host names of the router and of the owner's relays are masked. |
| Installer output | The host name. |

Each item is plain text, and a text editor shows what it holds before it is shared. A name or an address that has to stay private can be replaced with a placeholder, the same one wherever it appears.
