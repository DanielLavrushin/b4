---
sidebar_position: 6
title: Payloads
description: The data fake packets carry - generating and uploading payloads, and using them in sets and Discovery.
---

The **Payloads** tab keeps the files that fake packets can carry instead of a built-in payload. A payload is generated from a domain name or uploaded as a file. Generating, uploading and deleting act on the router at once, without **Save Changes**.

![The Payloads tab](/img/payloads/20261004000203.png)

## What a payload is for {#why-payloads-exist}

Some strategies send a fake packet ahead of the real one. The DPI inspects the fake, and the set's **Fake Strategy** keeps the server from accepting it, see [Faking](../sets/tcp/faking.md). The data the fake carries is the payload. TLS payloads serve the fakes a set sends over TCP, QUIC payloads the fakes it sends over UDP.

## Payload types {#payload-types}

The **Fake Payload Type** field of a set, under **TCP > Faking > Fake SNI Packets**, picks what the fake carries:

| Type | Contents |
| --- | --- |
| **Random** | 1200 random bytes, new for every connection |
| **Preset: Google (classic)** | A recorded TLS ClientHello for `www.google.com`. A new set starts with this type |
| **Preset: DuckDuckGo** | A recorded TLS ClientHello for `staticcdn.duckduckgo.com` |
| **Preset: STUN** | A STUN Binding Request, with no TLS in it |
| **My own Payload File** | A payload from this tab, picked in the **Generated Payload** field |
| **All Zeros** | 1200 zero bytes |
| **Inverted Original** | The data of the real packet with every bit inverted |
| **Generated from Domain** | A ClientHello built like the ones on this tab, for the name in the set's **Domain** field |

While **My own Payload File** has no payload picked or its file cannot be read, and while **Generated from Domain** has no name, the fake carries **Preset: Google (classic)**. The payload type of a [Discovery](../discovery.md) result is the one to start from.

## Generate Payload {#generating-a-payload}

The card builds a TLS ClientHello for the name in **Domain**. Typing a name, such as `youtube.com`, and clicking **Generate** or pressing Enter adds the payload to the list.

The ClientHello is built on the router. No connection is made, and the name is not resolved or checked. An `http://` or `https://` prefix and a path are dropped, and the name is lowercased. Each name holds one payload per protocol: when the name already has a TLS payload, generated or uploaded, the tab keeps it and reports that it exists.

Every generated ClientHello has SNI as its first extension. The generator is built for a TSPU shortcut: when SNI is the first extension, the name is checked against an allow-list, and an allowed name takes a fast path. Whether the shortcut applies depends on the provider's equipment.

## Upload Custom Payload {#uploading-a-payload}

The card adds a payload from a `.bin` file.

| Field | What it does |
| --- | --- |
| **Name/Domain** | The name the payload is listed under in sets and in Discovery. Nothing ties it to the contents of the file. When the field is empty, choosing a file fills it from the file name: `tls_youtube_com.bin` gives `youtube.com` |
| **Protocol** | **TLS (for TCP fakes)** or **QUIC (for UDP fakes)**. Choosing a file sets it from the first bytes of the file |
| **Choose File...** | Opens the file picker |
| **Upload** | Sends the file. Available once a file is chosen and a name is set |

The file has to be smaller than 64 KB and not empty. b4 stores it as it is, without checking the contents. An upload under a name and protocol that already exist replaces that payload without asking.

A [shared set](../sets/sharing.md) carries its payload file only when the file parses. For TCP fakes that is a TLS ClientHello with a server name, for UDP fakes a QUIC Initial with a readable ClientHello, at most 16 KB either way. Payloads installed from a shared set appear on this tab.

## Using a payload in a set {#using-in-sets}

**TCP fakes.** On the set's **TCP** tab, **Faking** sub-tab, with **Enable Fake SNI** on, the **Fake Payload Type** **My own Payload File** shows the **Generated Payload** field. It lists every payload on this tab by name and size.

**UDP fakes.** On the set's **UDP** tab, with **Action Mode** set to **Fake & Fragment**, **Fake Packet Payload** sets the body of the fake packets:

| Option | Body |
| --- | --- |
| **(zero fill)** | Zeros. A new set uses this option |
| **(auto: QUIC Initial)** | A new QUIC Initial with random contents for every fake |
| **QUIC preset 1**, **QUIC preset 2** | Built-in QUIC Initials of 1200 and 1357 bytes |
| A payload from this tab | Listed as `[quic] example.org (1250 bytes)`, QUIC payloads first |

The body is always **Fake Packet Size** long: a longer payload is cut, and a shorter one is padded with zeros. A new set sends 64 bytes, so a full QUIC Initial goes out whole only once the size is raised to its length. [UDP](../sets/udp.md) describes the other fake settings.

## Using a payload in Discovery {#using-in-discovery}

The **Captured ClientHello as the fake payload** field in the **Options** of the [Discovery](../discovery.md) page offers the TLS payloads from this tab. Discovery tries the picked payloads before its built-in ones, and a strategy found with one of them uses **My own Payload File** with that payload. A run that finds nothing with the built-in payloads can be repeated with payloads generated for several names.

## Generated Payloads {#management}

The list shows every payload on the router, whether generated, uploaded or installed from a shared set, with its name, size, protocol and the time it was added. The buttons are icons, and the names below are their tooltips.

| Button | What it does |
| --- | --- |
| **View/Copy hex** | Opens **Payload Hex Data** with the payload in hex. **Copy & Close** copies it to the clipboard |
| **Download .bin** | Saves the payload as a file |
| **Delete** | Deletes the payload at once, without confirmation |
| **Refresh list** | Reloads the list from the router |
| **Clear all captures** | Deletes every payload after a confirmation |

:::warning Deleting a payload that a set uses
Deleting a payload does not change the sets that refer to it. At the next start or configuration save, b4 logs an error for the missing file, and those fakes fall back to **Preset: Google (classic)** over TCP and to zeros over UDP.
:::

## Where payloads are stored {#where-payloads-are-stored}

Payloads are files in the `captures/` directory next to the [configuration file](../advanced/config.md#where-it-lives), and `payloads.json` in that directory lists them. The tab shows what `payloads.json` lists, so a `.bin` file copied into the directory by hand does not appear. Uploading the file adds it.
