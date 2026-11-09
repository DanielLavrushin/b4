# Changelog

All notable changes to b4 are recorded in this file.

## [Unreleased]

### Added

- **Real TCP window (wssize)**, based on [bolvan / Zapret2](https://github.com/bol-van/zapret2), rewrites outgoing windows through the first data packet or the original ClientHello.
- The log names the set and the device when a **TCP Timestamp** fake goes out without its timestamp, as from a Windows PC by default or toward a server that refuses timestamps. [#554](https://github.com/DanielLavrushin/b4/issues/554)
- With **Skip IPTables/NFTables Setup** on, the log warns when the router's connection tracking would drop **TCP Check** fakes. [#554](https://github.com/DanielLavrushin/b4/issues/554)
- When **Discovery** finishes while b4 is in the background, the tab title marks it, and over HTTPS **Notify me** on the **Discovery** page adds a browser notification with how many sites got a strategy. [#544](https://github.com/DanielLavrushin/b4/issues/544)

### Changed

- A set that an AI app creates through **MCP** `b4_find_bypass_strategy` is named after the site instead of an internal strategy name such as `cached-5-tcpmd5`. [#559](https://github.com/DanielLavrushin/b4/issues/559)

### Fixed

- With fake strategy **TCP Check**, or **TCP Timestamp** on a device without TCP timestamps such as a Windows PC, some sites on Cloudflare failed with an SSL error, because the server took the fake for the real start of the connection. [#554](https://github.com/DanielLavrushin/b4/issues/554)
- Fake strategy **MD5 Sum** broke the sites of its set, because the fake went out without the MD5 option. [#555](https://github.com/DanielLavrushin/b4/issues/555)
- A set with **Fake SNI Packets** on sent its fake again in front of later packets of a connection, and so did the **Decoy Packet** of **Combo**, so a server could close the connection right after it opened. [#556](https://github.com/DanielLavrushin/b4/issues/556)
- A fake meant to carry a wrong checksum could, in rare cases, carry a valid one and reach the server as data. [#557](https://github.com/DanielLavrushin/b4/issues/557)
- In the set editor, **Fake TTL** was greyed out while other features used it, **Corruption Strategy** stayed editable where it does nothing, and several faking help texts were wrong. [#558](https://github.com/DanielLavrushin/b4/issues/558)

## [1.90.0] - 2026-11-07

### Added

- **Discovery** finds the other blocked hosts a site's page needs for its styles, scripts and images: the new **Page hosts** step tests them, and **Apply as a set** adds the ticked ones.
- A set can keep the connections to a provider's own servers, such as its Google Global Cache nodes, even when a lower set matches their domain. The **IP priority** switch for this is under **Targets, Bypass IPs** in the set editor and is off by default.

### Changed

- A set routed into an AmneziaWG interface leaves its packets alone while the interface is up, as a set routed into WireGuard does.
- The **ASN** tab of the set editor is gone: ASN targets are under **Targets, Bypass IPs**.
- **Discovery** shows a run's results again when its page is reopened within 30 minutes, and marks a strategy found during a run as the fastest so far.
- Deleting a payload that a set uses on the **Payloads** tab asks first and names the sets that depend on it.
- The `diagnose_domain` prompt for AI apps first checks whether the site's traffic reaches b4.

### Fixed

- AI apps on the newest **MCP** protocol, such as Antigravity CLI, lost the connection before b4's tools appeared, and behind nginx they could wait a minute for them.
- An AI app could report one **Discovery** strategy and apply another, and b4's notes on settings did not turn up when searched in plain words.
- Many errors in the web interface showed only a bare status, such as 409 Conflict.
- In TUN mode, a device could keep waiting after b4 had cut its connection or answered its DNS query, because the router had moved the connection to another port.
- **IP Block Detection** and **Escalation** could cut off working sites: when a device reused a port, when a server asked the browser to repeat its first request, or right after **Escalation** moved a site to its backup set.
- **IP Block Detection** and **Escalation** missed some stuck connections: with RST or FIN fakes, or when the site name was in the second packet of the first request.
- A set using **Combo** with an **Overlap Length** could break connections soon after they opened, because the overlap was added to later packets too.
- On a busy network, connections could hang after b4 started, while b4 showed as running.
- A **Discovery** strategy could pass every test and then fail as a set, because captured ClientHello and **Random** fakes were tested with the built-in fake, or a site's QUIC capture went out in place of its TLS one.
- Applying a saved **Discovery** result whose payload had since been deleted gave the set the built-in fake without a warning.
- **Discovery** added DNS redirects that some sites did not need, and its result card and **History** could show another strategy's confirmation.
- A missing TCP fake payload also took away a set's UDP fake payload, so UDP fakes went out as zeros.
- In the set editor, Tab did not leave an empty entry field, and **Manual Bypass IPs** accepted text that is not an address, such as an AS number.
- A duplicated set was saved at the top of the list, ahead of its source, and took the connections to the sites both list.
- On a narrow screen, the history headers on the **Discovery** and **DPI Detector** pages could be unreadable.

### Security

- Without a login for the web interface, any website opened on a device in the network could watch b4's live log and the sites each device visits. A dashboard on another site, such as Home Assistant, needs a web interface login and its token, and a reverse proxy must pass the original `Host` header and `X-Forwarded-Proto`.

## [1.89.1] - 2026-10-31

### Added

- The **Captured ClientHello as the fake payload** list in **Discovery**'s options starts with **Select all payloads** and stays open, so several payloads can be picked in one pass. ([#475](https://github.com/DanielLavrushin/b4/pull/475), thanks [@asovetov](https://github.com/asovetov))

### Fixed

- Videos and sites still froze on servers the provider blocks for every site, because **IP Block Detection** missed blocks that drop the server's answer, and apps that try QUIC first kept waiting on banned addresses.
- **IP Block Detection** could ban a server that refuses unknown site names, because b4 tests a suspect address with unrelated names, and re-tests never lifted the ban.
- In TUN mode, QUIC and other UDP connections that a set rejects through **Block QUIC (force TCP)**, the UDP **Action Mode** or the **Block action** kept apps waiting instead of failing at once.

## [1.89.0] - 2026-10-30

### Added

- Sets with **IP Block Detection** and **Cache Blocked IPs** ban an address the provider blocks for every site on it, so connections to it fail at once instead of hanging, and the dashboard's **Banned addresses** panel lists the bans, an approach inspired by [Ladon](https://github.com/belotserkovtsev/Ladon). Bans are on by default, and **Remember blocked addresses** under **Settings, Core, Packet Engine** turns them off.
- AI apps connected over **MCP** can look up b4's REST API with the `b4_api_reference` tool, for tasks the other tools do not cover, and are told to ask the user for the web interface login when one is set.

### Changed

- The **MTProto Proxy**'s first answer to a Telegram app copies the cipher, key exchange and answer size of the **Fake SNI Domain**, and the log warns when that site has no TLS 1.3.

### Fixed

- In TUN mode, b4 could load the router's processor fully until no device could open sites, because it read the router's whole connection list for almost every new connection and, since 1.88.0, every DNS answer.
- On Keenetic, connections to port 443 through a proxy set or **Telegram over WebSocket** stalled once they opened, so Telegram, for example, stayed on Updating. ([#469](https://github.com/DanielLavrushin/b4/pull/469), thanks [@asovetov](https://github.com/asovetov))
- Sites that need a bypass could stop loading until b4 restarted when the firmware or another service removed the firewall rules that pass traffic to b4. ([#468](https://github.com/DanielLavrushin/b4/pull/468), thanks [@asovetov](https://github.com/asovetov))
- With **Telegram over WebSocket** on, Telegram connections the bridge cannot relay, such as a bot's API requests, skipped the proxy sets that covered them, and where Telegram is blocked they were reset after about 13 seconds.
- Photos and videos through the **MTProto Proxy** or **Telegram over WebSocket** sometimes started loading only after 1-3 seconds.
- On some networks, **Telegram over WebSocket** and the **MTProto Proxy** stopped loading photos, videos and files, because those networks began to drop Cloudflare connections that open with a large first message.

## [1.88.2] - 2026-10-29

### Fixed

- On Keenetic, devices handled by **Device Filtering** could not open any site, even one in no set, because b4 took their first packets before Keenetic's firewall had marked them. [#466](https://github.com/DanielLavrushin/b4/issues/466)
- With **Device Filtering** on, a firewall rule from the firmware or another service could make b4 rebuild all its firewall rules every 10 seconds, and settings saves waited for each rebuild.
- The log showed four failed routing commands whenever the **Telegram over WebSocket** set, or a proxy set with **Route UDP through upstream** on, was rebuilt or removed.

## [1.88.1] - 2026-10-26

### Fixed

- Some AI apps could not use b4's **MCP** server: LM Studio, also through LobeHub, refused every request, apps using OpenAI's API did too with **Allow active probes** on, and b4 refused Gemini CLI's tool calls.
- `b4_check_domain` called a domain covered when a set listed only one of its subdomains and did not check links or hosts with a port by their host, and a very long name kept the router's processor busy for minutes, in the web interface too.
- Other **MCP** tools gave AI apps wrong information: `b4_recent_connections` misnamed its columns, `b4_logs_tail` called every error-log line an error and could skip newer lines, and `b4_get_topic` got some modes wrong.
- Settings changed through **MCP** could differ from what the AI app was told: a set switched on matched nothing until a restart, a set with a port filter and no targets acted on every connection on its ports, and invalid values were saved, some able to stop b4.
- **Copy JSON** copied a set as dozens of formatted lines, too long for a chat message, in the set editor since 1.88.0 and in **Discovery**'s **Apply as a set** dialog.

### Security

- **MCP** permission checks had gaps: a queued write was saved after **Allow configuration changes** was turned off, and an AI app could open a SOCKS5 server without a password to the network or point b4's Telegram server-list downloads at any address, local ones included. The tool hints also called `b4_find_bypass_strategy` read-only, although it can rewrite sets.

## [1.88.0] - 2026-10-21

### Added

- A set can handle only calls, such as Discord voice calls, with **Handle only calls** in the **Calls** field on its UDP tab, narrowed by **Which calls** to the Discord app or browsers; such a set has no targets and takes each call on its UDP ports from any other set.
- The dashboard lists devices again in its **Devices** panel, with each device's name, address, connection count and most visited sites, and clicking a device opens its traffic on the **Traffic** page.
- Devices that left the network can be forgotten under **Settings, Core, Devices** with **Forget device** or **Forget offline**: b4 deletes their name, selection and MSS value and removes them from the sets' source devices.
- Settings and the set editor mark unsaved changes with a dot and list each change with its old and new value before they are lost, and in the set editor **Discard Changes** replaces **Cancel** and keeps the editor open. ([#434](https://github.com/DanielLavrushin/b4/pull/434), thanks [@alle-ox](https://github.com/alle-ox))
- A slider's value can be typed in by clicking the number next to the slider. ([#434](https://github.com/DanielLavrushin/b4/pull/434), thanks [@alle-ox](https://github.com/alle-ox))
- For a site that needs no bypass, **Discovery** lists the other strategies that also loaded it under **N other strategies worked**, and any of them can be applied as a set, for when a browser still cannot open the site. [#423](https://github.com/DanielLavrushin/b4/issues/423)

### Changed

- **Filter STUN Packets** on a set's UDP tab became the **Calls** field: on became **Pass call setup**, which passes STUN requests and the Discord app's voice packets unchanged, and off became **Handle like other UDP**.
- A set acts on QUIC to an address it learned only when the connection names a site the set targets, not on other sites that share the address, such as a CDN; **By SNI or matched destination** under **QUIC Matching** keeps the old behaviour. [#437](https://github.com/DanielLavrushin/b4/issues/437)
- The **Traffic** page and the dashboard show a device by its name, or by its IP address when it has none, where the **Traffic** page showed most devices by MAC address; the MAC address shows on hover.
- A set's JSON pasted on the **Import/Export** tab is not imported at once: it appears formatted in the box, which shows where broken JSON fails, **Import** applies it and **Format JSON** tidies hand-typed text. ([#434](https://github.com/DanielLavrushin/b4/pull/434), thanks [@alle-ox](https://github.com/alle-ox))
- A **Device Filtering** change takes effect when the settings are saved, where it used to wait for a restart of b4 and could meanwhile make b4 process the same connections twice.

### Fixed

- The **Traffic** page showed STUN and Discord voice packets that a set with **Filter STUN Packets** on let through unchanged as if the set had handled them.
- Some set editor help texts were wrong: the TCP **Port Filter** claimed port 443 is always included, the UDP **Port Filter** that an empty field turns it off, and the UDP **Evasion Technique** misdescribed **None** and **TTL**.
- The **Connection Packets Limit** sliders on a set's TCP and UDP tabs had no effect, because every set took the global limits under **Settings, Core, Packet Engine**.
- The import tool dropped a zapret port range with equal or reversed ends, such as `3478-3478`, so a set with only that range acted on every port, and it left shared domains unused when every profile had its own host list.
- `b4_test_domain_now` and the **Test** button of a **Community Hub** set reported sites that redirect to their `www.` address, such as youtube.com, as failing with and without b4. [#445](https://github.com/DanielLavrushin/b4/issues/445)
- **MCP** answers did not say which setting to change or that **Allow active probes** and **Allow configuration changes** are in b4's web interface, and an undo that needed **Allow active probes** stayed refused after it was turned on. [#446](https://github.com/DanielLavrushin/b4/issues/446)
- A block set limited to **Source Interfaces** also blocked sites by name for other interfaces and the router itself, and a block set with an escalation target stopped blocking a site once a device retried it a few times.
- **Block QUIC (force TCP)** had no effect on a proxy set with **Route UDP through upstream** on, so videos loaded slowly over a slow upstream, and with that switch off, other UDP such as WebRTC requests, which can reveal the real address, left directly. [#456](https://github.com/DanielLavrushin/b4/issues/456)
- A proxy set with **Route UDP through upstream** off let QUIC go around the upstream on ASUS routers with Asuswrt-Merlin, and on any router it refused QUIC also from the devices it excluded with **All devices except selected**.
- On ASUS routers with AiProtection on, and for the devices KeeneticOS lists for its hotspot, b4 missed the replies of many device connections, because the firmware's connection marks covered b4's **Packet Mark**. [#441](https://github.com/DanielLavrushin/b4/issues/441)
- With the UDP action **Coalesce QUIC Initial**, apps that open a QUIC connection in a single packet could not connect at all, even to sites the network does not block. [#427](https://github.com/DanielLavrushin/b4/issues/427)
- b4 could stop on a malformed QUIC packet sent to an address a set covers.
- A site that a set drops, rejects or blocks over QUIC still opened when another set had learned its address, such as a shared CDN address. [#437](https://github.com/DanielLavrushin/b4/issues/437)
- In TUN mode, memory use grew with every new connection.
- On a phone, the set editor's **Cancel** and **Save** buttons covered the set name, and long section headers, Russian ones first, ran past the edge of their card. [#448](https://github.com/DanielLavrushin/b4/issues/448), [#449](https://github.com/DanielLavrushin/b4/issues/449)
- In the Russian interface, slider units and a new set's name stayed in English, and in both languages the **SYN Fake TTL** slider showed its value in milliseconds, although it counts hops. [#450](https://github.com/DanielLavrushin/b4/issues/450)
- **Device Filtering** ignored devices added under **Manual Devices**, such as clients of the router's own VPN server: a selected one got no DPI bypass, an excluded one still did, and selecting only such devices bypassed every device. [#442](https://github.com/DanielLavrushin/b4/issues/442)
- In TUN mode, **Device Filtering** kept the selected devices out of **RST Injection Protection** and **Remove ECH keys from DNS answers**.
- An unnamed device vanished from the device lists when it went offline, yet stayed selected with its MSS value, and a set's list showed a device with a lowercase MAC address as not selected.
- **Discovery** and the watchdog's heals for a set with its own DSCP value tested without that value, so where a router in front of b4 picks the provider by DSCP, they searched through another provider, a heal could write the strategy found there into the set, and applying a result could move its sites to a set with another DSCP value or none.
- With iptables and legacy iptables tables both present, b4 did not see a removed DSCP chain as gone, so the firewall monitor retried its DSCP rules every few seconds.

## [1.87.0] - 2026-10-17

### Added

- A set can **Remove ECH keys from DNS answers**, under **DNS & Routing, DNS Redirect**, so browsers open sites such as Cloudflare ones where the network drops connections that use ECH, and **Discovery** proposes such a set when a site needs it. With `cloudflare-ech.com` listed, the set covers every Cloudflare site.
- The dashboard warns under **Recent changes** when a set's TCP port filter makes b4 skip connections, which can happen with **Skip IPTables/NFTables Setup** on or without the kernel's conntrack match.
- **System Info** shows whether the router can hold a dropped QUIC connection, in the row **QUIC drop holds for the whole connection**, and names what is missing, on OpenWrt the package `kmod-nf-conntrack-netlink`.
- A set routed through an interface can send its traffic to a gateway on that interface's network, such as a VPN gateway or another router, set in **Gateway (optional)** under **DNS & Routing, Traffic Routing**. ([#401](https://github.com/DanielLavrushin/b4/pull/401), thanks [@alle-ox](https://github.com/alle-ox))

### Changed

- On Keenetic and on routers with an older kernel where b4 cannot hold DNS queries, they reach the resolver the device asked, and b4 replaces its reply for names a set blocks, pins or sends to its own resolver, so that resolver still sees those names, even for a set with an encrypted resolver. **System Info** shows the row **DNS queries copied, not held (NFLOG)** where this applies.
- In NFQUEUE mode, a connection that another service sends through a WireGuard, AmneziaWG, TUN or TAP tunnel, or with legacy iptables reroutes as MagiTrickle does, gets no strategy from its set and is marked `tunnel->wg0` or `reroute->ppp1` on the **Logs** page. Block sets still block it, and a tunnel that carries the router's default route keeps strategies.

### Fixed

- On Keenetic, a device that asked for a name's IPv4 and IPv6 addresses at once, and the router itself, still often lost the second answer after 1.85.1, though **System Info** showed **DNS queries queued before conntrack (raw)** as available. [#402](https://github.com/DanielLavrushin/b4/issues/402)
- On routers with an older kernel, a device that asked at once for the IPv4 and IPv6 addresses of a name in a set with a DNS redirect occasionally got only one of the answers.
- On routers with iptables and an older kernel, such as Keenetic, **SYN Fake Packets** made connections open about a second slower, and the UDP mode **Fake & Fragment** lost most fakes, often with the real first QUIC packet.
- A device that sends several DNS lookups from one socket, such as a DNS server on the local network, had its later lookups skip b4 once b4 had answered or changed one of them, for example by removing IPv6 addresses.
- A set with **Domain-only matching** on applied its strategy only to the start of each connection, and the rest showed no set on the **Traffic** page. [#428](https://github.com/DanielLavrushin/b4/issues/428)
- A set that drops or rejects QUIC, or blocks a site, did not stop a browser such as Chrome from opening the site over QUIC, because Chrome sends the start of a QUIC connection in two packets. [#428](https://github.com/DanielLavrushin/b4/issues/428)
- With the UDP action **Coalesce QUIC Initial**, sites did not open over QUIC in Chrome, because the padding went in front of every handshake packet. [#427](https://github.com/DanielLavrushin/b4/issues/427)
- **Discovery** reported some sites, such as Cloudflare ones, as loading without b4 while Chrome could not open them, and could propose a remembered packet strategy for a site that only needed its DNS answer fixed.
- **System Info** listed tools such as jq, sha256sum and ipset without a version when they were on the USB drive b4 is installed on.
- A `regexp:` entry listed in two sets, such as a set and its copy made with **Duplicate**, worked only in the higher set, so with one of them limited by source devices, some devices got the wrong set or none.
- A device added by hand with an IPv6 address typed in another form, such as in capitals or with leading zeros, was not recognised, so a set that selects it under source devices skipped it and one that excludes it applied to it.
- Apps that use TCP Fast Open could not open sites through a set with a strategy. [#431](https://github.com/DanielLavrushin/b4/issues/431)
- **Window Mode** sent malformed fake packets on connections that use TCP timestamps, which Linux turns on by default.
- **SYN MD5 Signature** could send its fake SYN without a usable MD5 option, such as for an app that uses TCP Fast Open, so a server could take the fake for a real SYN.
- b4 could stop on a malformed TCP packet sent to an address a set covers.
- A set whose TCP port filter covered the ports devices pick for their own connections, such as `1-65535`, made b4 skip some or all TCP connections of every set. [#429](https://github.com/DanielLavrushin/b4/issues/429)
- Since 1.71.1, on the router's own connections and on IPv6 connections without address translation, **RST Injection Protection**, **Incoming Response Bypass** and escalation on **Forged resets** stopped working once b4 had sent fakes or split segments.
- Since 1.26.1, with **Device Filtering** off, the router's own connections showed up twice on the **Traffic** page, and **IP Block Detection** could mark their addresses as blocked after fewer failed attempts than its thresholds.
- Since 1.22.0, connections from the internet to a local server behind a port forward failed once a set matched them, such as a set that matches port 443 alone.
- Since 1.39.1, on nftables, **Global MSS Clamping** and the **MSS Clamping** of a set without source devices had no effect on devices or on the router's own connections.
- On routers whose firmware overwrites b4's connection mark, such as some ASUS models, a forwarded port gave no answer to connections from an address in a set routed through an interface.
- Connections that another service on the router had marked, such as those of devices KeeneticOS lists for its hotspot or those MagiTrickle routes, got no strategy from b4's sets, because b4 took those marks for its own. ([#422](https://github.com/DanielLavrushin/b4/pull/422), thanks [@asovetov](https://github.com/asovetov); [#276](https://github.com/DanielLavrushin/b4/discussions/276))

## [1.86.0] - 2026-10-09

### Added

- The **Discovery** page shows a run started through **MCP** or by the watchdog while the page is open, and its history marks such runs **via MCP** or **via watchdog**.
- **Copy JSON** in **Discovery**'s **Apply as a set** dialog copies the proposed set without creating it, ready to import on another device through the set editor's **Import/Export** tab.
- **System Info** shows the version of each tool it lists, such as iptables, nft, ipset, curl and wget, and its **Copy JSON** and the **MCP** tool `b4_diagnostics` include it as `version`.
- A set can write its own DSCP value into the packets sent to its addresses, in place of the **Set DSCP** value, so a router in front of b4 can route each set's traffic separately; the switch is under **DNS & Routing, Traffic Routing**.
- The browser tab title names the open page, such as `Traffic - B4`, and the open set in the set editor, so several b4 tabs can be told apart.
- The **Devices** panel on the **Traffic** page sorts by name by default, or by latest activity or packet count with its sort button, where devices used to swap places whenever one of them sent a packet.

### Changed

- A device on the dashboard's **Blocked** panel shows its IP address under its name, with the MAC address on hover.
- The dashboard arranges its panels in full-width rows, each panel as tall as the tallest in its row, instead of packing them into free spots, and in **Customize** panels can be resized and moved between rows.

### Fixed

- **Discovery** tried every strategy for minutes on a link that gives an error even without b4, such as the bare address of an image host, then reported the site as if it were blocked.
- **Discovery** started through **MCP** cut a link at a comma inside it and lowercased its path, so a link such as a YouTube image link failed on every attempt, and it never tried the **Community Hub** strategies.
- On Keenetic, b4 was sometimes not running after a router reboot, with no `errors.log`, and could stop when it restarted itself to retry its packet engine. [#404](https://github.com/DanielLavrushin/b4/issues/404)

## [1.85.1] - 2026-10-08

### Changed

- With iptables, other services' firewall rules for DNS queries, such as an Xray TPROXY rule or mwan3 and KVAS marks, also apply to the queries b4 has already seen.

### Fixed

- On routers with an older kernel, a device that asked for a name's IPv4 and IPv6 addresses at once often got no second answer, so apps such as a TV's video player never connected, and the router's own lookups could stall for seconds.

## [1.85.0] - 2026-10-04

### Added

- **Discovery** takes a **Trusted DNS server**, under **Settings, Discovery** or for one run, as `9.9.9.9`, `127.0.0.1:53053`, `tcp://`, `tcp+udp://` or an `https://` URL, checks DNS against its answers and gives it to a proposed set as its DNS where the set can use one.
- **Discovery** tests a site through pinned addresses, given for a run under **Pinned addresses** or taken from the set the run is for, keeps them in a proposed set, and reports a name that has no address in DNS as such instead of as misspelled.
- **Download Configuration** under **Settings, System, Backup** saves the running configuration as is, or as a safe copy to share when asking for help, with passwords, tokens, user names, host names and credentials in URLs replaced by `[redacted]`. b4 refuses to load or save a configuration that still holds these placeholders.
- **System Info** shows in its **Routing Sets** row how many routing sets are installed and, while installing them fails, the error and the next retry, and the log warns when no firewall tool can install routing at all.

### Changed

- The settings are regrouped: the **Core** tab has sub-tabs for **Packet Engine**, **Devices**, **Firewall**, **DNS** and **SOCKS5**, and a new **System** tab holds **Service**, **Web Server** and **Backup**, which used to be a tab of its own.
- The dashboard shows connections per minute in and outside sets, each set's matches in the last hour and the problems that need attention, **Runtime** moved to **System Info**, and **Reset Stats** became **Reset counters**, with a separate **Clear** on **Active Escalations**.
- **MCP** `b4_status` and `b4_metrics` and the metrics API count each connection once, with per-minute connection counts, `cpu_percent` and `rss_bytes` in place of `connections_seen`, `current_cps`, `current_pps` and `memory_percent`. `/api/metrics` and `/api/ws/metrics` carry the new dashboard snapshot, and `POST /api/escalations/clear` clears escalations.

### Removed

- **Discovery**'s **Fallback UDP DNS servers** list and the unused **Reference domain** setting are gone.
- The dashboard's **Device Activity** and **Domains Not In Any Set** panels are gone, in favour of links into the **Traffic** page.

### Fixed

- Dashboard figures misled: **Active flows** only grew, **Throughput** counted only the first packets of each connection, **Workers** always read all active, **Uptime** started over at **Reset Stats**, and **Domains Not In Any Set** stopped taking new domains.
- **Reset Stats** on the dashboard also sent every escalated site back to the set that had failed for it.
- **Discovery** for a domain that does not exist, such as a misspelled one, ran through every strategy for minutes, each test failing with `DNS resolution failed (no such host)`.
- **Discovery** could report a site as needing no bypass when its resolver gave a wrong address and no DNS server it tried answered honestly, and could propose a DNS redirect that gave a wrong address too.
- Applying a **Discovery** result could switch off the set's **Fail closed when the resolver is unreachable**.
- After a **Discovery** run, DNS queries over TCP for sets with a DNS redirect went to the original resolver from the next firewall rebuild until a restart, and during a run the **Active Escalations** list kept emptying.
- A set whose DNS redirect pointed at a resolver on another local device made its lookups wait out the timeout, or end in SERVFAIL with **Fail closed when the resolver is unreachable** on, whenever that resolver asked its upstream over plain DNS.
- Switching **Time Zone** back to **Auto (system default)** kept the previous zone in log timestamps until b4 restarted.
- A **NAT Masquerade** interface that was down or renamed disappeared from its card, which then looked as if masquerading covered every interface while it was limited to the missing one.
- Set cards gave target counts such as `1 domains`, and in Russian the longer labels in the panel a set card opens ran into their values or wrapped onto a second line.
- An update from the web interface failed with `Could not fetch the installer: mkdir /tmp/b4update-...: no such file or directory` when nothing could be created in /tmp, as in a MikroTik container after a restart, and with /tmp mounted noexec it seemed to start while nothing happened.
- On a kernel that rejects b4's nftables routing table, such as Synology DSM with b4 in Docker, b4 logged a routing error at every start and save and retried every 10 minutes, even when no set used routing.
- The Docker image lacked ipset, so on a host without working nftables routing sets failed and the NFQUEUE engine left out packet duplication and per-set MSS clamping by address, and the TUN engine gave each packet duplication address a capture rule of its own.

### Security

- A request to `/api/system/update` with a version such as `--remove` or `--arch=mips` removed b4 or installed a binary for another architecture, because b4 passed the version to the installer unchecked.

## [1.84.0] - 2026-09-30

### Added

- **Expose to internet** switches, off by default, open the ports of the web interface, the MTProto and SOCKS5 proxies and the Telegram Desktop WEB proxy in the firewall, and the MTProto share dialog can put the internet address in its link.
- **System Info**, the log and the **Telegram over WebSocket** card name the network bridges, such as `br-lan`, whose devices cannot use proxy sets or **Telegram over WebSocket** while `net.bridge.bridge-nf-call-iptables` is 1, as the dockerd package sets on OpenWrt. The **Bridge netfilter** row in **System Info** names the setting to turn off.
- **Set DSCP**, off by default, writes a DSCP value into the packets the host sends out, so a router in front of b4 can recognise b4's traffic; the switch is under **Settings, Core, Firewall Features**.

### Fixed

- The settings page asked for a restart of b4 after changes that need none: to the SOCKS5 proxy, to the web interface's username, password or language, or to the **MCP** settings.
- **Discovery**, including the watchdog's runs, lost some probe packets and ran several times slower on a site an enabled set already covered, on iptables and on nftables with some mark settings, such as a changed **Packet Mark**.
- A changed **Packet Mark** took effect only in part: routing sets kept the old value until the next firewall check or a restart, on iptables the old CONNMARK rule stayed even after b4 stopped, and a save from the web interface left **Discovery**'s marks at the old value plus 1 and plus 2.
- On iptables, b4 deleted masquerade rules it had not added at every start, stop and firewall refresh: a plain `-j MASQUERADE` in nat POSTROUTING and `-o <interface> -j MASQUERADE` for each interface listed under **NAT Masquerade**.
- One malformed address, such as `10.0.0.0/99` or a name, in a set with packet duplication kept the NFQUEUE packet engine from starting.
- In TUN mode, a set with packet duplication over many addresses, such as a GeoIP category or an ASN, delayed the web interface by a minute or more after start and, with a routing set, paused the bypass about as long again, while stopping b4 in that time timed out and left capture rules behind.
- In TUN mode on a kernel without the raw table or the CT target, such as Synology DSM, connections the device forwarded for others, such as Docker containers on a bridge network or LAN clients, failed past the local network.
- In TUN mode on a kernel without `xt_connbytes`, a set applied its DPI bypass to every packet of a connection instead of only its first ones, which slowed traffic through it many times over and wrote a log line per packet.
- The first connection to an address a routing set learned from a DNS answer could leave outside the set, directly instead of through a proxy set's upstream or the set's interface.
- Connections a proxy set sent to its upstream showed no site name on the **Traffic** page and in the log while **Send domain name to upstream** was off, or when the set listed only addresses and b4 had seen no DNS answer for the address.

## [1.83.1] - 2026-09-27

### Added

- When the packet engine fails to start, b4 keeps running so the web interface stays reachable, and the dashboard shows the reason with a button that switches to the other engine; b4 also retries three times within about two minutes.
- b4 can be restarted from the web interface without a service manager, for example in Docker, in a MikroTik container or after a manual start, and keeps its process ID, so the container keeps running.

### Fixed

- b4 could exit at startup without showing the reason on the console, some startup errors read `%!w(...)`, and when the packet queue failed to open, proxy sets kept sending traffic to a listener that was gone.
- On routers where b4 runs from an init script, such as ASUS Merlin and Keenetic, every restart or update from the web interface carried the old b4's packet-sending sockets into the new one, so open sockets piled up.
- With **Device Filtering** limited to selected devices, connections through b4's SOCKS5 server and others the router opened itself went out directly instead of through proxy sets and **Telegram over WebSocket**.
- On iptables, a routing set that **Device Filtering** limited to selected devices left without any, for example because its only selected devices were manually added IPv4 ones while IPv6 is on, kept its previous devices and made b4 rebuild routing at every firewall check.
- In TUN mode, a proxy set's site opened through b4's SOCKS5 server timed out unless its exact name was listed in the set, and a `.onion` address never worked through the SOCKS5 server in any mode.
- Where iptables is the nf_tables variant, as on Debian, Ubuntu and some OpenWrt builds, the router's own connections to a proxy set's addresses went out directly while b4 captured traffic for DPI bypass.
- A routing set with a resolver of its own under **DNS Redirect** could fill with the addresses the router's resolver gave for its domains, fake ones on a network with spoofed DNS, and turning that resolver on did not look the domains up again.
- On nftables, routing sets dropped each address learned from DNS when its time to live, an hour by default, ran out after it was first seen, however often DNS returned it, which cut live proxy connections and let traffic past block sets.
- On iptables, an address listed in a routing set by hand dropped out of it an hour after DNS also returned it.
- With `net.ipv4.conf.all.src_valid_mark` set to 1, as XrayUI does for a WireGuard outbound and wg-quick does, **Telegram over WebSocket** and proxy sets passed nothing from LAN and VPN devices, while the **MTProto Proxy** kept working.

## [1.83.0] - 2026-09-26

### Added

- A set can target whole networks by their ASN, such as `AS15169`, on the new **ASN** tab of the set editor or from the **Traffic** page and the **DPI Detector**, and b4 refreshes their address ranges daily.
- **Telegram over WebSocket** can be turned on with one switch under **Settings, Telegram**, without a set or GeoIP and GeoSite files, and its card shows whether the bridge works and what is missing.
- Two options under **Settings, Telegram**, both off by default, help where Telegram's WebSocket servers are blocked or workers.dev is slowed down: **Fronting name for Telegram's WS edge (advanced)** and **Let sets process Worker connections**.
- **Find a strategy** in a set card's menu runs **Discovery** on the addresses kept on the set editor's new **Discovery** tab, and with **Keep this set working with the watchdog** on, the **Watchdog** applies a new strategy when they keep failing.
- **System Info** shows how many of TUN's capture rules are in place and how often b4 restored its rules, and in TUN mode the firewall monitor interval appears under **Settings, Core, Firewall Features**.

### Changed

- The **MTProto Proxy** settings tab is renamed **Telegram**, and the **Traffic** page labels **Telegram over WebSocket** connections **Telegram bridge** instead of `-ws`.
- Replacing a set's strategy from **Discovery** works on any set and changes only what **Discovery** tested, keeping the set's UDP settings and routing; adding the site to the set, and out of every other set, is a checkbox, on by default.
- TUN picks its routing tables from the free ones below 100 instead of 9999 and 9998, which busybox `ip` refused, so TUN did not start there. `queue.tun.route_table` still sets a table by hand, and busybox `ip` older than 1.33 cannot run TUN.

### Fixed

- A set whose GeoIP or GeoSite file or category was missing acted on every connection to the ports in its port filter, and its MSS clamp on every HTTPS connection from its source devices.
- A cut-off download of `geosite.dat` or `geoip.dat` stopped b4 from starting until the file was fetched again by hand, and the installer could default to a source whose file did not fit on the router.
- Settings could be lost when a save was cut short, for example by a power loss, or when an address was added from the **Traffic** page during another save.
- Overlapping packet duplication addresses such as `8.8.0.0/16` and `8.8.8.0/24` stopped the bypass on nftables, and packet duplication and the MSS clamp picked up new addresses late.
- Saving settings took seconds with large routing sets on nftables.
- IPInfo and RIPE NCC lookups on the **Traffic** page could hang, and a failed add there showed a JSON parse error instead of the reason.
- Telegram through the MTProto proxy and the WebSocket bridge dropped connections and loaded media slowly where Telegram's own servers are blocked or unreliable, and on Android some channels' photos and videos did not load through the bridge.
- Telegram through the WebSocket bridge kept failing where workers.dev is slowed down, a working Cloudflare Worker was cut off mid-session, and Telegram's Cloudflare routes failed while a set covering Cloudflare was on.
- b4 contacted core.telegram.org (or proxy.b4core.app when that failed) and raw.githubusercontent.com on every start, even with every Telegram feature off.
- A heal by the **Watchdog** could damage the set it healed, and with **Cache Blocked IPs** on, the sites the old strategy had failed on stayed refused for up to five minutes after any strategy change.
- **Discovery** could pass a strategy that ended on the ISP's block page, probe the router's own network, and wait out the whole timeout on a connection that froze mid-page.
- The **Watchdog** and the **DPI Detector** reported blocking that was not there, and the **DPI Detector** said port 53 queries reached their resolvers when one redirect answered them all.
- Proxy sets and **Telegram over WebSocket** could stop carrying traffic, for example after XrayUI restarted xray, and a proxy set whose targets included the server of an upstream in the network cut off every device behind it.
- **Send domain name to upstream** changed nothing for most connections, so an upstream such as Tor got only an address and, with SafeSocks on, refused the connection.
- A set with an output interface sent its fake and split packets out the main uplink after some saves on iptables, and could delete every routing rule on the router and replace its default route when routing tables 100 to 249 were all taken.
- In TUN mode, a router firewall restart, such as after a port-forward change on Xiaomi, cut b4 off from traffic until b4 was restarted, some setting changes also needed a restart, and on iptables 1.8 device filtering briefly dropped LAN devices from capture.
- In TUN mode with **NAT Masquerade** on and no interface selected, or with a router masquerade rule that names no outgoing interface, b4 captured traffic but no site opened.

### Security

- The IPInfo token could end up in b4's log when an IPInfo request failed.

## [1.82.3] - 2026-09-23

### Added

- **Discovery** and the **DPI Detector** recognise a gateway in front of b4 that answers connections in the site's place: **Discovery** leaves its addresses out, and the **DPI Detector** reports the site as `GATEWAY`.
- **Discovery** keeps up to twelve other strategies that worked for a site in its **History**, not only the winner, and any of them can be applied from there later.

### Changed

- **Apply as a set** starts on replacing the strategy of the set a site is already in, keeping its name, sites and routing, instead of only creating another set.

### Fixed

- A site with **Pinned addresses** opened in a browser drifted after about a minute to an address b4 never handed out, and the tab ended in a timeout.
- A pinned address the router could not reach was handed out first every time, and a browser waited on it for over two minutes before trying the next one.
- A site outside every set that shares an address with a pinned name went out unprotected for two minutes after each lookup of that name.
- **Discovery** wasted a run on an `http://` check address whose port 80 answers everyone with a short error, and swept fake TTLs a second time after the first sweep had found none.

## [1.82.2] - 2026-09-19

### Changed

- A rule restore after a firewall rewrite announced by the Keenetic hook is logged at INFO level, and the remaining warning means the hook is missing on Keenetic, or elsewhere that something other than b4 removed the rules.
- Mirrors learned from the hub are tried after hub.b4core.app instead of before it, and **Settings, Integrations, Community Hub** lists the addresses in the order they are tried and marks the one that answered last.

### Fixed

- Saving a setting that rebuilds the firewall could leave the rules absent or half-built for up to twenty seconds on a slow router, with `Tables rules missing, restoring...` in the log.
- The hub never synced where a set covering it, such as a `regexp:.*` catch-all, stalled b4's own connection to it, and a failed sync was reported as `did not answer the health check` for one address only, whatever the reason.

## [1.82.1] - 2026-09-18

### Added

- b4 re-checks its firewall rules on `SIGUSR1`, and on Keenetic the installer hooks this into the router's firewall rebuilds, which used to leave b4 without its rules for up to ten seconds.

### Fixed

- A rule restore that ran while another program was rewriting the firewall reported `xt_connbytes kernel module is not available` and skipped IPv4, although the module was loaded.
- A custom list of five fallback UDP DNS servers under **Settings, Discovery** was lost after the next restart.
- On nftables, a duplication set or a set with MSS clamping that held more than a few thousand addresses stopped b4 with `argument list too long`.

## [1.82.0] - 2026-09-17

### Added

- Sets can be shared through the **Community Hub**, off by default under **Settings, Integrations**: published to hub.b4core.app after moderation, then browsed, applied and rated on the new **Community** page, and **Discovery** tries them before its own presets.
- The new **Coalesce QUIC Initial** UDP mode sends a padding-only QUIC packet ahead of the real handshake in the same datagram, so DPI that reads only the first packet stops on it.
- The new **Empty line before HTTP method** TCP option puts an empty line before a plain HTTP request, for sites filtered by their `Host` header; it covers port 80 only and needs a `User-Agent` header.
- The **Telegram Desktop WEB proxy** can have its own **Relay port**, for a forwarded WAN port 443 that otherwise showed the b4 login page, and its placeholder page can be replaced with an uploaded HTML file.

### Changed

- **Stop and confirm** ends a **Discovery** run early and still confirms the strategies found so far, and a second press during confirmation ends the run at once.
- **Match Fake Size** copies the real ClientHello into the fake with only the domain name replaced, instead of stretching the payload to its size, and **Discovery** tries this shape too.
- The **Compare Sets** dialog shows the same rows as the set cards, picks and swaps both sets inside it, names every setting that differs and has a **Differences only** switch. [#356](https://github.com/DanielLavrushin/b4/issues/356)

### Fixed

- **Discovery** could name a winning strategy for a site that stayed blocked, and kept searching with a fake it had already proved harmful.
- A passphrase-protected TLS key was accepted under **Settings** and the web interface silently stayed on plain HTTP, while a certificate file missing at start stopped the service altogether.

### Security

- Without a login for the web interface, any website opened on a device in the network could read and change b4's configuration.

## [1.81.1] - 2026-09-10

### Fixed

- After a service restart on OpenWrt, b4 could be left running and deaf to everything but `kill -9`, or run twice after an update. [#351](https://github.com/DanielLavrushin/b4/issues/351)

## [1.81.0] - 2026-09-06

### Added

- **Discovery** looks for a site's addresses that the block does not cover, among those served to some five hundred other regions, pins the site to them, and can fix a site by DNS alone with a set that changes no packets.

### Changed

- The **DPI Detector** page is rebuilt around the sites typed into it: each is fetched directly and through b4, so a row shows whether the ISP blocks it and whether b4 fixes it, and a button hands still-blocked sites to **Discovery**.
- The **Discovery** page is rebuilt around the sites typed into it, with one card and a plain verdict per site, the found strategy shown as on the **Sets** page, and the last run's log kept after a restart for download.

### Fixed

- **Discovery** could apply a set that did not match what it had found, and a stopped run left the page stale until it was reloaded.
- A proxy set that matched every address cut the device it was bound to off the network, and taking the address back out did not bring it back. [#322](https://github.com/DanielLavrushin/b4/issues/322)
- A set routed through an interface sent only the first packet of each connection that way, and every packet after it by the ordinary uplink.

## [1.80.4] - 2026-08-31

### Fixed

- Changing anything on a routing set took it down for about a second, and connections opened in that window left by the ordinary uplink for as long as they lived.
- Every setting changed through the **MCP** server kept a whole copy of the configuration, so memory climbed by tens of megabytes until b4 was restarted.
- A proxy set that resolves its upstream by domain went back to sending bare addresses a few minutes after any settings change, until b4 was restarted. [#338](https://github.com/DanielLavrushin/b4/issues/338)
- On Keenetic, the proxy and mtproto-ws routing modes were reported as unavailable after every reboot, although the firmware ships the modules they need. [#342](https://github.com/DanielLavrushin/b4/issues/342)

## [1.80.3] - 2026-08-30

### Fixed

- A routing table named in `/etc/iproute2/rt_tables`, as ASUS firmware names tables 100 and 200, was not recognised as a set's table, so b4 reported its own rule as missing on every pass and TUN left rules behind on shutdown.
- On iptables, a set in proxy or mtproto-ws mode diverted nothing on port 443, so Telegram sat at "Connecting" while the same set still worked on its other ports.
- On kernels up to 4.17, as on Keenetic, Padavan and older OpenWrt and Merlin builds, a set routed through an interface never got the rule that sends its traffic to its table, so everything it matched left by the ordinary uplink.
- A domain in a proxy-mode set failed through b4's built-in SOCKS5 server, while the same domain worked for a client passing through the router.
- A set in proxy mode with a hand-set `fwmark` took the router's whole network down.

## [1.80.2] - 2026-08-29

### Added

- A set routed through an interface has a **Kill switch**, so its traffic does not leave by the ordinary uplink with the router's real address when the interface goes away.
- The **MCP** server can act on b4, not only describe it: AI apps can search geosite and geoip, edit sets and their targets and, with **Allow active probes** on, test whether a domain loads and run **Discovery**.
- The **Update mirrors** field under **Settings, Control** takes https addresses that b4 and the installer try ahead of the built-in ones when GitHub cannot be reached, and the docs carry a Cloudflare Worker script to run one.
- The update window installs an update from a `b4-linux-<arch>.tar.gz` file fetched on another machine, with an optional SHA256 check, for a router that can reach no download source.

### Changed

- A set routed into a tunnel leaves its packets alone, without fakes, fragmentation, desync, SYN health checks, dead-IP escalation, **IP Block Detection** or TCP duplication; a set routed to a plain second uplink, or whose interface is down, keeps its strategy.

### Fixed

- After the b4 binary was replaced without a restart, the web interface reported the older version with nothing to say why.
- A set routed to a tunnel or an upstream proxy could take the router down within seconds of the service starting, because its traffic looped back into the proxy.
- A routed set could carry nothing at all with no sign of it in the interface, for example when no interface or upstream port was chosen, the firmware rebuilt its firewall, or a Broadcom router overwrote b4's routing mark.
- A routed set could send the start of a connection by the ordinary uplink and the rest by its route, take a routing table another program was using, or take over addresses another proxy set targeted.
- b4's own firewall work could take down the whole network: its commands raced the firmware where iptables has no lock, an overloaded b4 let the kernel drop packets, and on such routers pages stalled after their first fifty kilobytes once b4 stopped.
- Picking network interfaces to monitor could switch the bypass off for the whole network without a sign, once a VPN client or a transparent proxy moved the traffic to another interface.
- On the TUN engine, b4 captured a transparent proxy's own upstream connection instead of the network's traffic, sent LAN requests for a device on another bridge to the internet, and never saw DNS answers, so routing sets learned no addresses.
- Telegram kept reporting the MTProto proxy as incorrectly configured and switching it off, and on a router without IPv6 the bridge gave up on some of Telegram's servers.
- A **Discovery** result became unreachable once its run ended, stopping a run early threw away what it had found, the history file grew by about half a megabyte per domain, and the **Watchdog** judged domains on a check that skipped the bypass.
- A downloaded backup could be empty on drives prepared from Windows, because b4 skipped files marked as programs.
- Importing a zapret or byedpi configuration converted only a fraction of what was pasted, and misdescribed what it did convert.
- The MTProto settings page asked for a WEB proxy relay hostname while the MTProto proxy that serves it was off.
- Installing or updating b4 failed where the network blocks GitHub's download hosts, and the update window could come up with no versions listed.

### Security

- The settings folder, which holds the web interface login and the **MCP** token, was readable and writable by every account on the router, and restoring a backup could write files anywhere.

## [1.79.0] - 2026-08-18

### Added

- The MTProto proxy has a WEB carrier under **Settings, MTProto Proxy** that Telegram Desktop 7.1.1 reaches over ordinary HTTPS, for networks that block MTProxy and fake-TLS outright.
- The SOCKS5 proxy under **Settings, SOCKS5 Proxy** has **Allowed Sources**, a list of IP addresses and CIDR ranges allowed to connect, for clients such as Chrome that cannot send a username and password.
- A set's **UDP** tab has a **Block QUIC (force TCP)** switch, and a UDP action that leaves matched packets alone, for a set that matches QUIC only to learn a site's addresses.
- **Force IPv4 for matched domains** under **Settings, Core, DNS** drops the IPv6 records from DNS answers for names a set matches, so a dual-stack site cannot skip the set over IPv6.
- A set can have an **Egress IP** under **Routing** that replaces the source address of its traffic, for routers that pick their path by source address.
- The dashboard's **Runtime** card shows the operating system thread count, and b4 writes a goroutine dump to the log directory once it passes half the limit.

### Changed

- A set's DNS redirect falls back to the last good answer or the client's own resolver when its DoH server is unreachable, instead of answering SERVFAIL; **Fail closed when the resolver is unreachable** keeps the old behaviour.
- The two **QUIC Matching** values that both matched by server name are one value, **By SNI**, and stored sets are converted on the first start.
- The **API** settings tab is renamed **Integrations** and gives IPinfo, the AI assistant and **MCP** one card each, with its switch in the header, a masked credential field and, for **MCP**, a client configuration to copy.

### Fixed

- **Discovery** offered strategies for domains that were never blocked, and declared a strategy working on a single page fetch that may not have succeeded, such as a 502 error page longer than a kilobyte.
- On a router with working IPv6, **Discovery** stopped after about twenty tests and reported that no bypass was needed.
- **Discovery** built sets with a DNS redirect and geo categories they did not need, so a result for a CDN domain could not be added without a geo database.
- On systemd, restarting the service from the web interface left `Restart command failed: signal: terminated` in the error log, and a stop could hang for ten seconds and kill b4 during its firewall cleanup.
- An update could leave the service unable to load, or a second b4 running outside the service manager.
- A malformed IPv6 packet with extension headers could crash b4 and stop the service for every device on the router.
- A DNS answer that b4 rewrote never cleared the failure count for that name, so a site that had recovered moved to its backup set on the next single failure.
- A device added by hand, which has no MAC address, was ignored everywhere devices are matched, and an MSS clamp on it clamped every other forwarded connection instead.
- A routing set limited to source devices diverted traffic it was never meant to touch, such as the router's own outgoing traffic, and one matched only by port lost every DPI countermeasure.
- A routing set with no domain or IP target was built in full and quietly matched nothing.
- b4 took the router down after a few minutes on a busy line, leaving `runtime: program exceeds 10000-thread limit` in the log.

### Security

- The SOCKS5 proxy switched authentication off when only a username or only a password was set, its UDP relay could be pointed at another host on the network, and turning it off left open connections running.

## [1.78.0] - 2026-08-17

### Added

- An **MCP** server at `/api/mcp` lets an AI app read b4's status, configuration, sets, recent connections, log and diagnostics; it is off by default and uses its own token. A second switch, also off by default, lets the app change set, MTProto, SOCKS5 and log level settings, and the last change can be reverted. [MCP server](https://docs.b4core.app/docs/settings/mcp)

### Fixed

- Encrypted traffic on port 53, such as a VPN tunnel, was matched against sets as if it carried a domain name, and a site name read from QUIC traffic was accepted whatever bytes it held.
- Telegram could report the MTProto proxy as misconfigured and switch it off, because a session was accepted before b4 reached a data center and a blocked route was tried for longer than the client waits.
- Telegram sessions to data center 203 and others waited for a new connection, because spare connections were kept for only two data centers, and a route that went silent made every later session wait in full.
- A connection to a name with several addresses stalled on the first address that did not answer, while another address of the same name answered in under a second.
- The API reference on the documentation site rejected the b4 username and password in its Authorize window and accepted only a pasted login token.
- Turning the SOCKS5 proxy on or off, or moving it to another port, had no effect until b4 was restarted, although the save was reported as successful.

## [1.77.0] - 2026-08-15

### Added

- **Customize** on the dashboard reorders panels by dragging, widens or narrows them on a twelve-column grid and hides them, and the layout is kept in b4's configuration, the same in every browser.
- **Escalation** also moves a site to its backup set when its name lookup fails or returns no address, or when its address answers no connection attempt, and the backup set answers that lookup.
- A log trace begins with the enabled sets, with domain lists counted rather than listed and proxy credentials left out.

### Changed

- Unanswered TLS handshakes, forged resets and failed name lookups each have a threshold on the **Escalation** tab, in place of one control for forged resets and a threshold taken from **IP Block Detection**.
- Editing a set keeps the **Escalation** switch-overs that the edit does not affect, instead of dropping all of them.
- A firewall mark picked by hand for a set is refused and replaced with an assigned one when it holds every bit of b4's own packet mark, which made the set's traffic look like b4's own packets.
- A connection through a set's proxy is closed after an hour without data, or five minutes after one side has finished sending, and a proxy the set cannot reach shows up in the log and the diagnostics.

### Fixed

- Fake and split packets that b4 sends itself ignored the set's routing: on two WAN links the site reset the connection, and a set routed through a tunnel sent them, site name included, over the plain uplink.
- A set routed through a proxy leaked open files for each connection that died without closing, so within about an hour b4 hit the router's limit and stopped working until restarted.
- A set with an MSS limit for IPv4 addresses also capped every IPv6 HTTPS connection from the devices it names.
- A site moved to its backup set still used the failing set for its name lookups, the opening packet of each connection and connections that never reached TLS, so the backup set's DNS and SYN settings never applied.
- **Escalation** gave the backup set's routing only the one address a connection used, which expired while the switch was in force, and one device's failure moved every device to the backup set.
- Turning a backup set off erased the link to it, a set routed through a proxy could not escalate any further, and the chain-length warning was logged again for every packet.

## [1.76.3] - 2026-08-14

### Changed

- b4 resolves only a limited number of DNS queries at once and passes the rest to the device's usual resolver unchanged, as one slow DNS-over-HTTPS server could leave thousands of queries waiting.

### Fixed

- In TUN mode, devices got no reply to DNS queries that a set handled, blocked names, pinned addresses and curated answers included, because b4 read the device's address only after the router had replaced it.
- Every DNS query was listed under the router's address on the **Traffic** page, and a set limited to certain devices had nothing to match a query on.
- In TUN mode, a set routed through a proxy or an interface could add another packet's addresses to the firewall.

## [1.76.2] - 2026-08-12

### Changed

- Your own Cloudflare Worker is the last WebSocket route tried instead of the first, because Cloudflare can take a Worker back partway through a session, so it passed every check while video failed.
- b4 asks a Cloudflare Worker for a data center address without a port, because media from data center 203 failed on port 5222. The Worker script in the [Telegram documentation](https://docs.b4core.app/docs/mtproto) needs a compatibility date of `2026-04-07` or later.

### Fixed

- Telegram photos, videos and stickers stopped loading in 1.76.0 and 1.76.1, because media sessions were sent to a data center that could not serve them.
- b4 knew too few of Telegram's address blocks, so nearly half the connections matched no data center and had almost no routes to try, and two addresses went to the wrong data center.
- A route that took a request and returned nothing kept getting new connections, while sessions closed by the client counted against a route and pushed a working Cloudflare Worker below all others.

## [1.76.1] - 2026-08-11

### Fixed

- In the WebSocket bridge routing mode, Telegram could stay on loading with an empty chat list, because the bridge used only Cloudflare Workers and never Telegram's own route.
- The WebSocket bridge kept giving new Telegram connections a Cloudflare Worker that had stopped passing traffic, and sent traffic to a different data center address than the device asked for.
- Connections that b4 opens itself, such as a fail-open connection to a destination already in trouble, went out without any of b4's bypass applied.
- b4 put its routing rules back only when all of them were gone, and treated a partly missing rule set as healthy.

## [1.76.0] - 2026-08-10

### Added

- A set can pin addresses for its domains under **Pinned addresses**, written in hosts file order, for a CDN that keeps answering with a blocked address; a pin for a name the set does not match offers to add it.

### Changed

- b4 remembers every address seen in a DNS answer, not only those it connected to; before, a name with a single address, such as Meta's CDN, had nothing to fall back to once that address was blocked.

### Fixed

- Every DNS lookup took five seconds once a set was routed through a proxy or an interface, because b4 stopped reading packets while it wrote each answer's addresses to the firewall.
- System diagnostics reported jq and other tools as missing when they were installed on a USB drive.
- A curated DNS answer could hand a device an address that b4 had since marked unreachable, so the connection was reset on its first packet.
- In the WebSocket bridge routing mode, Telegram messages arrived but photos, videos and stickers did not, because every media session was announced to Telegram as an ordinary one.
- The **Traffic** page listed a TCP connection under a set whose port filter excludes it, and showed nothing about which resolver answered a DNS request or whether it was pinned, curated or dropped.
- On some systems, b4's firewall rules were torn down and rebuilt every few seconds, because the chain for the router's own DNS over TCP failed to load, and each rebuild left a moment with nothing intercepted.

## [1.75.0] - 2026-08-08

### Added

- **Import** on the **Sets** page turns a [byedpi](https://github.com/dovecoteescapee/byedpiandroid) or [zapret](https://github.com/bol-van/zapret) command line into one set per profile and reports each option as carried over, approximated or without an equivalent.
- **Match any domain** and **Match any IP address** make a catch-all set with IPv6 included, typing `*`, `any` or `all` into a target field does the same, and `*.example.com` is saved as `example.com`.
- A warning appears when several enabled sets name the same domain, since only the one placed higher in the list handles it.
- A **DNS** section in **Settings** sets the port for DNS over TCP, how long to wait for an answer and an off switch, and warns about ports other services already use.
- A log trace records whether each DNS request reached the set's DNS server or slipped past it.
- An **IP Health** section in **Settings** controls how long an address stays marked unreachable, with one verdict shared by all sets.
- A switch removes unreachable addresses from DNS answers, so a device does not hang on the one blocked address of a CDN while another device loads the site.
- A ready spare connection to your Cloudflare Worker for each data center saves the 65-90 ms setup that every new Telegram connection paid.

### Changed

- **IP Block Detection** also catches an address that never answers a connection request, and the router tests the address itself before treating it as blocked.
- Several Cloudflare Worker addresses are tried in random order instead of as written, which sent all traffic and all rate limiting to the first.

### Fixed

- A set applied from **Discovery** had no effect until b4 restarted, and two applied results could claim the same domains, so a domain got another result's strategy.
- When two sets listed the same IP range, the one stored last took it rather than the one higher in the list, and a set limited to source devices could lose it to a general set below.
- On routers with iptables, `0.0.0.0/0` in a set's IP targets never reached the firewall and took the set's MSS and duplication entries with it, and a set routed through a TCP-only proxy left no rules at all.
- DNS queries that a device sent over TCP skipped the set's DNS server and went to the router's usual resolver.
- One refusal from Telegram's WebSocket servers switched off a working Cloudflare Worker, and a paused data center address was refused at once, so Telegram reconnected in a loop and flooded the log.
- System diagnostics reported jq, sha256sum and nohup as missing while they were installed, and flagged flow offloading even when it was set up to leave b4 alone.
- An address marked as blocked stayed blocked for as long as a device kept retrying it and was never tested again, so a false alarm or a lifted block held on.

## [1.74.2] - 2026-08-02

### Added

- A **Remove** button deletes a downloaded `geosite.dat` or `geoip.dat` and clears its path and source URL, and downloading into another directory deletes the copy b4 wrote at the old one.
- An IPv4/IPv6 switch in the **DC Relay** socat helper produces `TCP6:` commands for a VPS that reaches Telegram's data centers over IPv6, and an IPv6 DC Relay address switches the listening side too.

### Changed

- A set routed through a TCP-only upstream proxy refuses QUIC on port 443 so that the browser falls back to TCP, instead of letting it go out directly past the proxy. Turn on **Route UDP through upstream** if the proxy supports UDP ASSOCIATE.

### Fixed

- b4's logs could fill a router's memory or storage: init scripts from older installers kept the full log in a RAM file on OpenWrt, and the error and update logs and trace files kept growing.
- A restart could leave a second copy of b4 running alongside the first.
- Turning **Route UDP through upstream** off had no effect until a restart, and matched UDP kept going to a port with nothing behind it, so HTTP/3 sites waited out a timeout instead of falling back to TCP.
- An upstream proxy that cannot relay UDP was reported only at trace level, so a set with **Route UDP through upstream** on looked like a broken site rather than a setting to change.
- Connections meant for a routing set could leave directly: the first one after each redirected DNS answer, and with an upstream SOCKS5 proxy, one for each address of a subdomain the set matches by suffix.
- An update could run out of space on devices with little storage, and a failed update left a second copy of b4 behind.

## [1.74.1] - 2026-07-31

### Added

- New set options shape the fake ClientHello: `fake_len_mode: "match"` sizes it to the real one, `apply_ttl` gives it the set's TTL with any faking strategy, and `md5_on_fake` adds the TCP MD5 option to it.
- With the `tls` fragmentation strategy, `middle_sni` places the split inside the domain name instead of at a fixed offset.
- **Discovery** also tries fake-only strategies, a low-TTL fake without any fragmentation, which reach filters that see through every split.
- The STUN fake payload is built in as **Preset: STUN** under **Fake Payload Type**, in place of a capture file uploaded to every device.
- Importing a shared set names the payload files it uses that are missing on the device, with a link to **Payloads**.

### Changed

- A fake packet and the real one carry different IP identification numbers, since a shared number gave away that both came from the same source.

### Fixed

- The **Watchdog** showed a newly added domain as healthy before any check, and indefinitely while the watchdog was off.
- A **Watchdog** healing run could declare a site healed after one lucky fetch, and overwrite a hand-tuned set's TCP, UDP, fragmentation and faking settings while logging only the strategy name.
- **Discovery** counted a fetch that returned no data as a working strategy.
- On an OpenWrt router without the packet-queue kernel module, b4 failed to start with a raw firewall error that did not point to the module, and the installer reported such a router as ready. [#275](https://github.com/DanielLavrushin/b4/issues/275)
- A kernel without connection packet counters stopped b4 from starting, and a failed start left part of b4's firewall rules behind.
- The diagnostics report left out the queue number, worker count and queue mode, and **System Info** listed the wrong kernel modules on nftables-only routers.
- Telegram on Android could stay at "Connecting" when a set routed it through the built-in Telegram bridge, because the bridge closed connections that were silent for five seconds. [#277](https://github.com/DanielLavrushin/b4/issues/277)
- The **Already exists in other sets** warning missed overlaps by domain suffix, such as `example.com` in another set for `www.example.com`. [#273](https://github.com/DanielLavrushin/b4/issues/273)
- The **DC Relay** socat helper generated commands for Telegram servers that drop the connection right after it opens.
- A request after an idle spell could hang for twenty seconds and YouTube on Android could take minutes to appear, because b4 missed a site name split across two packets and the connection got no strategy. [#279](https://github.com/DanielLavrushin/b4/issues/279), [#280](https://github.com/DanielLavrushin/b4/issues/280)
- The first connection to an address b4 had just resolved for a device could miss the set that owns the domain, and one device's traffic on a shared CDN address could decide the set for every other device. [#279](https://github.com/DanielLavrushin/b4/issues/279)
- A set built from a large geosite category took several times more memory than its domains, about 90 MB to read a few categories from a 51 MB file.

## [1.73.0] - 2026-07-05

### Added

- The **MTProto Proxy** settings have a TCP user timeout and an idle timeout, for carrier networks or router kernels where the defaults of about two and five minutes need adjusting or turning off.
- Two API endpoints, `GET /api/mtproto/sessions` and `GET /api/mtproto/active-clients`, list live MTProto client connections and group them by secret with each secret's client addresses.

### Fixed

- An MTProto proxy session could stay stuck for many minutes after a phone lost signal or put Telegram in the background, or when its link to Telegram over the WebSocket bridge stalled.

## [1.72.0] - 2026-07-02

### Added

- The MTProto proxy holds several named secrets, each switched on or off and shared as its own link or QR code, and the **Traffic** page and a dashboard panel show which secret each connection uses.
- **All devices except selected** in a set's **Source Devices** applies the set to every device except the ticked ones.
- **Domain-only matching** in a set's targets picks the set only from the domain it sees and does not learn or route the addresses behind it, for shared CDN addresses such as YouTube's and Google's.

### Changed

- A set's **Source Devices** table shows each device's MSS limit from **Device Filtering**.

### Fixed

- Memory use jumped after saving settings, even for changes unrelated to the blocked-site lists.
- On some routers, such as b4 in a container on MikroTik, a set routed through an interface, a proxy or the Telegram bridge gradually stopped working, because the router rejected newly found addresses. [#267](https://github.com/DanielLavrushin/b4/issues/267)
- With **NAT Masquerade** on, the log repeated `Tables rules missing, restoring...` every few seconds, and b4 re-applied all its rules although they were in place.

## [1.71.1] - 2026-06-29

### Added

- **System Info** has a **Kernel Capabilities** section that shows whether the router supports the features advanced modes rely on.

### Fixed

- The Telegram bridge could be slow to connect or cut transfers short, because b4 interfered with its own connection to Telegram.
- A named device that was off the network, for example connected over VPN, showed as its hardware address on the dashboard and the **Traffic** page.

## [1.71.0] - 2026-06-28

### Added

- Lines on the **Logs** page are coloured by level, and each level can be shown or hidden with one click.
- **Start trace** on the **Logs** page records everything until **Stop & save** and downloads one file with system details to attach when asking for help; recording stops by itself after 15 minutes.
- **NAT Masquerade** can apply to any combination of network interfaces, not only to one or to all of them.
- A runtime panel on the dashboard charts b4's memory use over time.

### Changed

- The live signal panel on the dashboard shows throughput, total data, the targeted share and active connections in place of connections and packets per second.

### Fixed

- In TUN mode, **Device Filtering** had no effect, and every device's traffic was processed whatever the selection.
- Memory use could creep up the longer b4 ran, because logging, AI chat streaming and **Discovery** did not always clean up after themselves.
- After an unclean stop in TUN mode, new sites could stop loading while open ones kept working, because a routing rule for the removed TUN interface stayed behind.

## [1.70.1] - 2026-06-23

### Added

- System diagnostics show whether b4 runs in NFQUEUE or TUN mode and list the firewall rules b4 maintains.

### Changed

- b4's own firewall marks moved to high bits, out of the range that Tailscale and some other router apps use, which could misroute traffic when they ran alongside b4.

### Fixed

- On gateway or container setups with **NAT Masquerade**, the bypass stopped working for mobile apps such as YouTube on Android, because b4's fake and split packets did not get the gateway's public address.
- b4 refused to start when a set mixed a country or service list with addresses added by hand that the list already covered, such as Instagram addresses next to the Facebook list.
- In TUN mode, every device appeared under the router's public address, so connection logs, per-device statistics and per-device set rules could not tell devices apart.

## [1.70.0] - 2026-06-21

### Added

- A TUN engine, chosen in the settings, runs b4 on devices whose kernel lacks the NFQUEUE modules, sending target traffic through a virtual interface whose uplink follows the default route when left on Auto. **Discovery** still needs NFQUEUE and does not run in this mode.
- Columns in the **Aggregated** view of the **Traffic** page sort by a click on their header.

### Changed

- The dashboard is rebuilt in one consistent style and opens with a live signal panel that combines connection rate with the key counters.

### Fixed

- On gateway and container setups with **NAT Masquerade**, such as b4 in a container on MikroTik, a set's DNS-over-HTTPS redirect could time out or return nothing, so pages did not load.
- On some setups, such as b4 in a container on MikroTik, a routing set filled in its addresses only on restart or when toggled, and stopped refreshing them once they expired.
- A set using the Telegram bridge kept failing after an internet outage until b4 was restarted by hand.

### Security

- The web interface password was stored as plain text and shown on the settings page. The field stays empty: leave it blank to keep the password, or type a new one to change it.

## [1.69.1] - 2026-06-14

### Fixed

- The **Sets** page seemed frozen after enabling, reordering, duplicating or deleting a set, because the screen changed only after the save and a full reload a second or two later.
- The MTProto proxy in 1.69.0 stopped connecting to Telegram on networks that block Telegram's data center addresses, so Telegram did not load through it.

## [1.69.0] - 2026-06-14

### Added

- System diagnostics, in **System Info** and in the installer, show whether flow offloading is on: this router speed feature, common on OpenWrt, sends traffic past b4 so that nothing is bypassed.
- **Max Connections** in the **MTProto Proxy** settings caps how many clients the proxy serves at once, and the default limit is 2048 instead of 512.

### Fixed

- A set routed through a proxy or the Telegram bridge could be handled by another router app such as [XrayUI](https://daniellavrushin.github.io/asuswrt-merlin-xrayui/en/install) instead, so it worked only while that app was installed.
- **Discovery** split a web address containing a comma into broken pieces, lost its log on a page refresh or after a search ended, and offered only the best result per group once a search finished.

## [1.68.0] - 2026-06-10

### Changed

- A set saved by **Discovery** sends its DNS lookups over DNS-over-HTTPS by default.

### Fixed

- On networks where the provider tampers with DNS, **Discovery** could not look up its test sites and reported that nothing worked, even when a working setup existed.
- Pressing **Cancel** while **Discovery** was testing several sites at once could crash b4.
- Stopping b4 could leave its firewall rules in place, and stopping it or running `--clear-iptables` left changed network settings, stray routes from sets sharing an interface and piled-up proxy or bridge rules.
- When a set's outgoing interface, such as a VPN or a modem, got a new address, the set's traffic kept using the old one until b4 was restarted.
- Changing the SOCKS5 proxy or a set (adding domains or IPs, creating, editing, reordering, deleting or toggling several) could save a configuration that did not match what was running.

## [1.67.2] - 2026-06-10

### Added

- Every update from the web interface writes a step-by-step trace to `update.log` in the log folder, reset at each attempt.
- The web interface shows the changelog in the selected language.

### Changed

- The log setting under **Settings, Service** is a folder (default `/var/log/b4`) instead of the path to `errors.log`, and existing configs are converted. An empty field turns file logging off.

### Fixed

- Each web interface update left a finished helper process behind, and where no system service runs b4, such as directly in a container, the update reported success but the old version kept running.

## [1.67.1] - 2026-06-10

### Fixed

- The **Traffic** page grew sluggish as more addresses had their AS label looked up.
- When a set's DNS-over-HTTPS server was briefly unreachable, lookups hung until they timed out, so pages failed slowly.
- Pressing **Cancel** did not stop **Discovery**, so starting another search ran two at once with mixed-up results.

## [1.67.0] - 2026-06-08

### Added

- A set's **DNS** tab can send lookups over DNS-over-HTTPS to a built-in or custom server, for services that need a specific resolver such as `xbox-dns.ru` and for providers that tamper with DNS.
- A set can be limited to IPv4 or IPv6 traffic, so a site can have its own set over each, and **Discovery** can search over one and limit its result to it. The choice appears when both IPv4 and IPv6 are on in **Settings**.

### Changed

- **RST Injection Protection** also drops the fake reset aimed at the server, not only the one aimed at the device.

### Fixed

- A set that routed its traffic to an upstream proxy or the Telegram bridge failed to open connections while its DPI bypass options were on.
- **RST Injection Protection** did nothing on a set that also used some SYN-based TCP techniques, because those connections were not tracked.
- Dummy network interfaces, such as `dummy0`, were missing from the interface lists in **Settings**, so they could not be picked for monitoring or NAT masquerade.
- Telegram Desktop reconnected every 30-60 seconds over the Telegram bridge, because a keep-alive added in 1.66.0 sent a signal that Telegram's servers rejected.
- **Watchdog** could repair a monitored site by adding it to a blocking or routing set that listed one of its subdomains, so the site got blocked. A site an earlier version added to such a set has to be removed from it by hand.

## [1.66.0] - 2026-06-07

### Added

- When a **Block** set blocks something, the **Dashboard** shows a **Blackhole** panel with blocked attempts, top domains and devices, and the **Traffic** page marks those connections `block`.
- **DC list fallback mirror** under **Settings, MTProto Proxy** turns off or replaces the b4 author's backup copy of Telegram's data center list on `lavrush.in`, used only when Telegram's own address is blocked.
- A set can have its own device list, so its routing, Telegram bridge or block applies only to those devices.

### Fixed

- A set's **Routing** ignored the devices chosen under **Settings, Devices**, so its interface, proxy, Telegram bridge or block applied to every device.
- The router itself showed up on the **Traffic** page as a separate device, sometimes named after a phone brand, holding the router's own connections.
- On a set with UDP on **Drop** or **Reject**, Discord voice and video calls could break, because b4 did not recognize their traffic.
- On some Asus Merlin firmware b4 quit right after starting with a misleading message about a missing `xt_connbytes`, because it used a firewall command option the router's tool did not understand.
- The **Traffic** page could show an empty list and empty activity bars when the viewing computer's clock was off by as little as 30 seconds.
- A device could still appear twice on the **Traffic** page, once under its name and once under its bare IP.
- **Discovery** found no working strategy for any site when the domains were entered in quotes, such as a pasted `"discord.com","youtube.com"`. [#241](https://github.com/DanielLavrushin/b4/issues/241)

## [1.65.0] - 2026-06-06

### Added

- The **Block** mode in a set's **Routing** tab blocks the matched domains, IPs or GeoSite categories, such as `category-ads-all`, for every device and the router itself ([Blocking](https://docs.b4core.app/docs/sets/blocking)).

### Changed

- A set routed to an upstream SOCKS5 proxy also sends its UDP traffic from LAN devices, such as QUIC and DNS, through the proxy. The proxy must accept UDP.

### Fixed

- In the **Aggregated** view of the **Traffic** page, a device could appear twice, under its name and its bare IP, and devices added by hand under **Settings, Devices** showed a raw IP instead of their name.
- Starting b4 with a command-line option such as `--skip-tables` saved it into the config, so it kept applying on later starts without it.

## [1.64.0] - 2026-06-01

### Added

- **MTProto Proxy** can use a Cloudflare Worker as a free personal relay, with no domain to buy, for accounts where media, reactions or stickers still fail to load.
- The **DPI Detector** also measures how fast DNS servers answer and tests Telegram's download speed and servers, with a **Legend** for its results. It is based on [dpi-detector](https://github.com/Runnin4ik/dpi-detector).

### Changed

- Telegram routing over WebSocket in bridge mode works on phones as well as desktop, loads media more reliably and always prefers WebSocket, whatever the proxy's transport setting.
- The **DPI Detector** tells apart more of the ways a provider blocks or tampers with a connection, and tests a refreshed list of sites and servers.

### Fixed

- When a set sent its DNS lookups to a chosen server on its **DNS** tab, names failed to resolve and the internet seemed to hang.
- Telegram data centers without a WebSocket edge stalled when the shared fallback domains were rate-limited.
- Copy buttons did nothing when the web interface was opened over plain HTTP, such as `http://192.168.1.1`, because browsers allow the clipboard only over HTTPS.
- Memory and CPU use slowly climbed while **Watchdog** repaired a site again and again, until b4 was killed. [#227](https://github.com/DanielLavrushin/b4/issues/227)
- The **Traffic** page's **Aggregated** view lumped devices without a known MAC address, such as those behind another router, into one Unknown device, and usually showed nothing for a filter such as `!github + !google`, which it read as plain text.

## [1.63.0] - 2026-05-22

### Added

- A switch in the top bar of the **Sets** page turns every set on or off at once.
- **Test direct TCP** under **Settings, MTProto Proxy** checks Telegram directly, bypassing the **DC Relay**, to tell a relay problem from one between b4 and Telegram.
- **Auto-update** under **Settings, Geo Databases** can download both files on every start and on a daily, weekly or monthly schedule. Missing files are downloaded at startup whenever a source URL is set.
- A set's **TCP** tab has **MSS Clamping** for the set's connections, such as a small MSS for a smart TV streaming YouTube. The set must target by IP, GeoIP or source device.

### Changed

- **Watchdog** keeps checking every site while it repairs one, and repairs sites that break close together in one go.
- **Test connection** for the MTProto proxy also completes the MTProto handshake and shows whether connecting failed, the handshake failed, or Telegram or the relay dropped the link right after.

### Fixed

- In rare cases the installer saved the geo database path as `b4/geosite.dat` instead of a full path, and the web interface then refused to download new files.
- **MTProto Proxy** dialled internal Telegram addresses that dropped direct TCP connections, ignored a configured **DC Relay** in **Auto** mode, and could not load media from data center 203 through the relay.
- A geosite or geoip file downloaded or uploaded in the web interface took effect only after a set was next edited.
- Some sites, such as YouTube, failed to load through b4's built-in SOCKS5 server while they worked directly.
- On iptables routers such as Merlin, Keenetic and OpenWrt without nftables, the global MSS overrode the per-device MSS when both were on.

## [1.62.1] - 2026-05-11

### Added

- **Memory Limit** under **Settings, Logging** caps how much memory b4 may use, with values such as `128MiB`, `1g` or `off`. Empty keeps the automatic limit of half the system RAM.
- **MTProto Proxy** can reach Telegram over WebSocket as well as direct TCP, or through Cloudflare with a custom domain when Telegram's WebSocket endpoint is blocked too.

### Changed

- On upgrade, the MTProto proxy switches to the new **Auto** transport, which tries WebSocket first, then TCP.
- The JSON in a set's **Import/Export** leaves out the settings of features that are turned off.

### Fixed

- Some Telegram clients could not connect to the MTProto proxy, because it accepted only one of Telegram's three transport variants.
- On some OpenWrt setups, the traffic of a set routed to an upstream SOCKS5 proxy skipped the proxy and went straight out, because two kernel modules were missing. [#221](https://github.com/DanielLavrushin/b4/issues/221)

## [1.61.4] - 2026-05-10

### Fixed

- b4 could refuse to start with a false "another b4 instance is already running" error after a crash, after a restart from the web interface, or inside containers such as on MikroTik.

## [1.61.3] - 2026-05-09

### Added

- A set's UDP fakes can carry a payload picked in **Fake Packet Payload**: a captured or uploaded `.bin`, a random QUIC Initial made for each fake, or a bundled QUIC preset. Uploads under **Payloads** are marked TLS or QUIC.
- A **?** button next to **DC Relay** under **Settings, MTProto Proxy** shows the current Telegram data center list and ready-to-copy commands for the VPS.
- b4 refuses to start when another b4 process is already running on the same host, and names that process's PID.
- **Combo** and **Disorder** splitting have an **Overlap Length**: that many pattern bytes go in front of a real fragment, with its sequence number shifted back by the same amount.
- Small AI buttons next to some fields explain what the field does. This is experimental: few fields are covered and the answers can be wrong.

### Changed

- When saving the configuration or a set fails, the web interface shows the reason, such as a port already in use.

### Fixed

- Routing a set through an upstream SOCKS5 proxy failed on BusyBox routers, because the 1.49.0 fix for routing table numbers had missed it.
- Blocking all QUIC let through the newer QUIC variant that Chrome on Android uses, so YouTube on phones kept using QUIC.
- On MikroTik containers, routing failed for interface names with dashes, and interface routing went nowhere when several container interfaces shared one bridge with a single gateway.
- Safari on macOS greyed out the downloaded `.tar.gz` backup in the restore file picker.
- In UDP fake mode b4 split every matched UDP packet, which could break voice calls, screen sharing and games.

## [1.60.1] - 2026-05-02

### Added

- A set's **Escalation** tab picks a set to fall back to: when the set keeps failing for a host, b4 moves that host to the other set on its next connection, for an hour by default.
- **Share connection link** under **Settings, MTProto Proxy** shows a `tg://` link with a QR code to scan with a phone, and buttons to copy it, open it in Telegram or share it.

### Fixed

- **Enable Packet Duplication** could stop b4 from starting on routers without `ipset`, such as some Keenetic and Merlin setups. Duplicating whole connections needs `ipset` (`opkg install ipset` on Keenetic or Entware).
- On some setups, **Update** in the web interface did nothing, and the version stayed the same after a restart.
- Adding an IPv6 address from the **Traffic** page cut it to its first group, such as `2a01`, put the port into the suggested CIDRs and broke its IPInfo and ASN lookups.

## [1.50.1] - 2026-04-29

### Changed

- The error log keeps its history across restarts instead of being wiped at every start, so a crash leaves a trace.
- The **Connections** page is renamed **Traffic**, and old `/connections` links redirect to it.
- The **Traffic** page keeps updating at any log level, such as `Warn` or `Error`, and shows the last few hundred events as soon as it opens.

### Fixed

- A set routed through an upstream SOCKS5 server, local or remote, sent its traffic directly instead of through the proxy.

## [1.50.0] - 2026-04-27

### Added

- **Combo** fragmentation's overlap can be tuned in the web interface again, with a preset (TLS handshake, HTTP GET, zeros) or a custom byte pattern.
- A set's **Routing** tab can forward matched traffic to a local or remote SOCKS5 proxy, such as Xray or sing-box, with the **Upstream SOCKS5 proxy** mode.

### Changed

- The web interface is redesigned, with cleaner type and spacing, calmer colours, larger numbers and better keyboard access.

## [1.49.1] - 2026-04-20

### Fixed

- Routing broke until b4 was restarted when the proxy's network interface, such as that of tun2socks or sing-box, was recreated or was not ready when b4 started.
- In the **Aggregated** view of the **Connections** page, the set name showed twice when a set matched by both domain and IP, and the SOCKS5 proxy label could overlap the domain.

## [1.49.0] - 2026-04-18

### Added

- Devices behind another router that are missing from the ARP table can be added by IP under **Settings, Device Filtering**, for per-device filtering and MSS clamping. [#185](https://github.com/DanielLavrushin/b4/issues/185)
- TCP fake settings can generate a fake TLS handshake from any domain typed in, such as `example.com`.
- **Refresh Telegram DC list** under **Settings, MTProto Proxy** fetches Telegram's current data center list, which b4 also fetches on every start. [#190](https://github.com/DanielLavrushin/b4/issues/190)
- The **Connections** page opens in a new **Aggregated** view that groups packets by device, protocol and domain or IP, with an activity chart per group and a device list to filter by. The old table is under **Raw feed**.
- The set editor has a save button that stays in the bottom-right corner while scrolling.
- **Reset Config to Defaults** under **Settings, Core Controls** is back, and keeps the sets, the web server settings and the geo file paths.

### Changed

- When one strategy works for every tested domain, **Discovery** shows them as one group that is applied in one click.
- **RST Injection Protection** uses more checks to tell real resets from injected ones.

### Removed

- The separate `mac_aliases.json` file is gone: device names are stored in the main config, and existing names are moved there on upgrade.

### Fixed

- Traffic routing failed on Keenetic routers, because routing table numbers could go above 255, which BusyBox does not support.
- With **Syn Fake** on, the fake SYN packet always carried the built-in payload instead of the one picked in the set.
- When the router forwarded DNS through a local proxy such as `https-dns-proxy`, domains from GeoSite categories were not added to routing sets.
- The `Error` log level set in the web interface went back to `Info` after a restart.
- Some Telegram pictures and files did not load through the MTProto proxy, because b4 did not know Telegram's media data centers such as DC 203. [#190](https://github.com/DanielLavrushin/b4/issues/190)
- When another service, such as a VPN, QoS or policy routing, put its own marks on packets, b4 missed its own mark, so queued packets could loop and masquerade on VPN interfaces broke with routing on.

## [1.48.1] - 2026-04-05

### Added

- A **Reject** option for QUIC and UDP answers matched packets with an ICMP Port Unreachable instead of dropping them, so clients fall back to TCP almost at once.

### Fixed

- A device that changed its IP address appeared twice in the device list, under its old and its new IP.

## [1.48.0] - 2026-04-05

### Added

- Before an update b4 saves a copy of its config file, such as `b4.json.bak.v1.47.2`, and the update dialog suggests downloading a full backup from **Settings, Backup**.
- **Watchdog** checks listed domains or URLs and, after repeated failures, runs a quick **Discovery** and applies the first working setup. It is configured under **Settings, Discovery** and has its own page.
- **IP Block Detection** notices when a destination IP is blocked outright and resets the connection, so the device tries another server sooner instead of waiting for a timeout.
- **RST Injection Protection** in a set's TCP settings drops fake TCP resets injected by DPI, spotted by their TTL, by arriving before any server reply, or by repeating on one connection.

### Changed

- **Sequence Offset** appears only for the **Past Sequence** and **Random Sequence** strategies, and **Timestamp Decrease** only for the Timestamp strategy.

### Fixed

- Adding an IP by hand that a GeoIP category in the same set already held caused a firewall error.
- Traffic routing stopped working after about an hour, because GeoIP and static IPs were added to the firewall with an expiry.

## [1.47.2] - 2026-04-01

### Changed

- **Discovery** tests several domains at once instead of one by one, which makes it noticeably faster.

## [1.47.0] - 2026-03-31

### Added

- **System Info** in **Settings** shows full system diagnostics, such as the kernel, network interfaces, firewall status with an NFQUEUE test and b4's paths, and copies them as JSON for sharing.

### Changed

- **Discovery** tests on its own isolated path, so the network's normal traffic is not affected while it runs.
- The **Discovery** page is redesigned: domains that share a strategy form one group applied in one click, logs open in a window, and raw MB/s figures are replaced by ranked presets and improvement percentages.
- The built-in Swagger UI is gone, which shrinks the binary from about 27 MB to 13 MB: the API docs are at [docs.b4core.app/swagger](https://docs.b4core.app/swagger), and `/swagger/` redirects there.
- All sets can be deleted, since b4 no longer keeps a default set, and an empty **Sets** page offers **Create Set**.
- The config file stores only the settings that differ from their defaults, so it is much shorter.

### Removed

- The **Reset to defaults** button is gone from **Settings**.

### Fixed

- Restarting WireGuard or another VPN while b4 was running broke routing until b4 was restarted.
- Cancelling **Discovery** did not reliably stop it at once or remove all of its firewall rules.
- **Discovery** history showed every entry as run a moment ago, because timestamps were not saved correctly.

## [1.46.6] - 2026-03-26

### Fixed

- With many sets and hundreds of thousands of IPs, b4 failed to start after a restart, because all the IPs went into a single firewall command.
- Routing ignored IP ranges, such as `10.0.0.0/24`, in a set's target IPs and routed only single IPs.
- Updating from the web interface appeared to succeed, but the version stayed the same.
- The update dialog showed a prerelease version, such as `1.46.6rc`, as a downgrade.

## [1.46.5] - 2026-03-23

### Fixed

- Domain-based routing failed on routers that run their own DNS, such as `dnsmasq` on OpenWrt, and the firewall monitor did not restore b4's DNS rule for routing when it went missing.
- A set whose routing was turned off and on again did not route until new DNS traffic came in.

## [1.46.0] - 2026-03-22

### Added

- A built-in Telegram **MTProto Proxy** disguises Telegram traffic as HTTPS and can go through an external **DC Relay** when Telegram's data centers are blocked by IP. It is set up in **Settings** with a fake SNI domain and a generated secret to paste into Telegram.
- The SOCKS5 proxy splits the first data packet of matched connections, which helps with protocols other than TLS.

### Changed

- Connections matched by a learned IP keep fragmentation and desync on even when their traffic is not TLS.

## [1.45.1] - 2026-03-21

### Added

- An interactive Swagger UI at `/swagger/` lists every REST API endpoint and can test them.

## [1.45.0] - 2026-03-21

### Added

- A set's **Routing** tab sends the traffic of its matched domains through a chosen network interface, using the IPs b4 resolves for them.
- The SNI, OOB and TLS record split positions, **First Segment Delay**, **Jitter Max** and **Fakes Per Segment** accept ranges, and several splitting strategies can be selected, so each connection gets a random pick.
- Domains and IPs can be edited as bulk text, and long lists collapse behind a **+N more** button.
- ASN details for a destination IP can be looked up straight from the connections table, and they are stored on the router, so every browser and device sees the same.

### Changed

- Fake packets look more like the real connection: RST, FIN and ACK fakes keep the original TCP options, their TTL never exceeds the real packet's, and they combine a bad checksum with that TTL.
- The log shows the domain and TLS version for every packet of a connection, not only the first.

### Fixed

- Uploaded and downloaded payloads got wrong file names, and disabled sets caused needless payload loading errors.
- Turning on syslog in a Docker container made b4 crash in a loop, because the syslog socket does not exist there.
- The time zone set in **Settings** had no effect on some routers, such as Keenetic, that lack time zone data.
- The log level set in **Settings** went back to Info after a service restart.
- A wrong certificate or key path in **Settings** made b4 crash on the next restart.

## [1.44.1] - 2026-03-15

### Added

- A specific **Time zone** for the logs can be picked under **Settings, Logging**.
- **Firewall Engine** in **Settings** picks `nftables`, `iptables` or `iptables-legacy` by hand when automatic detection does not work for a device.

### Changed

- Log timestamps use the system's local time instead of UTC.

### Fixed

- **Download** on a captured payload did nothing, and an uploaded payload was saved with its extension twice, such as `tls_payload_bin.bin`.
- **Update** in the web interface seemed to run, but b4 restarted with the same version.
- With a set's TLS version filter on **Any**, connections broke right after they opened, because encrypted data packets were handled as new TLS handshakes.
- b4 failed to start on some Xiaomi routers, such as the BE7000 and AX3200, with a "Could not process rule" error, because it picked nftables where nftables does not fully work. [#132](https://github.com/DanielLavrushin/b4/issues/132), [#133](https://github.com/DanielLavrushin/b4/issues/133)

## [1.43.0] - 2026-03-14

### Added

- The web interface can be switched between English and Russian in **Settings**.

### Fixed

- With a web interface login set, an update got stuck on "Waiting for service to restart" forever.
- On busy routers, packet queue overflows flooded the log with repeated errors and wasted CPU.
- Some QUIC connections showed a blank TLS version in the log and the connections table.

## [1.42.1] - 2026-03-13

### Fixed

- On some routers with older kernels, such as the ASUS RT-AC68U, b4 started but captured no traffic.
- **Copy JSON** in **Import/Export** did not work in Safari on Mac.

## [1.42.0] - 2026-03-11

### Added

- **Settings, Backup** downloads a `.tar.gz` with the config, the **Discovery** and detector history, device names and captured payloads, and restores it from an upload followed by a restart of b4.
- The web interface can be protected with a username and password under **Settings, Web Server, Authentication** or during installation.
- **TLS Version Filter** under **Targets** limits a set to TLS 1.2 or 1.3 traffic, a **Discovery** run for one version sets it on its result, and the connections log shows each connection's TLS version.

### Changed

- After an import in **Import/Export**, a green banner confirms it and reminds the user to save, instead of the page silently switching to another tab.

## [1.41.0] - 2026-03-09

### Added

- **DPI Detector** results are saved under **Previous Results**, where past runs can be expanded or deleted, and the page remembers which tests were selected.
- **Discovery** keeps its results on the router under **Previous Results**, where a past strategy can be applied without re-testing, a domain re-tested or entries deleted, and a page refresh during a run reconnects to it.
- Adding a domain or IP to a set warns when another set already has it, including inside GeoSite categories.

### Changed

- A failed **Discovery** preset takes about 5 seconds instead of 35.
- **Discovery** spots a domain blocked by IP early, marks it **Blocked** and skips the extended search for it.
- Errors read in plain words, such as "connection timed out" instead of "context deadline exceeded", and firewall errors name the rule that failed and why.

### Fixed

- Traffic could get the wrong set when sets had overlapping IP ranges, and later packets of a connection matched by domain could fall back to a set chosen by IP alone.
- **Discovery** reported DNS as poisoned on routers using encrypted DNS such as DoH or DNSCrypt, and could count a Russian provider's block page as a working bypass.
- **Discovery** could hang after detecting DNS poisoning, because its DNS workarounds waited for an answer with no timeout.

## [1.40.0] - 2026-03-08

### Added

- Several sets can be selected and deleted at once with **Select** in the toolbar, and **Select All** picks every set.
- With **Fake Per Segment** in the **Combo** or **Disorder** settings, b4 sends the chosen number of fake overlap packets before every real segment, not only the first, and **Discovery** tests this mode too.
- **Faking** has two new fake payload types: **All Zeros** and **Inverted Original**, a flipped copy of the real data.
- GeoIP and GeoSite `.dat` files can be uploaded from the computer with **Upload** under **Settings, Geo Databases**.
- A set can capture TCP ports other than 443, such as `80,5222,8000-9000`, with the **TCP Port Filter** on its TCP tab; port 443 is always included, and no restart is needed.

### Changed

- There is no special main set any more: all sets are equal, and an existing main set becomes a regular set.
- TCP and UDP connection bytes limits moved from the sets to **Settings, Core, Queue Settings**.
- The device list is sorted by name, with selected devices at the top.
- An exported set lists only the settings that differ from the defaults, plus a `b4_version` tag, and pasted set JSON applies at once, with buttons to copy and paste it.

### Fixed

- Pasting a set from the clipboard did not work on Android.
- b4 had several memory leaks that made its memory use grow.
- `veth` interfaces, which MikroTik containers use, were missing from the network interface list.

## [1.39.1] - 2026-03-02

### Added

- **MSS Clamping** makes packets smaller so blocked content such as YouTube on smart TVs loads, for all devices under **Settings, Network** or per device under **Settings, Device Filtering**.
- The new **DPI Detector** page checks whether the provider tampers with traffic, with tests for DNS spoofing, blocked websites and dropped connections.
- **NAT Masquerade** under **Settings, Feature Flags** sets up NAT for b4 in a container such as Docker, LXC or MikroTik CHR, also as `--masquerade` and `--masquerade-interface`.
- **Discovery** remembers the strategies that worked before and tries them first for a new domain.
- One **Discovery** run can test several domains or full URLs and finds a configuration for each.
- A button opens the **Discovery** logs in a large window.

### Changed

- **Discovery** tests real-world combinations of techniques instead of single tricks, finds the **Fake TTL** for **Combo** from preset values and tries every faking strategy, `timestamp` included.
- Saving in the set editor keeps the current tab and scroll position instead of returning to the sets list.
- The `--config` flag is optional: b4 looks for its config in `/etc/b4/` and `/opt/etc/b4/` and creates one on first run if there is none.
- The SOCKS5 proxy received updates. ([#64](https://github.com/DanielLavrushin/b4/pull/64), thanks [@remmody](https://github.com/remmody))
- Devices are found from the system ARP table instead of DHCP lease files, so they show up on any router; their names still come from DHCP when available.

### Removed

- The **Decoy SNI Domains** setting of **Combo** is gone: the decoy packet uses the fake payload set in **Faking**.

### Fixed

- **Discovery** used built-in payloads such as duckduckgo in place of the selected custom ones, and a configuration added from it lost its captured payload file and showed the wrong payload type.
- A configuration that passed in **Discovery** could fail once added, because the saved configuration differed from the tested one.
- The TTL search in **Discovery** did not change the fake packets' TTL, so every attempt was the same.

## [1.38.0] - 2026-02-27

### Added

- **TLS Version** in **Discovery** chooses the TLS version for probing: `Auto`, `TLS 1.2` or `TLS 1.3`.
- A built-in SOCKS5 server under **Settings, Network Configuration** lets apps such as browsers, curl or torrent clients send traffic through b4, with an optional login. ([#48](https://github.com/DanielLavrushin/b4/pull/48), thanks [@remmody](https://github.com/remmody))

### Changed

- The device manufacturer database, about 6 MB, is not downloaded at startup any more: turn on **Vendor Lookup** under **Settings, Device Filtering** to see device brands.
- In Docker, b4 shows `docker pull` instructions in place of the update button.

### Fixed

- The **Settings** page crashed to a white screen in containers such as Docker and MikroTik, because the interface list did not expect container interfaces such as `veth`. ([#44](https://github.com/DanielLavrushin/b4/issues/44), thanks [@kakosmakos](https://github.com/kakosmakos))
- The DNS check could report "DNS poisoned" when DNS was fine: it timed out before trying the system resolver, and CDN domains such as YouTube got different but valid addresses from different resolvers. [#46](https://github.com/DanielLavrushin/b4/issues/46)

## [1.37.0] - 2026-02-26

### Added

- The web interface can use HTTPS, set under **Settings, Network, Web Server**, and the installer offers it with the router's own certificate on OpenWrt and Asus Merlin. ([#40](https://github.com/DanielLavrushin/b4/pull/40), thanks [@Shiperoid](https://github.com/Shiperoid))

### Changed

- A **Reset Statistics** button on the dashboard clears all counters without a restart and replaces the restart button, which stays under **Settings, Core Controls**.

### Fixed

- Downloading a geo database failed with error 500 on routers with little free storage.

## [1.36.0] - 2026-02-16

### Changed

- The play and stop control of the **Connections** table is a floating button in its bottom-right corner instead of a control in the top bar.
- The set editor has 5 tabs instead of 7: **TCP** holds **General**, **Splitting** and **Faking**, and **Faking** groups its settings in collapsible sections that show their status.

## [1.35.2] - 2026-02-16

### Fixed

- Edits to a new set could be undone at random, and its tabs reset, while it was being created.
- Custom TCP window values added or removed in the `Oscillate` or `Random` window modes were not saved.
- **Packet Duplication** connections showed wrong data in the connections table.

## [1.35.0] - 2026-02-15

### Added

- **Packet Duplication** sends several copies of each packet, against providers that throttle addresses such as Telegram's by dropping packets. It replaces the set's other DPI bypass and uses more bandwidth.

### Changed

- The `GeoSite` and `GeoIP` databases download separately and from different sources, so either can be used alone, and [b4geoip](https://github.com/DanielLavrushin/b4geoip) is a built-in source.

### Fixed

- Connections to matched domains sometimes showed as not matched, because learned addresses were lost when settings were saved or geo databases updated.

## [1.34.0] - 2026-02-10

### Added

- **Segment 2 Delay** takes a min-max range, and each packet waits a random delay within it; equal values keep a fixed delay.
- The redesigned **Dashboard** shows device activity per domain, domains in no set with **Add to set**, bypass statistics and active sets, and counts all traffic, not only bypassed connections.
- The **TCP Timestamp** faking strategy sends fakes with an outdated timestamp, so the server ignores them while DPI still reads them, inspired by [youtubeUnblock](https://github.com/Waujito/youtubeUnblock).

### Changed

- The set editor opens as a full page instead of a popup.

### Fixed

- UDP and QUIC traffic stopped matching its set after a while, because b4 dropped the link between a server and its domain even while traffic was flowing.

## [1.32.0] - 2026-01-19

### Added

- **Discovery** can require 1 to 5 successful connections before it accepts a configuration, with the validation tries setting (default 1).

### Changed

- Connection logs show the domain of QUIC traffic that no set handles.

### Fixed

- A set with a domain such as `youtube.com` handled only the first UDP connection to it, not all UDP traffic to that domain's addresses.
- A UDP port filter such as 443 for one service could match all UDP traffic on that port, including other services.

## [1.31.2] - 2026-01-16

### Fixed

- Custom payload files did not work in **Discovery** for older configurations with relative paths such as `captures/payload.bin`.

## [1.31.0] - 2026-01-11

### Added

- Support for the TCP MD5 option, which **Discovery** also tests as a strategy.

### Changed

- b4 uses the `iptables` multiport module for rules with several ports when it is available, and the installer detects it.
- Importing a set made before 1.29 converts it to the current format.

## [1.30.1] - 2026-01-02

### Fixed

- Importing a set saved before 1.29 gave a white screen.

## [1.30.0] - 2026-01-02

### Added

- **Discovery** can test strategies with captured TLS payloads instead of the built-in ones.

### Fixed

- **Discovery** could crash when its status was checked during a run, for example on **Create Set** or a page refresh (reported by Andrew B.).

## [1.29.1] - 2026-01-01

### Added

- **Incoming Response Bypass** works around throttling that stalls downloads after about 15 KB; the TCP incoming `fake` mode suits most cases.

### Fixed

- The **Discovery** progress bar could go over 100%.

## [1.28.1] - 2025-12-30

### Added

- **Bind Address** sets the address the web interface listens on, such as `127.0.0.1` for local access only or `0.0.0.0` for all interfaces, IPv6 included.
- **Skip DNS** in **Discovery** skips the DNS checks for faster results when DNS is not blocked.
- Support for MIPS devices with soft float.
- **Post-ClientHello RST** in the TCP **Desync** settings sends a fake connection reset after the ClientHello, to confuse DPI that tracks connection state.

### Changed

- **Discovery** finds the best TTL for the network instead of using a fixed value.
- On shutdown, b4 waits up to 5 seconds for packets in progress to finish.
- Fragmented IP packets pass b4 untouched, since it cannot inspect them in full.
- **Combo** can send a decoy, a fake ClientHello with an allowed domain such as ya.ru or vk.com, before the real split request; it is on the set's fragmentation tab.

### Removed

- The DPI fingerprinting phase of **Discovery** is gone, as it was slow and unreliable, so testing starts at once.
- The `overlap` fragmentation strategy is gone; **Combo** covers it.

### Fixed

- DPI bypass did not work for LAN devices on routers with `nftables`, such as OpenWrt.
- The TCP **Desync** modes `rst`, `fin`, `ack`, `combo` and `full` sent malformed packets over IPv4 and IPv6, so they failed or were ignored.
- The **Packet Mark** setting was missing from the network settings.
- Connections could hang when packets got stuck in b4's processing queue.
- Packet sending could go on after b4 stopped, which could crash it or make it unstable during a restart.
- On routers with several network interfaces, b4 could make wrong filtering decisions because of a race in its interface name cache.

## [1.27.2] - 2025-12-27

### Fixed

- Adding several services with many UDP ports, such as Discord or WhatsApp, could make the `iptables` rules fail, so b4 did not start or restart.

## [1.27.1] - 2025-12-27

### Fixed

- DNS poisoning detection could report poisoning when the system resolver and encrypted DNS returned the same addresses in a different order.
- UDP and QUIC traffic of services matched by address, such as YouTube, could be handled wrongly when another set had a port filter, which could break video.
- Downgrading from the web interface reported success but kept the current version.

## [1.27.0] - 2025-12-27

### Added

- A **Discovery Logs** panel shows the progress of a run as it happens, through DPI fingerprinting, DNS checks and strategy tests.
- For [more than 20 major services](https://github.com/DanielLavrushin/b4/blob/main/src/discovery/dns.json) such as Instagram, YouTube, Telegram, Discord and Netflix, **Discovery** tests their full IP ranges instead of a single DNS answer.
- When it finds geo-blocked addresses for a CDN service, b4 sends split DNS queries to external resolvers to find working servers.
- **Discovery** detects when the provider returns fake addresses for blocked sites, by checking with encrypted DNS from Google, Quad9 or Cloudflare, and then tests against the real server.

### Fixed

- QUIC traffic, used by YouTube, Google and many other sites, was ignored when any set had custom UDP ports.
- UDP traffic to an address could go to a set that matched only its port instead of the set that defined both the address and the port.

## [1.26.3] - 2025-12-26

### Added

- Several domains can be pasted into **Targets** at once, separated by spaces, commas or pipes.

### Fixed

- UDP port ranges such as `50000-50032` could stop b4 from starting on older devices, with `iptables` or `nftables`.

## [1.26.2] - 2025-12-25

### Changed

- UDP port ranges written with a dash are converted to the colon form that `iptables` expects.

### Fixed

- **Discovery** missed DPI that cuts a download mid-transfer, for example after 16 KB, and could report a false success.
- Payload capture always reported a timeout, even when the target site was visited.

## [1.26.1] - 2025-12-24

### Added

- Payload files can be uploaded on the **Capture** tab of **Settings**, and **My own Payload file** in **Faking** uses a captured or uploaded payload.
- Adding a configuration from **Discovery** also adds the matching geosite category, such as `youtube` for `youtube.com`.
- A set can filter UDP by port, and b4 listens only on those ports.
- Changes to UDP ports, connection limits and other core settings update the firewall rules without restarting the service.
- **Discovery** accepts full URLs such as `https://youtube.com/watch?v=xyz`, not only domain names.
- A sequence overlap pattern (`seqovl`) in **Fragmentation** mixes custom bytes into TCP segments to confuse DPI while the real data reaches the server intact; it works with **Disorder** and **Combo**.

### Changed

- By default b4 handles UDP only on port 443 (QUIC), not on all UDP ports.
- Device names in the **Connections** tab appear only when **Device Filtering** is on in **Settings**.

### Fixed

- b4 crashed on startup when `geosite.dat` or `geoip.dat` had been deleted by hand.
- Re-downloading geodat files did not reload the domain and IP targets until the service restarted.
- Turning IPv6 off in the config did not stop IPv6 packet processing.

## [1.25.4] - 2025-12-21

### Fixed

- Kernel module errors flooded the system log on OpenWrt routers.

## [1.25.3] - 2025-12-21

### Fixed

- Forwarded traffic got no bypass with `nftables` on OpenWrt, because b4 caught the packets after NAT.

## [1.25.2] - 2025-12-19

### Changed

- When the fake SNI does not fit, the `overlap` strategy falls back to plain TCP fragments.

### Fixed

- The `overlap` strategy could crash b4 on 32-bit ARM devices.

## [1.25.0] - 2025-12-19

### Added

- The **Connections** table names the device behind each connection, from DHCP or a custom name under **Settings, Devices**, and **Device Filtering** limits DPI bypass to chosen devices or excludes them.
- Filters in the **Connections** table combine with `+`, exclude with `!` and match one field with `domain:`, `asn:` or `device:`, as in `domain:youtube+!asn:cloudflare`.

### Removed

- The `--skip-local-traffic` option is gone: it caused connection problems and did not solve the problem it was added for.

## [1.24.0] - 2025-12-17

### Added

- `--skip-local-traffic`, on by default under **Core**, leaves the router's own traffic alone so a transparent proxy such as Xray, Clash or Sing-Box on it keeps working; a change needs a restart.
- b4 can be limited to chosen network interfaces, such as `eth0` or `tun0`, under **Core** without a restart; none selected means all.

### Fixed

- Errors other than crashes were written to `errors.log`.

## [1.23.1] - 2025-12-16

### Fixed

- The `overlap` fragmentation strategy crashed b4 when the SNI went past the end of the packet.
- Crash errors were not written to `errors.log`.

## [1.23.0] - 2025-12-16

### Added

- An error log for crash diagnostics, `/var/log/b4/errors.log` by default, with a configurable path.
- The **Connections** table can filter by ASN name, everywhere or with the `asn:` field filter.

### Changed

- A set's TCP and UDP connection byte limits cannot exceed the main set's, in the web interface and in the service.

### Fixed

- The web interface could leak memory.

## [1.22.1] - 2025-12-09

### Fixed

- **Discovery** showed "0 of 0 checks" and "NaN%" while checking DNS, and a run that finished too fast ended with "Failed to fetch discovery status".
- DNS poisoning detection falsely triggered for CDN domains, because it compared addresses literally instead of testing whether they connect.

## [1.22.0] - 2025-12-08

### Added

- **DNS Redirect** in a set sends its DNS queries to a clean resolver, to get around DNS poisoning by the provider.
- **Discovery** checks DNS and manages DNS settings, with support for custom DNS servers.
- Enter starts **Discovery**. ([#5](https://github.com/DanielLavrushin/b4/pull/5))

### Changed

- **Discovery** has better fragmentation configurations and new presets for **Combo** and **Disorder**.

## [1.21.1] - 2025-12-07

### Added

- A **SYN Fake TTL** slider in the TCP settings.

### Fixed

- With the `randseq` or `pastseq` faking strategies, SYN fakes went out with full TTL, reached the server and broke connections.
- Config validation compared a set's connection bytes limit with the main set's default instead of its saved value.
- The web interface crashed when a set was created with **Overlap** fragmentation.

## [1.21.0] - 2025-12-07

### Added

- New fragmentation strategies for modern DPI: **Combo** (recommended), **Disorder**, **Overlap**, **Extension Split**, **First-Byte Desync** and **Hybrid**.

### Changed

- b4 leaves packets to private addresses alone.

### Fixed

- Saving a set was slow.

## [1.20.3] - 2025-12-03

### Added

- **Discovery** measures a reference domain first and requires the target to download at least 4 KB at 30% or more of its speed; the reference domain is under **Settings, Discovery**, `yandex.ru` by default.

### Changed

- **Discovery** finds the TTL and split position with a binary search, cutting its second phase from about 50 tests to 15.
- The save button of the set editor shows a spinner and is disabled while saving, so a set cannot be saved twice.
- Sets are reordered by drag and drop instead of up and down arrows.

### Fixed

- **Discovery** trusted the first successful response and missed DPI that throttles, stalls or blocks resources mid-transfer.

## [1.20.1] - 2025-12-03

### Added

- Experimental DPI fingerprinting in **Discovery** tries to identify the DPI type and blocking method and tests the likely strategies first, falling back to the full scan when unsure.
- **Faking** offers a choice of fake payload: **Random**, **Custom** or a built-in one, `FakeSNI1` (google, the default) or `FakeSNI2` (duckduckgo).
- **Discovery** tries both built-in fake payloads, since DPI may react to only one, and uses the one that works for the rest of the run.

## [1.20.0] - 2025-12-03

### Added

- QUIC fragmentation splits at the site name inside the encrypted QUIC packet, against DPI that decrypts the first QUIC packet to read it.
- The OOB strategy supports the `tcp_check` and `md5sum` faking strategies, for when TTL-based faking is unreliable.

### Changed

- In fragmentation settings, "Middle SNI" is renamed **Smart SNI Split**, a packet diagram shows the split, and the manual position is in a collapsible advanced section.

### Fixed

- IP-level fragmentation broke in several ways: IPv4 UDP fragments had a wrong offset, IPv6 fragments split at the wrong place, and **Smart SNI Split** was ignored.
- QUIC fragmentation sent fragments in reverse order when the reverse option was off, and in normal order when it was on.
- The OOB strategy broke connections: its extra byte reached the server, and the second segment had a wrong sequence number.

## [1.19.1] - 2025-12-01

### Changed

- **Discovery** tests more than 150 configurations.
- The SNI split position in fragmentation is a number field instead of a slider.

### Fixed

- New sets with geosite or geoip categories did not match traffic until the service restarted.

## [1.19.0] - 2025-11-29

### Added

- Sets can be filtered by name, domain, geosite category or geoip category.
- Two sets can be compared side by side, with the differences grouped by TCP, UDP, fragmentation, faking and targets.
- **Discovery** names a new set after its preset, such as `tcp-frag-rev-fake`, and offers to add the domain to a similar existing set instead.
- **Discovery** shows working configurations as tests finish, so they can be applied before the scan ends, and skips the later phases when no DPI is detected.

### Changed

- New sets are added at the top of the list.
- **Sets** moved from the **Settings** menu to the main navigation.
- Creating, editing, deleting, duplicating, reordering and turning sets on or off save at once, without **Save Changes**.

### Fixed

- Refreshing the **Settings** page lost the selected tab.

## [1.18.5] - 2025-11-27

### Changed

- The fake SNI payload is back to the earlier one, for better compatibility.

## [1.18.4] - 2025-11-25

### Changed

- The default **Desync** mode of a set is `off`.

## [1.18.3] - 2025-11-25

### Fixed

- The **Domains** table stopped updating with new packets, and its columns were cramped.

## [1.18.2] - 2025-11-25

### Added

- **ClientHello Mutation** works for IPv6.

### Fixed

- **Discovery** presets lacked default values for the **SNI Mutation**, TCP window and **Desync** settings.

## [1.18.1] - 2025-11-25

### Fixed

- The **ClientHello Mutation** mode could not be changed in the **Faking** settings.

## [1.18.0] - 2025-11-24

### Added

- **TCP Window Manipulation** (`--tcp-win-mode`) sends fakes with altered TCP window sizes to confuse DPI that tracks connections, in the modes `oscillate`, `zero`, `random` and `escalate`.
- **TCP Desync Attack** (`--tcp-desync-mode`) injects fake RST, FIN or ACK packets with low TTL and broken checksums to throw off DPI connection tracking: `rst`, `fin`, `ack`, `combo` or `full`.
- **SNI Mutation** changes the structure of the ClientHello to get past DPI fingerprinting, in the modes `duplicate`, `grease`, `padding`, `reorder`, `full` and `advanced`.

## [1.17.1] - 2025-11-23

### Added

- The **Out-of-Band** (OOB) strategy (`--frag=oob`), with configurable position, reverse order and character, which **Discovery** also tests.
- **TLS Record Splitting** (`--frag=tls`) splits the ClientHello into several TLS records, against DPI that expects one.
- **SACK dropping** (`--tcp-drop-sack`) strips Selective Acknowledgment options from TCP headers to force full retransmissions and confuse DPI that tracks connections.

### Changed

- The fake SNI payload, the default included, uses a TLS 1.3 ClientHello for `staticcdn.duckduckgo.com`.
- For domains longer than 30 bytes, the SNI split falls 12 bytes before the end of the name, so suffixes such as `googlevideo.com` are split across packets.
- Sets with large geosite or geoip categories use 70-90% less CPU.
- Downloaded geodat files are available in the sets manager at once, without a restart.
- The **Fragmentation** tab of a set is reworked.
- `--frag-sni-reverse` is renamed `--frag-reverse`.

### Fixed

- Changing the log level needed a reload of the web interface.
- b4 failed to start with the error `TCP ConnBytesLimit greater than main set`, because config validation compared the main set with itself.

## [1.16.1] - 2025-11-20

### Fixed

- YouTube and other video could buffer or hang and sites loaded slowly, because with default settings each QUIC packet held up the kernel queue for at least 6 ms.
- IPv6 QUIC packets used the TCP delay settings instead of the UDP ones.
- A set created from the **Domains** page lost its custom name and got a generic one such as "Set 1".

## [1.16.0] - 2025-11-17

### Added

- A set can be turned off without deleting it.
- A clear button next to a set's IP and CIDR list.
- `GeoSite` and `GeoIP` files can be downloaded from **Settings**, with preset sources.

### Changed

- Test domains are managed on the redesigned **Test** page instead of in **Settings**.
- **Discovery** has new test strategies.

### Fixed

- The **Domains** page slowed down badly when ASN filters were added.

## [1.15.0] - 2025-11-16

### Added

- Fake SYN packets with a configurable payload length go out before the real connection opens, to confuse DPI; set them with `--tcp-syn-fake` and `--tcp-syn-fake-len` or in the TCP settings.
- With an IPInfo token under **Settings, API**, clicking a destination address on the **Domains** page shows its location, ASN, organization and network.
- [RIPE Stat](https://www.ripe.net/) integration shows the prefixes an ASN announces and details about the network, to help target address ranges.
- Sets can be exported as JSON files and imported on another device.

### Changed

- **Discovery** results show a configuration card for each domain instead of a single recommended configuration.

## [1.14.0] - 2025-11-13

### Added

- Adding a domain or an IP or CIDR address from the **Domains** page asks which set it goes to.
- A **Discovery** result can be added to the sets with one click.

### Changed

- **Discovery** testing is rebuilt to be more reliable and faster.

### Fixed

- b4 leaked memory over time.
- Updating b4 through the web interface could fail.

## [1.13.0] - 2025-11-10

### Added

- Clicking a destination address on the **Domains** page adds the exact IP or its CIDR range to a set, without a reload or restart.
- A switch on the **Domains** page shows all packets or only those with a known domain, which helps when checking UDP traffic.
- Sets show how many domains and addresses their geodat categories contain.
- A new tab on the **Test** page shows a **Discovery** configuration card for each domain instead of a single recommended configuration.

### Changed

- In the reworked UDP tab of a set, port filtering takes one field such as `80,443,2000-3000` instead of from and to, and `--udp-dport-filter` replaces `--udp-dport-min` and `--udp-dport-max`.
- The packet count badge on the **Domains** menu counts only packets handled by b4's targets.

### Fixed

- UDP entries were not logged when UDP packets were set to be ignored.
- The web interface crashed when the filter on the **Domains** page was used.
- Manually added domains needed a service restart when no geodat files were configured.
- The tests reported failure when a site answered with an HTTP status other than 200, even though DPI was bypassed.

## [1.12.0] - 2025-11-09

### Added

- Configuration sets give different targets their own TCP, UDP, fragmentation and faking settings, matched by domain, IP or CIDR range, or UDP port range.
- Support for `geoip.dat`.

## [1.11.0] - 2025-11-05

### Added

- A DPI bypass test on the **Test** page checks the configured domains and measures download speed, with its settings under `/settings/checker`.
- A button in the **Core** tab of **Settings** resets b4's settings to their defaults.

### Changed

- The restart button moved to the **Core** tab of **Settings**, under **Core Controls**.

### Fixed

- Restarting the service from the web interface did not work with some service managers (Entware, OpenWrt, `systemctl`).
- The pause shortcut `P` on the domains and logs pages fired while typing in the search field.

## [1.10.1] - 2025-11-03

### Fixed

- Blocked sites could fail to load at random in Safari, Firefox and Chrome.

## [1.10.0] - 2025-11-02

### Added

- b4 restores its `iptables` or `nftables` rules when they disappear, without a manual restart; `--tables-monitor-interval` sets how often it checks (10 seconds by default, `0` turns it off).

## [1.9.2] - 2025-11-02

### Changed

- b4 keeps connection state for longer and with a larger buffer, and checks the characters of detected domain names more strictly.

## [1.9.1] - 2025-11-02

### Fixed

- The geosite path field was missing from the settings.

## [1.9.0] - 2025-11-02

### Added

- Hotkeys on the **Domains** and **Logs** pages: `Ctrl+X` or `Delete` clears the entries, and `P` or `Pause` pauses the stream.
- Regex entries in geosite files are read.
- TCP and UDP connection bytes limits in the network settings.

### Fixed

- The settings showed a wrong total number of domains.

## [1.8.0] - 2025-11-01

### Added

- Support for `nftables`.

### Changed

- `--skip-iptables` and `--clear-iptables` are renamed `--skip-tables` and `--clear-tables`.
- TCP connections are split into fragments after the domain name (SNI) is found.

## [1.7.0] - 2025-10-31

### Added

- A **Restart Service** button in **Settings** restarts b4 from the web interface.
- The **Domains** table sorts by a column when its header is clicked.
- b4 can update to a new version from the web interface.

### Removed

- The `iptables` rule in the `OUTPUT` chain.

## [1.6.0] - 2025-10-29

### Removed

- The unused `--conntrack` and `-gso` flags.

### Fixed

- The **Dashboard** did not work.

## [1.5.0] - 2025-10-28

### Added

- `--clear-iptables` removes b4's iptables rules.
- IPv6 support, with `--ipv4` (on by default) and `--ipv6` (off by default) to choose the protocol versions.

### Changed

- Domains from geodata files are handled better.

[unreleased]: https://github.com/DanielLavrushin/b4/compare/v1.90.0...HEAD
[1.90.0]: https://github.com/DanielLavrushin/b4/compare/v1.89.1...v1.90.0
[1.89.1]: https://github.com/DanielLavrushin/b4/compare/v1.89.0...v1.89.1
[1.89.0]: https://github.com/DanielLavrushin/b4/compare/v1.88.2...v1.89.0
[1.88.2]: https://github.com/DanielLavrushin/b4/compare/v1.88.1...v1.88.2
[1.88.1]: https://github.com/DanielLavrushin/b4/compare/v1.88.0...v1.88.1
[1.88.0]: https://github.com/DanielLavrushin/b4/compare/v1.87.0...v1.88.0
[1.87.0]: https://github.com/DanielLavrushin/b4/compare/v1.86.0...v1.87.0
[1.86.0]: https://github.com/DanielLavrushin/b4/compare/v1.85.1...v1.86.0
[1.85.1]: https://github.com/DanielLavrushin/b4/compare/v1.85.0...v1.85.1
[1.85.0]: https://github.com/DanielLavrushin/b4/compare/v1.84.0...v1.85.0
[1.84.0]: https://github.com/DanielLavrushin/b4/compare/v1.83.1...v1.84.0
[1.83.1]: https://github.com/DanielLavrushin/b4/compare/v1.83.0...v1.83.1
[1.83.0]: https://github.com/DanielLavrushin/b4/compare/v1.82.3...v1.83.0
[1.82.3]: https://github.com/DanielLavrushin/b4/compare/v1.82.2...v1.82.3
[1.82.2]: https://github.com/DanielLavrushin/b4/compare/v1.82.1...v1.82.2
[1.82.1]: https://github.com/DanielLavrushin/b4/compare/v1.82.0...v1.82.1
[1.82.0]: https://github.com/DanielLavrushin/b4/compare/v1.81.1...v1.82.0
[1.81.1]: https://github.com/DanielLavrushin/b4/compare/v1.81.0...v1.81.1
[1.81.0]: https://github.com/DanielLavrushin/b4/compare/v1.80.4...v1.81.0
[1.80.4]: https://github.com/DanielLavrushin/b4/compare/v1.80.3...v1.80.4
[1.80.3]: https://github.com/DanielLavrushin/b4/compare/v1.80.2...v1.80.3
[1.80.2]: https://github.com/DanielLavrushin/b4/compare/v1.80.1...v1.80.2
[1.79.0]: https://github.com/DanielLavrushin/b4/compare/v1.78.0...v1.79.0
[1.78.0]: https://github.com/DanielLavrushin/b4/compare/v1.77.0...v1.78.0
[1.77.0]: https://github.com/DanielLavrushin/b4/compare/v1.76.2...v1.77.0
[1.76.2]: https://github.com/DanielLavrushin/b4/compare/v1.76.1...v1.76.2
[1.76.1]: https://github.com/DanielLavrushin/b4/compare/v1.76.0...v1.76.1
[1.76.0]: https://github.com/DanielLavrushin/b4/compare/v1.75.0...v1.76.0
[1.75.0]: https://github.com/DanielLavrushin/b4/compare/v1.74.2...v1.75.0
[1.74.2]: https://github.com/DanielLavrushin/b4/compare/v1.74.1...v1.74.2
[1.74.1]: https://github.com/DanielLavrushin/b4/compare/v1.74.0...v1.74.1
[1.73.0]: https://github.com/DanielLavrushin/b4/compare/v1.72.0...v1.73.0
[1.72.0]: https://github.com/DanielLavrushin/b4/compare/v1.71.1...v1.72.0
[1.71.1]: https://github.com/DanielLavrushin/b4/compare/v1.71.0...v1.71.1
[1.71.0]: https://github.com/DanielLavrushin/b4/compare/v1.70.1...v1.71.0
[1.70.1]: https://github.com/DanielLavrushin/b4/compare/v1.70.0...v1.70.1
[1.70.0]: https://github.com/DanielLavrushin/b4/compare/v1.69.1...v1.70.0
[1.69.1]: https://github.com/DanielLavrushin/b4/compare/v1.69.0...v1.69.1
[1.69.0]: https://github.com/DanielLavrushin/b4/compare/v1.68.0...v1.69.0
[1.68.0]: https://github.com/DanielLavrushin/b4/compare/v1.67.2...v1.68.0
[1.67.2]: https://github.com/DanielLavrushin/b4/compare/v1.67.1...v1.67.2
[1.67.1]: https://github.com/DanielLavrushin/b4/compare/v1.67.0...v1.67.1
[1.67.0]: https://github.com/DanielLavrushin/b4/compare/v1.66.1...v1.67.0
[1.66.0]: https://github.com/DanielLavrushin/b4/compare/v1.65.0...v1.66.0
[1.65.0]: https://github.com/DanielLavrushin/b4/compare/v1.64.0...v1.65.0
[1.64.0]: https://github.com/DanielLavrushin/b4/compare/v1.63.0...v1.64.0
[1.63.0]: https://github.com/DanielLavrushin/b4/compare/v1.62.1...v1.63.0
[1.62.1]: https://github.com/DanielLavrushin/b4/compare/v1.62.0...v1.62.1
[1.61.4]: https://github.com/DanielLavrushin/b4/compare/v1.61.3...v1.61.4
[1.61.3]: https://github.com/DanielLavrushin/b4/compare/v1.61.2...v1.61.3
[1.60.1]: https://github.com/DanielLavrushin/b4/compare/v1.60.0...v1.60.1
[1.50.1]: https://github.com/DanielLavrushin/b4/compare/v1.50.0...v1.50.1
[1.50.0]: https://github.com/DanielLavrushin/b4/compare/v1.49.1...v1.50.0
[1.49.1]: https://github.com/DanielLavrushin/b4/compare/v1.49.0...v1.49.1
[1.49.0]: https://github.com/DanielLavrushin/b4/compare/v1.48.1...v1.49.0
[1.48.1]: https://github.com/DanielLavrushin/b4/compare/v1.48.0...v1.48.1
[1.48.0]: https://github.com/DanielLavrushin/b4/compare/v1.47.2...v1.48.0
[1.47.2]: https://github.com/DanielLavrushin/b4/compare/v1.47.1...v1.47.2
[1.47.0]: https://github.com/DanielLavrushin/b4/compare/v1.46.6...v1.47.0
[1.46.6]: https://github.com/DanielLavrushin/b4/compare/v1.46.5...v1.46.6
[1.46.5]: https://github.com/DanielLavrushin/b4/compare/v1.46.4...v1.46.5
[1.46.0]: https://github.com/DanielLavrushin/b4/compare/v1.45.1...v1.46.0
[1.45.1]: https://github.com/DanielLavrushin/b4/compare/v1.45.0...v1.45.1
[1.45.0]: https://github.com/DanielLavrushin/b4/compare/v1.44.1...v1.45.0
[1.44.1]: https://github.com/DanielLavrushin/b4/compare/v1.44.0...v1.44.1
[1.43.0]: https://github.com/DanielLavrushin/b4/compare/v1.42.1...v1.43.0
[1.42.1]: https://github.com/DanielLavrushin/b4/compare/v1.42.0...v1.42.1
[1.42.0]: https://github.com/DanielLavrushin/b4/compare/v1.41.0...v1.42.0
[1.41.0]: https://github.com/DanielLavrushin/b4/compare/v1.40.1...v1.41.0
[1.40.0]: https://github.com/DanielLavrushin/b4/compare/v1.39.2...v1.40.0
[1.39.1]: https://github.com/DanielLavrushin/b4/compare/v1.39.0...v1.39.1
[1.38.0]: https://github.com/DanielLavrushin/b4/compare/v1.37.0...v1.38.0
[1.37.0]: https://github.com/DanielLavrushin/b4/compare/v1.36.0...v1.37.0
[1.36.0]: https://github.com/DanielLavrushin/b4/compare/v1.35.2...v1.36.0
[1.35.2]: https://github.com/DanielLavrushin/b4/compare/v1.35.0...v1.35.2
[1.35.0]: https://github.com/DanielLavrushin/b4/compare/v1.34.0...v1.35.0
[1.34.0]: https://github.com/DanielLavrushin/b4/compare/v1.33.2...v1.34.0
[1.32.0]: https://github.com/DanielLavrushin/b4/compare/v1.31.2...v1.32.0
[1.31.2]: https://github.com/DanielLavrushin/b4/compare/v1.31.1...v1.31.2
[1.31.0]: https://github.com/DanielLavrushin/b4/compare/v1.30.1...v1.31.0
[1.30.1]: https://github.com/DanielLavrushin/b4/compare/v1.30.0...v1.30.1
[1.30.0]: https://github.com/DanielLavrushin/b4/compare/v1.29.1...v1.30.0
[1.29.1]: https://github.com/DanielLavrushin/b4/compare/v1.29.0...v1.29.1
[1.28.1]: https://github.com/DanielLavrushin/b4/compare/v1.28.0...v1.28.1
[1.27.2]: https://github.com/DanielLavrushin/b4/compare/v1.27.1...v1.27.2
[1.27.1]: https://github.com/DanielLavrushin/b4/compare/v1.27.0...v1.27.1
[1.27.0]: https://github.com/DanielLavrushin/b4/compare/v1.26.3...v1.27.0
[1.26.3]: https://github.com/DanielLavrushin/b4/compare/v1.26.2...v1.26.3
[1.26.2]: https://github.com/DanielLavrushin/b4/compare/v1.26.1...v1.26.2
[1.26.1]: https://github.com/DanielLavrushin/b4/compare/v1.26.0...v1.26.1
[1.25.4]: https://github.com/DanielLavrushin/b4/compare/v1.25.3...v1.25.4
[1.25.3]: https://github.com/DanielLavrushin/b4/compare/v1.25.2...v1.25.3
[1.25.2]: https://github.com/DanielLavrushin/b4/compare/v1.25.1...v1.25.2
[1.25.0]: https://github.com/DanielLavrushin/b4/compare/v1.24.0...v1.25.0
[1.24.0]: https://github.com/DanielLavrushin/b4/compare/v1.23.1...v1.24.0
[1.23.1]: https://github.com/DanielLavrushin/b4/compare/v1.23.0...v1.23.1
[1.23.0]: https://github.com/DanielLavrushin/b4/compare/v1.22.1...v1.23.0
[1.22.1]: https://github.com/DanielLavrushin/b4/compare/v1.22.0...v1.22.1
[1.22.0]: https://github.com/DanielLavrushin/b4/compare/v1.21.1...v1.22.0
[1.21.1]: https://github.com/DanielLavrushin/b4/compare/v1.21.0...v1.21.1
[1.21.0]: https://github.com/DanielLavrushin/b4/compare/v1.20.3...v1.21.0
[1.20.3]: https://github.com/DanielLavrushin/b4/compare/v1.20.2...v1.20.3
[1.20.1]: https://github.com/DanielLavrushin/b4/compare/v1.20.0...v1.20.1
[1.20.0]: https://github.com/DanielLavrushin/b4/compare/v1.19.1...v1.20.0
[1.19.1]: https://github.com/DanielLavrushin/b4/compare/v1.19.0...v1.19.1
[1.19.0]: https://github.com/DanielLavrushin/b4/compare/v1.18.5...v1.19.0
[1.18.5]: https://github.com/DanielLavrushin/b4/compare/v1.18.4...v1.18.5
[1.18.4]: https://github.com/DanielLavrushin/b4/compare/v1.18.3...v1.18.4
[1.18.3]: https://github.com/DanielLavrushin/b4/compare/v1.18.2...v1.18.3
[1.18.2]: https://github.com/DanielLavrushin/b4/compare/v1.18.1...v1.18.2
[1.18.1]: https://github.com/DanielLavrushin/b4/compare/v1.18.0...v1.18.1
[1.18.0]: https://github.com/DanielLavrushin/b4/compare/v1.17.1...v1.18.0
[1.17.1]: https://github.com/DanielLavrushin/b4/compare/v1.17.0...v1.17.1
[1.16.1]: https://github.com/DanielLavrushin/b4/compare/v1.16.0...v1.16.1
[1.16.0]: https://github.com/DanielLavrushin/b4/compare/v1.15.0...v1.16.0
[1.15.0]: https://github.com/DanielLavrushin/b4/compare/v1.14.0...v1.15.0
[1.14.0]: https://github.com/DanielLavrushin/b4/compare/v1.13.0...v1.14.0
[1.13.0]: https://github.com/DanielLavrushin/b4/compare/v1.12.0...v1.13.0
[1.12.0]: https://github.com/DanielLavrushin/b4/compare/v1.11.1...v1.12.0
[1.11.0]: https://github.com/DanielLavrushin/b4/compare/v1.10.1...v1.11.0
[1.10.1]: https://github.com/DanielLavrushin/b4/compare/v1.10.0...v1.10.1
[1.10.0]: https://github.com/DanielLavrushin/b4/compare/v1.9.2...v1.10.0
[1.9.2]: https://github.com/DanielLavrushin/b4/compare/v1.9.1...v1.9.2
[1.9.1]: https://github.com/DanielLavrushin/b4/compare/v1.9.0...v1.9.1
[1.9.0]: https://github.com/DanielLavrushin/b4/compare/v1.8.0...v1.9.0
[1.8.0]: https://github.com/DanielLavrushin/b4/compare/v1.7.0...v1.8.0
[1.7.0]: https://github.com/DanielLavrushin/b4/compare/v1.6.0...v1.7.0
[1.6.0]: https://github.com/DanielLavrushin/b4/compare/v1.5.0...v1.6.0
[1.5.0]: https://github.com/DanielLavrushin/b4/compare/v1.4.3...v1.5.0
