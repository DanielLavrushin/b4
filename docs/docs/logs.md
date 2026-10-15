---
sidebar_position: 13
title: Logs
description: The b4 log in real time, with filters, and trace recording for bug reports.
---

The **Logs** page shows the b4 log as it is written.

## Controls

| Control | What it does |
| --- | --- |
| **Filter logs...** | Shows only the lines that contain the text, ignoring case. |
| **ERROR**, **WARN**, **INFO**, **TRACE**, **DEBUG** | Show or hide the lines of each level. Each chip counts its lines. |
| **Streaming** / **Paused** | Pauses the stream and resumes it. |
| **Clear Logs** | Removes the lines shown in the browser. The log on the router is not affected. |

**P** or **Pause** pauses and resumes the stream, **Ctrl+X** or **Delete** clears it.

## Recording a trace

A trace saves the log of one session to a file, for a bug report.

1. Click **Start trace**.
2. Reproduce the problem.
3. Click **Stop & save**. The browser downloads the trace file.

**Download last trace** downloads the last trace again. The file starts with the b4 version, a system diagnostics snapshot and the enabled sets, followed by the log lines written during the session. A trace stops by itself after 15 minutes. Only the last trace is kept.

[Reporting a problem](./troubleshooting/reporting.md) lists what else a bug report needs.

## Log level

**Log Level** in the **Logging** card under **Settings > System > Service** decides how much detail the log carries, see [Logging](./settings/system.md#logging).
