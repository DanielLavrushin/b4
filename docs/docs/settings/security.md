---
sidebar_position: 4
title: Security
---

## Authentication

By default the web interface is open without a password. To restrict access, set a username and password:

1. Go to **Settings -> Core -> Web server**
2. Fill in the **Username** and **Password** fields
3. Save the settings

After that, opening the web interface requires credentials.

:::warning External access
If b4 is exposed externally (for example, on a VPS), set up authentication. Without it, anyone who knows the address and port gets full management access.
:::

:::danger Authentication without HTTPS
If authentication is enabled but HTTPS is **not configured**, the username and password are transmitted over the network **in plain text**. Anyone who can intercept traffic (for example, on public Wi-Fi) can see your credentials. Always enable HTTPS together with authentication, especially if b4 is reachable outside the local network.
:::

## HTTPS

To enable HTTPS:

1. Prepare certificate and key files (`.crt`/`.pem` and `.key`/`.pem`)
2. In the web server settings enter the file paths:
   - **TLS Certificate** - path to the certificate file (`.crt` or `.pem`)
   - **TLS Key** - path to the key file (`.key` or `.pem`)
3. Save and restart

For a self-signed certificate (suitable for a local network):

```bash
openssl req -x509 -newkey rsa:2048 -keyout server.key -out server.crt -days 365 -nodes -subj "/CN=b4"
```

Copy the files to the configuration directory (for example, `/etc/b4/`) and point the settings at them.

After HTTPS is enabled, the web interface is available over `https://`.

:::warning An unusable pair falls back to HTTP
When the certificate and key do not load, b4 logs a warning and starts the web server over plain HTTP anyway. The service comes up either way, so the log line is the only sign.
:::

:::info The Telegram WEB proxy needs more than this
The [Telegram Desktop WEB proxy](../telegram/web-proxy.md) is served by this same web server, on a hostname of its own and ahead of the authentication above. Telegram Desktop requires a publicly trusted certificate on port 443 for that hostname, so a self-signed pair is not enough there, and requests to it never see the web interface's credentials.
:::
