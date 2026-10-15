---
sidebar_position: 1
title: MTProto proxy
description: Running a Telegram proxy on b4 that Telegram connects to with a secret, inside the network or from outside.
---

The MTProto proxy is a Telegram proxy server on b4. Telegram connects to it with a secret, from the local network or from outside, such as a phone on mobile data. Every Telegram client supports it. To the network, a connection looks like HTTPS to the domain set as **Fake SNI Domain**.

## Requirements

- For devices outside the local network, the proxy port is reachable from the internet. **Expose to internet** opens it on the b4 host. A router or modem in front of the host also needs a port forward, and a WAN address behind carrier-grade NAT cannot be reached over IPv4 at all. See [Access from the internet](../settings/security.md#expose-to-internet).
- The clocks of the b4 host and of the devices agree within two minutes. NTP on both keeps them so.

## Setup

1. Open **Settings > Telegram**.
2. On the **MTProto Proxy** card, turn on **Enable MTProto Proxy**.
3. Set **Port**. The default is `3128`. `443` is a common choice, since the traffic already looks like TLS.
4. For devices outside the local network, turn on **Expose to internet**.
5. Keep the default **Fake SNI Domain**, or enter a domain that is reachable from the network and carries ordinary traffic.
6. On the **Secrets** card, click **Add secret**. The new entry comes with a generated secret.
7. Enter a name for the person or device in **Name / comment**.
8. Click **Save Changes**.

![The MTProto Proxy card](/img/telegram/20260826233001.png)

![The secrets list](/img/telegram/20260826233002.png)

A secret for each person lets access be withdrawn one person at a time: each entry has its own switch and its own link, and the log names the secret a connection used. Only fake-TLS secrets, hexadecimal and starting with `ee`, are accepted. A secret in another format, such as one copied from another proxy, is skipped, and the log shows `skipping invalid secret`.

Some changes, such as a new port, restart the proxy on save.

On a VPS outside the censored network, **Direct TCP** in [Telegram upstream](./upstream.md) is enough. On a router inside it, the default **Auto (WebSocket → TCP)** reaches Telegram without a VPS, and a [DC Relay](./dc-relay.md) is the next step when WebSocket is blocked as well.

## Adding the proxy in Telegram

1. On the **Secrets** card, click the share button of a secret (**Share connection link**).
2. At the top of the dialog, choose **Local network** for devices on the same network, or **Internet** for devices outside. **Internet** fills in the public address b4 finds, and a DDNS name can be typed instead.
3. Open the `tg://proxy` link on the device, or scan the QR code.

To add the proxy by hand, open **Settings > Data and Storage > Proxy > Add proxy > MTProto** in Telegram and enter the b4 address as **Server**, the port and the secret.

![Proxy details in Telegram](/img/telegram/20260322135130.png)

## Checking the result

- In **Internet** mode, the share dialog warns when the link is not expected to work from outside, and names the cause.
- A test from the same Wi-Fi says nothing about access from outside, because many routers do not loop a connection to their own public address back to the local network. A phone on mobile data connects from outside.
- The **Telegram** panel of the [dashboard](../dashboard.md#telegram) shows the networks, connections and traffic of each secret.
- The [Traffic](../connections.md) page shows the proxy's connections with the secret's name.

## When it does not work

[Telegram problems](./troubleshooting.md) covers each symptom: Telegram [sits at "Connecting"](./troubleshooting.md#telegram-sits-at-connecting), the [client is rejected](./troubleshooting.md#the-client-is-rejected), Telegram [turns the proxy off](./troubleshooting.md#telegram-says-the-proxy-is-misconfigured-and-turns-it-off), or [dialling fails](./troubleshooting.md#dialling-fails).

## What an unrecognised connection sees

A connection that fails the secret check, such as a probe by the network, is passed through to the Fake SNI domain on port 443, so the real site answers it rather than b4. Data that is not a TLS handshake at all is dropped.
