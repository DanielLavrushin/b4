# B4 - Bye Bye Big Bro

## [1.80.0] - 2026-08-25

- FIXED: **On nftables, a set routed to an output interface lost the packets b4 crafts for it as soon as any set used proxy mode** - the proxy set put its own pair of mark returns at the top of the shared output chain, and that chain is a hook, so a return there ended it for every set hanging below. The fakes and split segments b4 generates carry the queue mark, matched the first return, and left by the router's ordinary uplink while the connection they belonged to went out the set's interface.
- FIXED: **Picking network interfaces to monitor could switch the bypass off for the whole network with nothing saying so** - the setting matches the interface a packet leaves by, which a VPN client or a transparent proxy moves without touching the list, so a selection made before that quietly stopped matching and every packet was queued to b4 and then accepted unchanged. Settings, Core carries a warning whenever an interface outside the selection is carrying outgoing traffic, naming it and its share, and the same per-interface counts are in the diagnostics report. A new [Guides](https://docs.b4core.app/docs/guides) section covers the three interface settings and running b4 alongside Xray.
- FIXED: **A set routed to a TUN interface a proxy reads took the whole router's memory with it, usually within the hour** - the set's rules marked the connections the router opens itself, and a proxy with a TUN inbound does not forward a packet, it opens its own connection to the same destination from the same box. That destination is in the set, so b4 marked the new connection and routed it straight back into the TUN the proxy was reading. Every turn carried a new source port, so nothing in the kernel saw it as a repeat, and every turn cost the proxy another session and another socket. b4 now recognises a TUN or TAP device the kernel opened for a userspace program and leaves the router's own traffic on the normal route there; a new **Router's own traffic** setting on the routing tab overrides it either way, and choosing to route it into such an interface anyway is capped at 200 new router-originated connections per second so a loop cannot run away.
- FIXED: **b4 could claim a routing table that another VPN client was already using, and flush it on cleanup** - the table number came from the output interface's name with nothing checking whether the number was free, and the range it picks from is the one Asuswrt-Merlin gives its OpenVPN and WireGuard clients: an output interface named `tun0` landed on the table Merlin uses for WireGuard client 2. b4 now looks at what a table already holds and moves to another number, saying which one it took in the log.
- ADDED: **A kill switch for a set's output interface** - when the interface went away the kernel removed the route b4 had put in the set's table, the mark rule found the table empty, and the lookup fell through to the main table, so the set left through the ordinary uplink with the router's real address while its rules still read as correct. Turned on, b4 keeps a blackhole route in the set's table at a worse metric than the real one, so the set stops instead of leaking, and starts again by itself when the interface returns. `Sets > Routing`.
- CHANGED: **A set routed into a tunnel no longer has its packets faked, fragmented and desynced** - the work was done on the inner packet, which is wrapped or terminated on the router before anything on the network sees it, so it bought nothing and cost CPU on every connection, and the fake packets were delivered to the tunnel program's own stack one hop away with no room to expire. SYN health checks, dead-IP escalation, IP block detection and TCP duplication stop with it, for the same reason. Applies to TUN, TAP and WireGuard output interfaces, matching what proxy mode already did; a set keeps its bypass strategy when it routes to a plain second uplink and when its output interface is present but down, since neither can carry the packet. Such connections are tagged `routed-><iface>` in the connection log.
- FIXED: **Telegram kept reporting the MTProto proxy as incorrectly configured and switching it off, and the 1.78.0 fix was aimed at the wrong cause** - the dialog comes from a single four-byte -444 relayed back from a data center, not from a slow dial, and b4 could send an ordinary session to a data center's media edge `kwsN-1`, which answers a non-media session with exactly that. Both names lead to the same address, so that fallback routed around nothing, and a -444 ends the connection instead of reaching the client.
- FIXED: **A set bound to particular devices sent the packets b4 crafts for it out of the router's ordinary uplink** - the fakes and fragments b4 generates come from the router itself, so a filter written to match a LAN client can never match them, and the chain that hands them the set's routing mark was dropped for such a set altogether. Its fakes travelled one path while the connection they belonged to travelled another.
- FIXED: **Sets routed through a proxy took effect in the reverse of the order they are listed in** - each one placed its firewall jump ahead of the set before it, so with four proxy sets configured the last in the list claimed every address two of them targeted, and the pass that arranges the other routing modes by priority skipped proxy sets altogether. Editing a single set moved it to the front on its own, and traffic the router itself originates followed the same reversed order. [#324](https://github.com/DanielLavrushin/b4/issues/324)
- ADDED: **An MCP server that can act on b4 rather than only describe it** - the AI could read status and change one setting at a time, so anything a user asked for came back as instructions to carry out by hand. It gains per-setting facts, geosite and geoip search, target and set editing, the last update's transcript, and behind a separate **Allow active probes** permission, testing whether a domain loads and running Discovery. `Settings > Integrations`.
- ADDED: **A log line for every MCP tool call and every request the endpoint turns away** - a server able to rewrite strategy sets and make the router fetch a site left no trace, and a client guessing at the access token was refused silently.
- FIXED: **A Discovery run's result became unreachable the moment the run ended** - only the copy held in memory was consulted, and that is dropped 30 seconds after a run finishes, though the web interface had always applied the same result from saved history. A strategy named while a run was still testing also read as settled, and stopping a run early threw away what it had found.
- FIXED: **The Discovery history file grew by roughly half a megabyte per domain tested** - every strategy a run tried was saved with a full copy of the set b4 built to test it, so the hundred entries the file keeps could reach 60 MB on a router whose flash holds far less.
- FIXED: **The watchdog judged every watched domain on a fetch that skipped b4's own bypass** - its checks carried the mark b4 puts on traffic it has already handled, so the verdict described the unprotected path rather than the set meant to be protecting that domain.
- FIXED: **A downloaded backup could contain nothing but an empty folder** - b4 skipped every file marked as a program, and on drives prepared from Windows the settings files carry that mark even though they are not programs, so the backup held none of the settings it was meant to save while the download still reported success. A backup that would hold no files is refused with the reason.
- FIXED: **The settings folder was open for any account on the router to write to** - the settings file in it holds the web interface login and the MCP access token yet was created readable and writable by everyone, the copies kept before each upgrade the same, and restoring a backup followed folder shortcuts left in that folder, so an entry in the archive could land anywhere on the router. b4 restricts the folder and those files at startup, and a restore refuses to write through a shortcut.
- ADDED: **Documentation for the Telegram WEB proxy, which shipped in 1.79.0 with nothing written about it** - its prerequisites cannot be worked out from the interface: the MTProto proxy has to be running, the relay needs a hostname of its own with publicly trusted TLS, and the link Telegram Desktop accepts carries a different form of the secret than the settings show. It joins a [Telegram](https://docs.b4core.app/docs/mtproto) section that covers the three modes separately, replacing one page that described two of them, quoted log lines b4 does not write, and gave a worked set example that fails validation.

## [1.79.0] - 2026-08-18

- ADDED: **A WEB carrier for the MTProto proxy, which Telegram Desktop 7.1.1 reaches over ordinary HTTPS** - a client on a network that blocks MTProxy and fake-TLS outright had no way in, because every transport b4 offered still opened a recognisable MTProto socket from the client. b4 answers a relay hostname with a bridge page the client loads in a hidden WebView, multiplexes the MTProxy stream through a same-origin WebSocket, and hands each logical stream to the same secrets, DC pool and upstream routing as a plain connection; requests without a valid bridge token get an ordinary page. `Settings > MTProto Proxy`.
- FIXED: **A set's DNS redirect answered SERVFAIL for every domain it covered as soon as its DoH server went unreachable** - the redirect had no fallback of any kind, so a resolver the censor happened to be blocking took the whole set offline while the bypass strategy underneath it was working perfectly well. b4 replays the last good answer or hands the query back to the resolver the client was already using, and a Fail closed switch under `Sets > Routing > DNS Redirect` keeps the old behaviour.
- FIXED: **Discovery offered a bypass strategy for domains that were never blocked** - nothing in a run was compared against its own no-bypass fetch, so one blocked domain put a strategy on the whole batch, and the improvement percentage measured the site against yandex.ru, the reference domain b4 uses to gauge the line. Each domain is judged against its own unmodified fetch and reported on its own.
- FIXED: **A Discovery strategy was declared working on one page fetch that may not have succeeded at all** - only HTTP 451 counted as a failure, so a 502 longer than a kilobyte scored as a success, the probe negotiated HTTP/1.1 with no ALPN while every browser asks for HTTP/2, and a redirected page was scored over the address of the domain that redirected it. The winner is fetched three more times in the shape saving it as a set gives it, and one that fails any of those tries gives way to the next candidate.
- FIXED: **On a router with working IPv6, Discovery stopped after about twenty tests and reported that no bypass was needed** - its test connections were free to use either version of the protocol while b4 acts on IPv4 alone, so the opening test succeeded over a route nothing was interfering with.
- FIXED: **Discovery built sets around a DNS redirect and geo categories the router had no use for** - the redirect was stamped onto every set of a run as soon as one domain looked poisoned, though the probes never used it themselves, and geosite and geoip categories were carried in whatever was installed, so a result for a CDN domain could not be added at all without a geo database.
- FIXED: **Restarting or stopping the service from the interface went wrong on systemd** - b4 waited on a systemctl that systemd stops together with the service it is restarting, so every restart left `Restart command failed: signal: terminated` in the error log, and an unfinished browser connection could hold a stop open for the full ten seconds until the kill landed while the firewall cleanup was still running.
- FIXED: **An update could stop the service loading, or leave a second b4 running outside the service manager** - the updater recognised every init system except systemd, so a unit written by anything other than b4's installer was rebuilt as a SysV init script, and it waited one second for the service to come back before starting b4 by hand, racing the manager's own restart.
- ADDED: **A source allowlist for the SOCKS5 proxy, holding the IP addresses and CIDR ranges permitted to connect** - the only control over who reached the proxy was a username and password, which Chrome and Chromium cannot send at all, so a LAN client on either browser meant leaving the proxy open to everything that could reach its port. `Settings > SOCKS5 Proxy`.
- FIXED: **The SOCKS5 proxy served clients it was meant to keep out** - a username without a password, or a password without a username, switched authentication off rather than failing, and the UDP relay took an address written into the associate request over the source of the control connection, so an association could be pointed at another host on the network.
- FIXED: **Turning the SOCKS5 proxy off left the connections it was already carrying running** - only the listener closed, so established tunnels kept relaying until the client dropped them by itself, and changing the port or the bind address behaved the same way.
- ADDED: **A Block QUIC switch on a set's UDP tab, and a UDP action that leaves matched packets alone** - pushing a browser off QUIC and back onto TCP took two settings in two places with nothing saying they belonged together, and every action a set could pick altered the traffic, so a set matching QUIC only to learn a site's addresses had to fake and fragment it anyway. `Sets > UDP`.
- CHANGED: **Two of the three QUIC matching values did the same thing** - one read as turning QUIC matching off and the other as merely parsing, while both matched the set by the server name in the ClientHello. They are one value, By SNI, and stored sets are converted on the first start.
- FIXED: **An IPv6 packet carrying extension headers could crash b4** - a header claiming more bytes than the packet held sent the read past its end, stopping the service for every device on the router.
- ADDED: **Force IPv4 for matched domains, which drops the IPv6 records from answers for names a set matched** - b4 processes IPv4 only until IPv6 support is turned on, so a dual-stack site a set targets was reached over IPv6 and bypassed the set entirely. `Settings > Core > DNS`.
- FIXED: **A DNS answer that b4 rewrote never cleared the failure count for that name** - the answer was judged before the rewrite rather than after, so a site that had recovered was moved to its backup set on the next single failure.
- ADDED: **An egress IP for a set, which replaces the source address of its traffic** - a set could only be steered by naming an interface or an upstream SOCKS5 proxy, which a router that picks its path by source address cannot use. `Sets > Routing`.
- FIXED: **A device added by hand was ignored everywhere devices are matched** - such a device has no MAC address on the network, so a set bound to it stopped taking its route, block sets and the all-devices-except-selected option were disarmed, the filter under Settings, Device Filtering sat inert while routing kept applying to every device, and an MSS clamp on it clamped nothing while pulling every other forwarded connection down to the smallest configured size. Such devices are matched by the IP address entered for them.
- FIXED: **A routing set limited to source devices diverted traffic it was never meant to touch** - the rules on the router's own outgoing traffic carried no device filter, which paired with Match any IP address pulled its whole outbound path into the tunnel, and a set matched only by TCP or UDP port checked the port before any source at all while claiming its traffic was being diverted, which switched off every DPI countermeasure.
- FIXED: **A routing set with no domain or IP target was built in full and quietly matched nothing** - every routing rule matches a destination address, so a set with none could never match a packet, and nothing above trace level said so.
- CHANGED: **Three unrelated services shared the API settings tab through a two-column grid and three different ways of showing a credential** - the IPinfo token was a plain field, the AI key a panel of chips and buttons, the MCP token a full-width field printing the secret in clear, and each service's enable switch sat among the fields it governed. The tab is called Integrations and gives every service one card whose header carries its on/off switch, one masked credential field with reveal, copy and generate, and for MCP a client configuration block that copies the endpoint and token as a ready-made JSON snippet.
- FIXED: **b4 took the router down after a few minutes on a busy line, leaving `runtime: program exceeds 10000-thread limit` in the log** - every matched packet started its own injection task with no ceiling, and those tasks wrote to a raw socket that had no send timeout, so an outgoing interface that stopped draining held one kernel thread per task until the Go runtime refused to create more.
- ADDED: **The operating system thread count on the dashboard Runtime card** - the card reported goroutines but nothing about OS threads, which is the number the Go runtime stops the service over, so a runaway stayed invisible until the crash. b4 writes a goroutine dump to the log directory once the count passes half the limit.

## [1.78.0] - 2026-08-17

- ADDED: **An MCP server at /api/mcp** - b4 spoke no protocol an external AI app could connect to. Read-only tools report status, configuration, strategy sets, domain coverage, recent connections, the log tail, metrics and diagnostics, and a resource per documented setting describes what that setting does. The model runs in the client app; b4 reaches no AI provider and needs no API key. Off by default, and authenticated by its own token, because a web login token is discarded on every restart. A second switch, also off by default, permits writes to any setting inside a set, to the MTProto and SOCKS5 subsystems and to the log level; credentials, the web server, the capture engine, the firewall backend and the packet marks are refused, and the last change can be reverted. [MCP server](https://docs.b4core.app/docs/settings/mcp).
- FIXED: **Traffic that merely used port 53 was read as a name lookup** - a VPN or proxy tunnel hidden on that port had its encrypted contents taken for a domain name and matched against sets.
- FIXED: **The server name read out of a QUIC connection was accepted whatever it contained** - any bytes of any length passed for a hostname, unlike the same name read from an ordinary HTTPS connection, which was checked.
- FIXED: **Telegram reported the MTProto proxy as incorrectly configured and switched it off** - a session was accepted before b4 had reached a data center, and one blocked route was given longer than the client was willing to wait.
- FIXED: **Data center 203 had no connection ready in advance** - spare connections were kept for two data centers only, so every session on the others started by opening one from scratch.
- FIXED: **A route that answered nothing was set aside only when it replied with a rate limit** - a route that went silent cost every session after it the same full wait.
- FIXED: **A name with several addresses was abandoned after the first one went unanswered** - the attempt stalled on that address while another address of the same name answered in under a second.
- FIXED: **The API reference on the documentation site rejected the b4 username and password** - the credentials were sent to the service unchanged instead of being exchanged for a login token, so the Authorize window accepted a pasted token alone.
- FIXED: **Turning the SOCKS5 proxy on or off, or moving it to another port, changed nothing until b4 was restarted** - the setting was saved and the save was reported as successful, but the listener was only ever started once at boot, so a proxy switched off in the interface kept accepting connections and one switched on never opened its port.

## [1.77.0] - 2026-08-15

- ADDED: **A Customize control on the dashboard: panels are reordered by dragging, widened or narrowed by dragging the right edge across a twelve column grid, and hidden with the eye, remembered per browser** - the order and the widths were fixed in the code, so a panel worth keeping an eye on, such as the MTProto proxy and the devices using it, sat below a domain list hundreds of rows long, there was no way to move it up, give it more room or drop a panel that never gets read, and a short panel beside a tall one left everything after it waiting below the taller one.
- CHANGED: **The dashboard layout is kept in b4's own configuration rather than in the browser** - the order, the hidden panels and the widths were held in browser storage alone, so a layout arranged on one machine was absent from every other one, and a new browser, a private window or a cleared site cache put the dashboard back to its defaults. The browser copy is still written and is read when b4 cannot be reached, so the page keeps its arrangement while the service is restarting.
- CHANGED: **The runtime, live signal and blackhole panels lay themselves out by their own width rather than the width of the window** - each read the browser's breakpoints, so a panel given less than the full width of the page kept a layout sized for the whole screen and had its contents cut off at the panel edge.
- FIXED: **The packets b4 makes itself left a router by whichever uplink the main routing table picked, not by the interface the set they belong to is routed through** - fakes, split segments and desync packets are sent by b4 rather than forwarded, so they carried only b4's own mark and the set's routing never applied to them. On a router balancing two WANs the connection opened over one and continued over the other, from a different public address, and the destination reset it; with a set routed through a tunnel those packets, and the server name inside them, went out over the plain uplink instead.
- CHANGED: **A set's hand-picked firewall mark is refused, and one is assigned instead, when it carries every bit of the mark b4 puts on its own packets** - the two became indistinguishable, and the set's traffic was read as traffic b4 had injected.
- ADDED: **The enabled sets in the trace header, with domain lists counted rather than listed and proxy credentials left out** - a trace carried the system side of a report and nothing about what b4 was told to match, so a set that misbehaves had to be read without its targets, strategy or routing at hand.
- FIXED: **A set routed through a proxy leaked two sockets and two pipes for every connection whose far side died without closing** - the count reached the router's open-file limit within about an hour, and b4 stopped working until it was restarted.
- CHANGED: **A proxied connection carrying no data in either direction is dropped after an hour, and after five minutes once one side has finished sending** - a relay had no time limit at all once the proxy handshake was done.
- CHANGED: **A set that cannot reach its proxy reports it in the log and in the diagnostics bundle** - the failed connection was recorded at trace level only, so a proxy refusing every attempt left nothing behind at the default log level. Traffic the set matched was accepted, held for the length of the connection timeout and then dropped, with nothing on record to say why.
- FIXED: **A set whose MSS limit is scoped to IPv4 addresses no longer caps the IPv6 traffic of the devices it names** - the limit was written once per address family, and the family the set holds no addresses for fell back to matching the device alone, so every IPv6 HTTPS connection from that device was capped at the set's value regardless of where it was headed.
- FIXED: **A site switched to a backup set still had its name looked up by the set that was failing** - the backup's pinned addresses, DoH server and forwarding target were never consulted for it, so a domain that only the backup could resolve stayed unresolvable however the escalation was set up.
- ADDED: **An unusable name lookup switches the site to the backup set** - NXDOMAIN, SERVFAIL and answers that carry no address count towards the switch, and the backup set answers the lookup that tripped it. Escalation only ever reacted to something that happens after a connection is already open, which a site with no address on record never reaches.
- ADDED: **A destination that answers no connection attempt switches the site to the backup set** - b4 already spotted the address going unanswered and confirmed it by probing from the router itself, but that finding went nowhere except into cutting the device's attempt short. The most common way a site fails, its address dropped outright, had no path to the backup set.
- FIXED: **The backup set only took over once a TLS handshake had been read out of the traffic** - a connection that never gets that far, and the opening packet of a connection to a site already switched over, were both handled by the set that was failing, so the backup set's SYN-stage settings never ran.
- FIXED: **How readily a site switches over was read from IP block detection, a separate feature that is off by default** - unanswered TLS handshakes, forged resets and unusable name lookups each got a control of their own on the Escalation tab. The single one on offer before governed forged resets only, so moving it changed nothing about the trigger that fires most.
- FIXED: **Turning a backup set off destroyed the link to it** - the link was erased on the next save and the set could not be picked again until it was enabled, so a set switched off for a minute had to be wired back by hand.
- FIXED: **A set routed through a proxy could not escalate any further** - the chain stopped at the first proxy hop, though it is built to run up to eight.
- FIXED: **Only the single address a connection was headed for was handed to the backup set's routing** - a site behind several addresses had to fail once per address, and the entry aged out of the firewall while the switch was still in force, which put the traffic back on the failing path without a word.
- FIXED: **A switch-over ignored which devices the backup set targets** - one device's failure moved every device on the network onto the backup set.
- CHANGED: **Editing a set drops only the switch-overs that the edit invalidates** - every switch-over in force was discarded on each save, including those for sites the edit did not touch.
- FIXED: **The chain-length warning repeated for every packet** - once a site had been through all eight hops, each retransmission logged it again.

## [1.76.3] - 2026-08-14

- FIXED: **Name lookups from devices on the network went unanswered in TUN capture mode** - the answer b4 built was addressed to the router's own uplink instead of to the device that asked, because b4 read the sender out of the packet after the router had already replaced it on the way out. The device was left waiting on a lookup b4 had taken out of the network and answered somewhere nothing was listening.
- FIXED: **A name lookup b4 could not trace back to a device was destroyed rather than left alone** - the sender was gone from the packet and nothing checked whether it had been recovered before the query was taken out of the network, so the device retried into the same hole for as long as the set stayed enabled.
- FIXED: **A blocked name, a pinned address and a curated answer never reached the device they were meant for, in TUN capture mode** - all three are delivered as a packet b4 builds itself, and all three carried the router's address as the destination.
- FIXED: **Every name lookup was recorded against the router rather than against the device that made it** - the source column on the Traffic page read as the uplink address for all of them, and a set narrowed to particular devices had nothing to match a lookup on.
- CHANGED: **The number of name lookups b4 resolves at once is capped, and the rest go to the device's own resolver untouched** - each intercepted lookup started a five-second resolver of its own with nothing limiting how many could exist at the same time, so one slow or unreachable DoH server turned an ordinary browsing session into thousands of waiting lookups. A set that matches a large geosite category and also redirects DNS put every name on the network through that path.
- CHANGED: **A lookup the router itself made is remembered for five minutes instead of two seconds** - it was recorded as a failed lookup and re-read from the kernel's connection table, in full, every two seconds for as long as the flow lived.
- FIXED: **The addresses b4 read out of a packet pointed into the memory the next packet is read into, in TUN capture mode** - a set routed through a proxy or an interface writes those addresses to the firewall from a background thread, and by the time it ran the packet they came from could already have been replaced by an unrelated one.

## [1.76.2] - 2026-08-12

- FIXED: **Photos, videos and stickers stopped loading in 1.76.0 and 1.76.1** - the data center Telegram serves media from was addressed under another one's name, so every media session was carried somewhere that could not serve it.
- FIXED: **Two of Telegram's addresses were carried to the wrong data center** - they answer as data center 4, but sit inside a block b4 read as data center 2.
- FIXED: **Most of Telegram's addresses matched no data center, which left those connections almost no routes to try** - b4 knew five address blocks, and nearly half the connections in a capture from a censored network fell outside all of them.
- CHANGED: **The address block b4 reads as data center 4 was narrowed to a quarter of its size** - the 768 addresses dropped from it replied to nothing.
- CHANGED: **A Cloudflare Worker of your own is the last WebSocket route tried rather than the first** - Cloudflare takes a worker back part-way through a session, well after the connection is made, so it passed every check while video failed on it.
- FIXED: **A route that took a request and returned nothing was handed the next connection anyway** - only a route that went quiet mid-session, with a request still outstanding, was ranked down.
- CHANGED: **The Cloudflare Worker script in the documentation holds its relay open longer** - Cloudflare could cut off the part carrying data back from Telegram the moment the connection was accepted. Set the worker's compatibility date to `2026-04-07` or later.
- FIXED: **`tgprobe` never tested the shared Cloudflare pool** - it started from a blank configuration, so every on/off setting in it read as off.
- CHANGED: **The Cloudflare Worker is asked for a data center address without a port** - data center 203 does not answer on 5222, one of the three ports Telegram serves the same endpoint on, so passing the port on killed media connections. The script in the [Telegram documentation](https://docs.b4core.app/docs/mtproto) is again the tg-ws-proxy script unchanged, and a Worker deployed from either page needs nothing done to it.

## [1.76.1] - 2026-08-11

- FIXED: **Telegram never got past loading, with an empty chat list, in the WebSocket bridge routing mode** - the bridge only ever went through a Cloudflare Worker, never Telegram's own route, the one the MTProto proxy had been taking all along.
- FIXED: **A Cloudflare Worker that had stopped passing traffic kept being picked for new Telegram connections** - a session on a dead one was held for five minutes before it was given up.
- FIXED: **The WebSocket bridge sent traffic to a different data center address than the device had asked for** - it kept one address per data center, while Telegram hands out many.
- FIXED: **Connections b4 opens for itself went out with none of its own bypass applied** - a fail-open connection, made when a destination is already in trouble, was the one it left most exposed.
- FIXED: **b4 put its routing rules back only once they had disappeared completely** - part of a rule set going missing counted as healthy.

## [1.76.0] - 2026-08-10

- FIXED: **Every name lookup took five seconds once a set was routed through a proxy or an interface** - each answer for such a set was written into the firewall from the same thread that reads packets from the kernel, one `ipset` process per address, so nothing was read for as long as those processes took. Packets arriving meanwhile were dropped by the kernel without a word, and a lookup for a name b4 has no interest in crosses that queue several times, so it was the one most likely to lose a packet and wait out the resolver's five-second retry.
- CHANGED: **The addresses from a DNS answer are written to the firewall in the background, in one batch** - the same addresses were re-added on every answer for the name, at one process per address, which on a router is most of a second of work repeated for traffic that changed nothing.
- FIXED: **A DNS request sent by the router itself was handed to b4 twice** - the rule that captures it sat in a chain reached from two places, so every such request paid the round trip to b4 and back a second time for no purpose.
- CHANGED: **A burst of packets no longer overruns b4 while it is busy** - the kernel's hand-off buffer was left at the system default, a few hundred kilobytes shared by every packet in flight.
- FIXED: **System diagnostics reported jq and other tools as missing when they were installed on a USB drive** - the check never looked outside the router's own folders.
- ADDED: **Pinned addresses for a domain** - when a CDN keeps answering with an address the firewall drops, no bypass strategy reaches the server at all, and the only remedies were a hosts file on every device or a `dnsmasq` alias on the router, both redone by hand each time the CDN changed its addresses. Written in hosts file order, address first. A pin only applies to names the set already targets, which is easy to get wrong silently, so a pin naming something the set does not match says so and offers to add it. `Sets > DNS`.
- FIXED: **A curated DNS answer could name the very address b4 was about to reset** - when every address in an answer was unreachable, the replacement was taken from the addresses remembered for that name without checking whether any of them had since been marked unreachable as well, so a device could be handed one and have the connection reset on its first packet.
- CHANGED: **Every address seen in a DNS answer is remembered, not only the ones that completed a handshake** - a name whose answers carry a single address, as the Meta CDN does, left nothing to fall back to once that address was dropped, because an address could only be remembered by connecting to it and the block is what stops the connection.
- FIXED: **Telegram messages arrived while photos, videos and stickers did not, in the WebSocket bridge routing mode** - a client marks a connection as a media one by the sign of the data center number it sends in its opening handshake, and b4 took that number from the address the client had dialled instead, which carries no such mark. Every media session was therefore announced to Telegram as an ordinary one and answered by the wrong half of the data center. The address now settles which data center it is, the client's handshake whether it is the media one.
- FIXED: **A TCP connection was listed under a set whose port filter excludes it** - a set narrowed to a single TCP port, or to one TLS version, still had its name written next to every TCP connection it matched by address, so a set built to take only UDP looked as though it kept taking TCP after being scoped away from it. The connection itself was already left alone; only the name in the table said otherwise.
- FIXED: **The Traffic page said nothing about what happened to a DNS request** - which resolver answered it, whether the answer came from a pin, had unreachable addresses replaced, was turned into NXDOMAIN or dropped outright, all of that was written into the connection record and then discarded by the interface, which could only draw the five markers it had been taught. The documentation listed the outcomes as visible on that page while the page showed a bare UDP row.
- ADDED: **A documentation page for DNS** - what b4 does with name lookups was written down only in pieces, beside each setting it belonged to. [DNS](https://docs.b4core.app/docs/dns).
- FIXED: **Every firewall rule was torn down and rebuilt every few seconds on some systems** - the chain that catches DNS over TCP from the router itself was created with a priority named after a place it cannot sit, so the whole DNS table was rolled back; the rule monitor then found that table missing and restored all of b4's rules from scratch on its next pass, over and over. Each rebuild leaves a moment in which nothing is intercepted at all.

## [1.75.0] - 2026-08-08

- FIXED: **A domain was handled by a different strategy than the one Discovery found for it** - every result was published with all the domains of the run, so two applied results claimed the same domains and set order picked the winner.
- FIXED: **A set applied from Discovery had no effect until b4 was restarted** - it was saved and listed in the interface while the traffic it named kept going out under the previous settings.
- ADDED: **A warning when several enabled sets name the same domain** - only one of them handles that domain, chosen by its place in the list rather than by which strategy works.
- ADDED: **A switch for matching every domain, and one for matching every address** - a set meant to catch everything had to be written by hand as `regexp:.*` or `0.0.0.0/0`, and neither form appears anywhere in the interface. The IPv6 half, `::/0`, was easy to leave out, so a catch-all set passed IPv6 traffic through untouched without saying so. Typing `*`, `any` or `all` into either field adds the same entries, and `*.example.com` is stored as `example.com`, which was taken as written and matched nothing.
- FIXED: **Two sets listing the same IP range fought over it unpredictably** - only one of them reached the matcher, and it was whichever was stored last rather than the one placed higher in the list. A set restricted to source devices lost the range outright to a general set below it, and the winner could change between restarts. Ranges of equal length are settled by set order, and a device-scoped set is offered the traffic first.
- FIXED: **`0.0.0.0/0` among a set's IP targets was dropped on the way to the firewall** - `ipset` cannot store a network with a zero prefix size, so on routers using iptables the routing set lost that entry, while per-set MSS clamping and duplication lost every entry alongside it, since those are loaded in one batch that fails whole. The matching engine had accepted the same entry all along, so a set matched traffic while its firewall side stood empty. Both catch-alls are written to the firewall as their two halves.
- FIXED: **A set routed through a TCP-only upstream proxy left no firewall rules at all on routers using iptables** - each sync of the set failed on the name of its QUIC-refusing chain, one character over the length iptables accepts.
- FIXED: **DNS sent over TCP skipped the set's DNS server** - only requests over UDP were intercepted, so a device that switched to TCP reached the router's usual resolver instead.
- ADDED: **A DNS section in Settings** - the port used for DNS over TCP, how long to wait for an answer, an off switch, and a check against ports other services already use.
- ADDED: **DNS request outcomes in the trace** - a trace named the domain but never whether the request reached the set's DNS server or slipped past it.
- FIXED: **One refusal from Telegram's WebSocket edge switched off a working Cloudflare Worker** - the refusal was treated as a verdict on every WebSocket route to that data center, including the relay the user runs themselves.
- FIXED: **A paused Telegram data center address was answered with an instant failure** - Telegram read that as grounds to reconnect immediately, filling the log with hundreds of identical lines a second.
- ADDED: **A warm spare connection to your Cloudflare Worker** - one ready connection per Worker and data center, in place of the 65-90 ms handshake every new Telegram connection paid for.
- FIXED: **Traffic to a Cloudflare Worker was cut into one small write per Telegram message** - that framing was only ever required by Telegram's own WebSocket edge.
- CHANGED: **Several Worker addresses are tried in a random order** - they were tried strictly as written, so the first absorbed all the traffic and all the rate limiting.
- FIXED: **System diagnostics reported jq, sha256sum and nohup as missing while they were installed** - the check looked in fewer folders than a terminal session does.
- FIXED: **Flow offloading was reported as a problem even when it had been set up to leave b4 alone** - the check could not tell a fast path that skips b4 entirely from one held back until b4 has seen the start of a connection.
- CHANGED: **IP block detection also covers a destination that never answers its SYN** - the feature is described for addresses the firewall drops at the IP level, but it counted TLS ClientHello retransmissions, and a ClientHello exists only once a handshake has completed. An address-level block kills the handshake itself, so the single case the description named was the one case the check could not see, and what it did catch was a stateful block after the handshake.
- ADDED: **The router tests a destination itself before treating it as blocked** - packets going unanswered for one device is equally consistent with a slow server, a brief routing glitch or an uplink outage, so acting on that evidence alone resets connections to a destination nothing was ever wrong with, and an uplink that drops for a minute looks indistinguishable from every destination being blocked at once.
- FIXED: **A destination marked blocked stayed blocked for as long as anything kept trying it** - each lookup refreshed the entry's timestamp, so the five-minute expiry was never reached while a device kept retrying, and nothing re-tested the address in the meantime. A false positive, or a block that had since been lifted, held for the lifetime of the traffic.
- ADDED: **An IP Health section in Settings** - whether an address answers depends on the address and the path to it rather than on the set that happened to match it, so the verdict is one shared judgement rather than a per-set one, and how long it lasts had no control of its own.
- ADDED: **A switch that strips unreachable addresses out of DNS answers** - a CDN answers with several addresses of which the firewall drops only some, so one device hangs on a blocked one while another on the same network loads the site. Pinning a working address by hand in the hosts file or as a `dnsmasq` alias had to be done per address, per domain, and again whenever the CDN changed its addresses.
- ADDED: **Import from another tool** - a [byedpi](https://github.com/dovecoteescapee/byedpiandroid) or [zapret](https://github.com/bol-van/zapret) configuration had to be rebuilt in b4 by hand, option by option. A pasted command line becomes one set per profile, and every option is reported as carried over, approximated, or without an equivalent in b4. Domains are asked for per set, since such a command line rarely carries the host list itself. `Sets > Import`.

## [1.74.2] - 2026-08-02

- FIXED: **b4's own output filled a router's memory** - init scripts from older installers wrote b4's full log to a file kept in RAM on OpenWrt, and updates never replaced those scripts.
- FIXED: **A restart could leave a second copy of b4 running alongside the first** - the departing process deleted the file that tracks the running service, after its replacement had already claimed it.
- FIXED: **Log files kept growing while b4 ran** - the error log was checked against its size limit only at startup, the update log was never trimmed, and abandoned trace files were left behind.
- FIXED: **Turning "Route UDP through upstream" off left the UDP rules in the firewall and took the listener away** - the rule state was keyed on the upstream host, port and username alone, so the switch never rebuilt the firewall side, while the listener was rebuilt without its UDP half. Matched UDP kept being diverted to a port with nothing behind it, so a browser speaking HTTP/3 to a routed site sat through its timeout rather than falling back to TCP, and switching the option off made the site less reachable than leaving it on. Only a restart applied the change.
- CHANGED: **A set routed through a TCP-only upstream proxy refuses QUIC instead of letting it past** - with "Route UDP through upstream" off, matched UDP was not diverted at all, so any site offering HTTP/3 was reached over QUIC straight out of the router while its TCP traffic went through the proxy. The bypass was silent, and a browser kept preferring it for a month after a single visit, since sites advertise HTTP/3 with a long `alt-svc` lifetime. Matched UDP on port 443 is refused with an ICMP port-unreachable, which browsers read as a signal to fall back to TCP. Turn the option on if the upstream implements UDP ASSOCIATE.
- CHANGED: **An upstream proxy that cannot relay UDP reported it only at trace level** - matched UDP was dropped without a word, so a set pointed at a TCP-only SOCKS5 with "Route UDP through upstream" left on looked like a broken site rather than a setting to change.
- FIXED: **Traffic handed to an upstream SOCKS5 proxy went out direct once for every address behind a domain** - a set matches by domain suffix, so `ipinfo.io` covers `website-cdn.assets.ipinfo.io`, while the address pre-resolve only looked up the names written in the set. Every other hostname was learned one address at a time from connections that had already left unrouted, and a site spread across a CDN kept producing them. A hostname that matches a routing set by suffix is resolved in full, and every address it answers with enters the set.
- FIXED: **The first connection after a redirected DNS answer could escape a routing set** - b4 sent the answer to the client and only then wrote the addresses into the set, and writing them runs the `nft` or `ipset` binary, which takes longer than the client takes to send the packet that follows its DNS answer.
- ADDED: **A Remove button for each geo database** - a downloaded `geosite.dat` or `geoip.dat` could only be replaced, never taken off the device, and pointing the destination directory somewhere else left the previous copy at the old path, since editing that field alone changes nothing to save. On a router with little free storage that meant two copies of a 51 MB file with no way to reach either from the interface. Remove deletes the file and clears its path and source URL, so the scheduler does not fetch it back, and a download or upload into a different directory deletes the copy b4 wrote at the old location.
- FIXED: **An update ran out of space on devices with little storage** - it kept a second copy of the installed b4 alongside the new one, and a failed update left that copy behind.
- ADDED: **An IPv4/IPv6 switch in the DC Relay socat helper** - the generated commands could only reach Telegram over IPv4, so on a VPS whose working route to the data centers is the IPv6 one, the addresses had to be found elsewhere and pasted in by hand every time a host was lost. The switch emits `TCP6:` upstreams, and a DC Relay address written as an IPv6 literal (`[2001:db8::1]:7007`) also flips the listening side to `TCP6-LISTEN`.

## [1.74.1] - 2026-07-31

- FIXED: **A domain added to the watchdog was listed as Healthy before anything had checked it** - the entry was created green and counted toward the healthy total, so a site the user already knows is blocked read as fine until the first check landed, and with the watchdog switched off it read that way forever. A domain waiting for its first check is marked Queued, and the force-check button is disabled while the watchdog is off, where it did nothing.
- FIXED: **The watchdog reported a site as healed while it was still unreachable** - a healing run crowned a strategy after one successful fetch, and against a filter that blocks intermittently one of the twenty strategies it tries lands in a gap by chance. That result went straight into the set and the domain was marked healthy, with nothing checking whether ordinary traffic to the site worked afterwards. A healed strategy has to reproduce the fetch several times and survive a re-check through the live engine, or the configuration is put back.
- FIXED: **A healing run could quietly replace a hand-tuned set** - it overwrote the whole tcp, udp, fragmentation and faking section while logging only the fragmentation strategy name, so a switch between two combo variants read as "combo -> combo" and every other tuned value disappeared without a trace.
- FIXED: **A fetch that returned nothing counted as a working strategy during discovery** - a response without a declared length was accepted as complete even when zero bytes of the page arrived.
- FIXED: **The fake packet sent before a blocked greeting did not resemble the greeting it shadowed** - it carried whatever length the fake payload happened to be, in one common case more than twice the real greeting, leaving a filter that reassembles the connection an untouched copy of the real bytes to read. Per-set `fake_len_mode: "match"` sizes it to the greeting.
- FIXED: **The fake greeting's TTL was ignored unless the faking strategy was set to "ttl"** - a set could carry a TTL value while its fake ClientHello still went out at the full system TTL and reached the far end. The SYN fakes read the same value whatever the strategy is, so the setting looked live while doing nothing on the path that mattered. Per-set `apply_ttl` applies it to the greeting alongside any faking strategy.
- ADDED: **The TCP MD5 option on the fake greeting itself** - `md5_on_fake` makes the far end discard the fake while a filter in between still reads it. The option only ever went on a separate fake connection opening, and only alongside a fragmentation strategy.
- CHANGED: **The fake and the real packet no longer share an IP identification number** - one field gave away that both came from the same source.
- ADDED: **Fake-only discovery strategies** - a low-TTL fake with no fragmentation at all, signed and unsigned, and the same shape across the per-family TTL sweep. Every strategy that carried a fake also split the greeting, and the family sweep is built on a base whose fragmentation strategy is combo, so against a filter that reassembles TCP well enough to see through any split, this shape was out of reach at every phase of discovery.
- CHANGED: **The TLS split position can follow the domain name** - `middle_sni` on a set using the `tls` fragmentation strategy places the split inside the domain name instead of a fixed offset from the record header.
- ADDED: **The STUN fake payload ships with b4** - `tls_stun.bin` is built in and picked as "Preset: STUN" under `Fake Payload Type`. It had to be uploaded as a capture file on every device, and a set shared with someone else arrived pointing at a `.bin` they did not have.

- ADDED: **Import of a shared set said nothing when the set referenced a payload file the device does not have** - the set pointed at a `.bin` capture that only existed on the machine it came from, the fake packets silently fell back to a built-in payload, and the set behaved differently than for the person who shared it. The import screen names the missing files and links to the Payloads settings.
- FIXED: **b4 would not start on an OpenWRT router whose kernel has no packet-queue module** - startup ended with a raw firewall error that pointed nowhere near the missing kernel module. [#275](https://github.com/DanielLavrushin/b4/issues/275)
- FIXED: **The installer reported the router as ready when it could not run b4 at all** - the check passed if any one of three queue modules was present, so a router carrying the wrong one installed cleanly and failed at first start.
- FIXED: **The diagnostics report showed no queue number, worker count or queue mode** - it read them from config keys b4 has never written, so those lines were skipped on every router.
- FIXED: **A failed start left part of b4's firewall rules on the router** - when applying the rules stopped partway, the table, chains and hooks created up to that point stayed behind after b4 exited.
- FIXED: **A kernel without connection packet counters stopped b4 from starting at all** - the whole queue rule was rejected and startup failed, over an optimisation that was never required.
- CHANGED: **System Info listed the wrong kernel modules on nftables-only routers** - it marked the iptables-era modules as missing and said nothing about the nftables ones b4 depends on there.
- FIXED: **Telegram on Android could sit at "Connecting" when a set routed it through the built-in Telegram bridge** - the bridge closed connections that stayed silent for five seconds, though Telegram opens them before it has anything to send. [#277](https://github.com/DanielLavrushin/b4/issues/277)
- FIXED: **The "already exists in other sets" warning missed overlaps that do change routing** - it compared entries character for character while traffic is matched by domain suffix, so `example.com` in another set drew no warning for `www.example.com`. [#273](https://github.com/DanielLavrushin/b4/issues/273)
- FIXED: **The DC Relay socat helper generated commands that pointed at the wrong Telegram servers** - it built them from Telegram's published proxy list, whose addresses drop the connection right after the handshake.
- FIXED: **The first request to a domain after an idle spell could hang for twenty seconds or time out, and YouTube on Android could sit for minutes before its interface appeared** - when a client sent a TLS ClientHello too large for one packet, the domain name could land in the second one; b4 read each packet on its own and never joined them, so the set did not match and the connection went out with no bypass strategy at all. Chrome reorders the fields inside that greeting on every connection, which is why the same domain failed on one attempt and loaded on the next. [#279](https://github.com/DanielLavrushin/b4/issues/279) [#280](https://github.com/DanielLavrushin/b4/issues/280)
- FIXED: **The first connection to an address b4 had just resolved for a client went out without the set that owned the domain** - nothing linked a DNS answer, or a domain seen in a rejected QUIC attempt, to the connection that followed it, so until some connection produced a readable domain name the traffic was treated as unknown. That gap was the one place where a domain b4 had already seen went unrecognised, and on Android YouTube it could leave the app retrying for minutes. The link is remembered per client and destination and is discarded when two domains on one address belong to different sets. [#279](https://github.com/DanielLavrushin/b4/issues/279)
- FIXED: **One device could decide which set another device's traffic went through** - the domain learned for a shared CDN address was stored once for the whole network and overwritten by whichever device connected last, so on an address serving both YouTube previews and video, one phone's choice was applied to everyone else. Evidence a device produced itself, from its own DNS answers or QUIC attempts, is preferred over the shared record. [#279](https://github.com/DanielLavrushin/b4/issues/279)
- FIXED: **A plain connection opening handshake was taken apart and re-sent for no reason** - any set with fragmentation or fake-SNI turned on pulled every packet without content out of the kernel and pushed it back through a raw socket, including the opening SYN, although none of those techniques act on a packet that carries no data.
- FIXED: **A set built from a large geosite category took several times more memory than its domains** - reading a few categories out of a 51 MB file needed around 90 MB, and the matcher and a switched-off SOCKS5 server each kept a spare copy of every domain.

## [1.73.0] - 2026-07-05

- FIXED: **A phone that dropped off mobile data could leave a Telegram proxy connection stuck** - when a mobile client backgrounded Telegram or lost signal, its connection frequently died without a clean close, yet b4 held the half-dead session and its link out to Telegram open for many minutes, in some cases until the app was reopened, so returning to Telegram could mean waiting on a dead socket instead of a clean reconnect.
- FIXED: **A stalled link out to Telegram over a WebSocket bridge could wedge a proxy session** - if that link stalled mid-transfer, tearing the session down could itself block for many minutes on the stuck write, holding the client's slot and both connections until a kernel timeout expired.
- ADDED: **Tunable MTProto connection timeouts** - Settings → MTProto exposes a TCP user timeout and an idle timeout for the proxy, for unusual carrier networks or router kernels where the built-in defaults (about two minutes and five minutes) need adjusting or turning off.
- ADDED: **API to list active MTProto sessions and clients** - two HTTP endpoints for integrations: GET /api/mtproto/sessions returns one entry per live client connection (client IP and port, the Telegram destination, and when it connected and was last active), and GET /api/mtproto/active-clients groups those by secret (active connection count and the distinct client IPs using each one). The API exposed only a per-secret connection count, with no way to see the client addresses behind it.

## [1.72.0] - 2026-07-02

- ADDED: **A separate MTProto secret for each person** - the MTProto proxy can now hold several named secrets instead of one shared code. Each one can be named (for example, per person), switched on or off on its own, and shared as its own link or QR code, so access can be given or taken away one person at a time.
- ADDED: **See who is using the MTProto proxy** - the Traffic page now labels each proxy connection with the secret it used, and a new dashboard panel shows active connections, total sessions, and data used for each named secret.
- ADDED: **A set can now apply to every device except chosen ones** - in a set's Source Devices there is a new switch, "All devices except selected". With it on, the set applies to traffic from all devices except the ticked ones, instead of only to the ticked ones - handy when one device (say, a work laptop) should be left alone.
- CHANGED: **The set's device list shows each device's MSS value** - if a device has an MSS limit set under Settings → Device Filtering, the set's Source Devices table now shows it next to the device, so there is no need to switch pages to check.
- ADDED: **Match a set by domain only, without learning its IPs** - a per-set "Domain-only matching" switch under the set's Targets. With it on, b4 decides the set purely from the domain or SNI it can see and does not remember or route the IP addresses behind those domains. It is meant for shared CDN addresses (YouTube, Google): a video domain and an unrelated one such as preview images can share a single IP, so the address b4 learned for the first one pulled the second onto the wrong bypass strategy.
- FIXED: **Memory use jumped after saving settings** - saving the configuration made b4's memory use climb, even for changes that had nothing to do with the blocked-site lists.
- FIXED: **A routing set could stop adding new addresses on some routers** - on certain setups (for example b4 in a container on MikroTik) the router rejected freshly discovered addresses, so a set routed through a chosen interface, proxy, or the Telegram bridge slowly stopped working. Those addresses are now stored in a form the router accepts. [#267](https://github.com/DanielLavrushin/b4/issues/267)
- FIXED: **The log repeated "Tables rules missing, restoring..." every few seconds** - with NAT Masquerade turned on, b4 looked for one of its firewall rules in the wrong place, decided it was missing, and needlessly re-applied all its rules over and over. The rules were in fact fine; b4 now checks the right place and the endless restoring has stopped.

## [1.71.1] - 2026-06-29

- ADDED: **System Info now shows the router's kernel capabilities** - a new section reports whether the router supports the underlying features that advanced modes rely on.
- FIXED: **Telegram bridge could be slow to connect or drop transfers** - in some setups b4 got in the way of its own connection out to Telegram, slowing the first connect and occasionally cutting transfers short; b4 now keeps its own traffic clear.
- FIXED: **A named device sometimes showed as its address instead of its name** - on the dashboard and Traffic page a device you had named could appear as a plain hardware address while it was off the network (for example connected over VPN); its saved name now stays shown, and named devices remain listed in Device Filtering with an "offline" label.

## [1.71.0] - 2026-06-28

- ADDED: **The Logs page is easier to read** - log lines are now colour-coded by importance, and each type (error, warning, info, and so on) can be shown or hidden with one click.
- ADDED: **Record a log session and download it to share** - the Logs page now has a "Start trace" button that records everything until "Stop & save", which downloads a single log file to attach when asking for help. The file already includes system details, and recording stops on its own after 15 minutes.
- ADDED: **NAT Masquerade can now target several interfaces at once** - when NAT Masquerade is turned on (Settings → Feature), you can pick any combination of network interfaces for it to use — for example two out of three — instead of only a single interface or all of them.
- ADDED: **Runtime panel on the dashboard** - shows b4's memory use over time, with a live chart and a couple of health readings, so a slow climb is easy to keep an eye on.
- ADDED: **Per-device filtering (whitelist/blacklist) works in TUN mode** - the Device Filtering list under Settings only took effect in NFQUEUE mode; in TUN mode every device's traffic was inspected regardless of the selection, so choosing which devices use the bypass changed nothing there.
- CHANGED: **Live Signal panel reworked around throughput** - the connections- and packets-per-second readings hovered near zero on a home router and meant little, so they gave way to live throughput, total data handled, targeted share, and active connections.
- FIXED: **Memory could slowly creep up the longer b4 ran** - a few background jobs (logging, AI chat streaming, Discovery) didn't always tidy up after themselves, so usage grew over long uptime.
- FIXED: **After stopping b4 in TUN mode, new sites could stop loading while already-open ones kept working** - on an unclean stop (a forced kill, or a shutdown that ran past its time limit) the virtual TUN interface disappeared but its routing rule could be left behind, steering fresh connections and DNS lookups into a path that no longer existed until b4 was started by hand again.

## [1.70.1] - 2026-06-23

- ADDED: **System diagnostics now show the active engine and the firewall rules b4 has set up** - the diagnostics page tells you whether b4 is running in NFQUEUE or TUN mode (so TUN setups no longer look like a failed firewall), and lists the firewall rules b4 currently maintains, making it easier to see what is in place and share when asking for help.
- CHANGED: **b4's internal firewall marks moved to a less-contended range** - b4 tags its own packets with a firewall mark (fwmark); the values it used overlapped the range Tailscale and some other router apps use, which could misroute traffic when they ran alongside b4. The internal marks now use high bits that other tools rarely touch.
- FIXED: **Bypass stopped working for mobile apps (for example the YouTube app on Android) on gateway/container setups with NAT Masquerade** - the fake and fragmented packets b4 sends to defeat DPI were no longer given the gateway's public address, so they never reached the server. This mainly hit QUIC traffic, which phone apps prefer, while ordinary websites over TCP kept working.
- FIXED: **A set could fail to start when it mixed a country/service list with its own listed addresses** - if an address you added by hand was already covered by a chosen list (for example adding Instagram addresses alongside the Facebook list), b4 saw them as overlapping and refused to start.
- FIXED: **In TUN mode every device appeared under the router's public address, so traffic could not be told apart by device** - the TUN engine tags packets with the router's WAN address before b4 inspects them, which flattened the connection logs, per-device statistics, and per-device set rules onto that one address and hid the phone, laptop, or TV behind it.

## [1.70.0] - 2026-06-21

- ADDED: **New engine for devices without NFQUEUE** - some minimal devices lack the kernel modules b4 normally needs, so it could not run on them. A new mode under Settings → Feature Flags routes traffic through a virtual interface (TUN) instead. It inspects only the first packets of each TLS (port 443) and DNS connection, like the normal engine, rather than carrying every packet, and leaves the device's default route in place; on kernels that cannot count per-connection packets it routes the whole default route through the TUN instead. Strategy discovery (auto-tuning) still needs NFQUEUE, so it cannot run on these devices.
- ADDED: **TUN uplink can follow the default route automatically** - leave the uplink on Auto (the default) and b4 takes the outbound interface, gateway and source address from the current default route, re-pointing them whenever it changes; pick a specific interface to pin a fixed path instead. Without it the uplink was fixed at startup, so a WAN failover or an L2TP/PPP/VPN tunnel reconnecting after b4 had started left processed traffic going out the old, dead path and bypassing b4 until a manual restart.
- ADDED: **Sortable columns in the Traffic page's Aggregated view** - clicking a column header sorts the grouped list by that column, ascending or descending, like the Raw feed.
- CHANGED: **Web UI password is now stored securely** - it is kept only as a hash and is no longer shown on the settings page (leave the field blank to keep it, or type a new one to change it).
- CHANGED: **Dashboard rebuilt on one consistent design** - it now leads with a live signal panel that puts connection-rate and the key counters together, with every block sharing one panel, row, and header style.
- FIXED: **A set's encrypted DNS (DoH) redirect failed in some setups** - lookups could time out or come back empty, so pages would not load. This affected gateway and container setups (for example b4 in a container on MikroTik) using NAT masquerade.
- FIXED: **Routing set stopped updating its addresses on its own** - in some setups (for example b4 in a container on MikroTik) the set's addresses only filled in on restart or when the set was toggled, then expired and were never refreshed. The set now learns addresses live from DNS replies again.
- FIXED: **Telegram bridge did not recover after an internet outage** - if the connection dropped and came back later, a set using the Telegram bridge kept failing until b4 was restarted by hand.

## [1.69.1] - 2026-06-14

- FIXED: **Sets manager felt frozen after enabling, reordering, duplicating, or deleting a set** - the screen only changed once the action had been saved and the whole configuration re-fetched a second or two later, so a click looked like nothing had happened, a dragged set snapped back to its old place before jumping to the new one, and a duplicate appeared out of nowhere after a pause.
- FIXED: **MTProto proxy stopped connecting to Telegram on networks that block its data-center addresses** - 1.69.0 changed the proxy to reach each Telegram data center at its own address, which many restricted networks drop, so connections timed out and Telegram would not load through the proxy.

## [1.69.0] - 2026-06-14

- ADDED: **Diagnostics now flag flow offloading** - many routers have a speed feature called `flow offloading` that sends traffic down a fast path which skips b4 entirely, so b4 looks installed and running but nothing is actually bypassed (a common puzzle on OpenWrt). The system diagnostics (Settings system info, and the installer's diagnostics screen) now show whether flow offloading is switched on, so this can be spotted at a glance and turned off.
- ADDED: **Connection limit for the MTProto proxy** - a new "Max Connections" field in Settings -> MTProto Proxy caps how many client connections the proxy serves at once, with the built-in default raised from `512` to `2048` so a proxy shared by many people has room before it turns connections away.
- FIXED: **Routing could clash with another router app such as [XrayUI](https://daniellavrushin.github.io/asuswrt-merlin-xrayui/en/install)** - a set sent through a proxy or the Telegram bridge could be handled by that other app instead, so it only worked while the app was installed.
- FIXED: **Discovery refused web addresses that contain a comma** - some perfectly valid links (for example certain Google image addresses) have a comma in them, which Discovery mistook for a separator between addresses and split the link into broken pieces, so it could not be added.
- FIXED: **Could not pick individual results once a Discovery search had finished** - while a search was running you could add any of the configurations it turned up, but as soon as it stopped only the single best per group could be applied and the buttons to add the others disappeared. The buttons to add each result stay available after the search ends.
- FIXED: **Discovery log was lost on page refresh and missing after a search finished** - the log was only streamed live, so refreshing the page wiped everything shown so far and left only the next incoming line, and a finished (especially quick) search often showed no log at all. The log is now kept on the service, so the full run is replayed whenever the page is opened or refreshed and stays readable after the search ends.

## [1.68.0] - 2026-06-10

- FIXED: **Discovery found nothing on connections that tamper with DNS** - on networks where the provider tampers with DNS (so ordinary name lookups came back wrong or empty), Discovery could not look up its test sites and reported that nothing worked, even when a working setup existed. It looks those sites up over encrypted DNS instead, and saves the result set to use encrypted DNS (DNS-over-HTTPS) for its lookups by default, which holds up where plain DNS is tampered with.
- FIXED: **Cancelling Discovery could crash b4** - pressing Cancel while a search was testing several sites at once could crash the whole service and force a restart.
- FIXED: **Stopping b4 could leave its firewall rules on the router** - the cleanup at shutdown could be undone at the last moment, so the rules stayed in place after exit.
- FIXED: **Routing through another interface broke after that interface changed its address** - when the chosen outgoing interface (for example a VPN or a modem) got a new address, the set's traffic kept using the old one until b4 was restarted.
- FIXED: **Incomplete cleanup on stop** - stopping b4 (or `--clear-iptables`) left traces behind: adjusted system network settings, stray routing entries when two sets shared one outgoing interface, and a firewall rule that piled up with proxy or bridge routing.
- FIXED: **Saving settings could leave the running configuration out of step** - changing the SOCKS5 proxy or your sets (adding domains or IPs, creating, editing, reordering, deleting, or bulk-toggling) could save a result that did not match what was actually running.

## [1.67.2] - 2026-06-10

- FIXED: **Web UI update reported success but kept the old version on some setups** - where b4 isn't managed by a normal service (for example running directly in a container), the update replaced the program on disk but never restarted the running copy. It now stops the old copy and relaunches the new one, so the update actually takes effect.
- FIXED: **Leftover "zombie" update processes piling up** - each update attempt left a finished helper process behind; these are now cleaned up.
- ADDED: **Update log** - every Web UI update now writes a step-by-step trace to `update.log` in the log folder (default `/var/log/b4`, reset each attempt), making failed updates much easier to diagnose.
- ADDED: **Localized changelog** - the Web UI now shows the changelog in the selected language.
- CHANGED: **Logging setting is now a folder, not a single file** - Settings → Service now asks for a log _directory_ (default `/var/log/b4`) instead of a path to `errors.log`, so all of b4's log files (errors, updates, and any added later) live together and can be moved in one place. Existing configs are migrated automatically (your old folder is kept); leave the field empty to turn file logging off.

## [1.67.1] - 2026-06-10

- FIXED: **Connections page slowed down the more network owners you looked up** - looking up the "AS..." label for many addresses made the live list sluggish over time. It now stays responsive no matter how many you add.
- FIXED: **Slow page failures when a set's encrypted DNS server was briefly unreachable** - lookups used to hang until they timed out; they now fail right away instead of stalling, and never fall back to plain DNS.
- FIXED: **Cancelling Discovery did not stop it** - pressing Cancel left the search running, so starting another ran two at once with mixed-up, unreliable results. Cancel now stops the search promptly, and a new search waits until the previous one has stopped.

## [1.67.0] - 2026-06-08

- ADDED: **Encrypted DNS (DoH) for a set** - a set's DNS tab can now send its name lookups over an encrypted DNS-over-HTTPS connection instead of to a plain DNS server's IP. Some services only work when looked up through a specific resolver (for example `xbox-dns.ru` for sites that say "not available in your country"), and some providers tamper with ordinary DNS - encrypted DNS gets around both. Switch the set between "Plain DNS" and "DNS-over-HTTPS", then pick a server from the built-in list or paste your own address. Only that set's lookups are affected, so the rest of your DNS stays as it is.
- ADDED: **Target a set to IPv4 or IPv6 only** - a set can now be limited to only IPv4 or only IPv6 traffic, alongside the existing domain, IP, and TLS-version targeting. Some networks handle IPv4 and IPv6 differently, so the settings that work for a site over one can differ over the other. You can now make one set for a site over IPv4 and a separate set for the same site over IPv6, each with its own options. Discovery can also search for a working setup specifically over IPv4 or IPv6 and saves the result limited to that version. The default is "Any" (both), and the choice only appears when both IPv4 and IPv6 are turned on in Settings.
- FIXED: **Sending a set's traffic to a proxy didn't work while bypass options were on** - when a set was set to route its traffic through an upstream proxy (or the Telegram bridge), b4 still tried to apply its DPI-bypass tricks to that same traffic, which fought with the routing and the connection failed to open. b4 now hands routed traffic straight to the proxy, so routing works even with the bypass options left enabled - and routed connections still show up on the Traffic page.
- FIXED: **RST protection did nothing on sets that also used SYN-based bypass** - if a set combined "RST protection" with certain TCP bypass techniques, the protection was silently inactive because those connections weren't being tracked, so the site still got reset. RST protection now works in that combination. It also blocks the matching fake reset aimed at the destination server, not only the one aimed at your device.
- FIXED: **Dummy network interfaces were missing from the interface lists** - a dummy interface (for example `dummy0`) did not appear in Settings, so it could not be picked for monitoring or NAT masquerade. Any dummy interface that is up now shows up in both lists.
- FIXED: **Telegram desktop kept reconnecting every 30-60 seconds over the Telegram bridge** - a background keep-alive added in 1.66.0 to hold idle connections open actually sent a signal Telegram's servers rejected, so the connection dropped and rebuilt itself about twice a minute (you would see parts of the Telegram window flicker). That keep-alive has been removed, so connections stay healthy on their own again.
- FIXED: **Monitoring a site could add it to a blocking set and break it** - if you monitored a site (for example `youtube.com`) and also ran a set that blocks or routes traffic - such as an ad-blocking set built from a category like `category-ads-all` - the watchdog could mistake that set for the one that owns your site, because the block list happened to contain a sub-domain of it (for example `ads.youtube.com`). When the site dipped and the watchdog tried to repair it, it added the site to that block/routing set, so the site got blocked instead of restored. The watchdog now only repairs sets that list the site by name, and never touches a set that has Routing turned on, so monitoring can no longer poison a blocking or routing set. Note: if an earlier version already added the monitored site to such a set, remove it there by hand once.

## [1.66.0] - 2026-06-07

- ADDED: **Blocking stats on the Dashboard** - when a set uses Block (blackhole) mode and actually blocks something, the Dashboard now shows a "Blackhole" panel with the total number of blocked attempts, the most-blocked domains, and which devices ran into the most blocks. The panel stays hidden until there is something to show, so it never clutters the page when nothing is being blocked. Blocked connections are also tagged with a "block" label on the Traffic page so you can spot them in the live feed.
- ADDED: **Control over the Telegram server-list backup** - to reach Telegram, b4 fetches Telegram's data center list from Telegram's official address, and falls back to a backup copy hosted by the b4 author only if that is blocked (`lavrush.in`). Settings -> MTProto Proxy now has a "DC list fallback mirror" switch to turn it off or point it at your own copy.
- FIXED: **Set Routing now respects your device choices** - if you used Settings -> Devices to make b4 work for only some devices (or to exclude some), that choice was ignored by a set's Routing tab. Whatever the set did with the traffic (send it to another network interface or an upstream proxy, run it through the Telegram bridge, or block it) happened for every device regardless. Routing now applies only to the devices you picked. You can also give an individual set its own device list in its settings, so a single set can route, bridge, or block traffic for just specific devices without affecting the rest.
- FIXED: **The router itself showed up as a wrongly-named device on the Traffic page** - b4 mistook the router's own address for a regular client and listed it as a separate device (sometimes guessed as a phone brand), filing the router's own connections under it. b4 now recognizes its own interfaces and labels that traffic simply as "Router".
- FIXED: **Discord voice and video calls could drop when blocking UDP** - on a set with UDP set to Drop or Reject, Discord calls could break because b4 did not recognize Discord's call traffic. b4 now lets it through (when "Filter STUN" is on, the default), so calls keep working.
- FIXED: **b4 would not start on some Asus Merlin routers** - on certain firmware (for example the MerlinWRT) b4 quit right after starting and showed a confusing message about a missing `xt_connbytes` feature, even though the router actually supported it. The real cause was b4 using a newer firewall command option that the router's built-in tool did not understand. b4 now adapts to the router's own tools automatically, so it starts normally with nothing extra to install.- FIXED: **Connections page showed nothing when the device clock was out of sync** - the time filter (30s / 1m / 5m / 15m) and the per-connection activity graph compared the live data against the browser's own clock, so a computer whose clock was off by even ~30 seconds could see an empty list and empty activity bars. Filtering and the activity graph now use the timestamps in the connection data itself, so they work regardless of the device clock or its timezone.
- FIXED: **A device could still show up twice on the Traffic page** - one device sometimes appeared as two rows, once under its name and once under its bare IP. b4 now recognizes these as the same device and shows it only once.
- FIXED: **Discovery found no working strategy when site names were entered with quotes** - if you added domains to Discovery wrapped in quotation marks (for example `"discord.com","youtube.com"`, as happens when pasting a copied list), b4 kept the quote marks as part of each name, so every lookup failed and Discovery reported nothing working for any site. Surrounding quotes are now removed automatically. [#241](https://github.com/DanielLavrushin/b4/issues/241)

## [1.65.0] - 2026-06-06

- ADDED: **Block (blackhole) routing mode** - a new "Block" option in a set's Routing tab that blocks all matched traffic (ad/tracker domains, IPs, or a GeoSite category like `category-ads-all`) across the whole network - every LAN device and the router itself - with no output interface needed. It blocks by name and not just by IP, so it keeps working even with encrypted DNS and won't break unrelated sites sharing the same servers. See [Blocking](https://docs.b4core.app/docs/sets/blocking) for setup and details.
- ADDED: **Upstream SOCKS5 proxy now routes UDP too** - sets that send traffic to an upstream SOCKS5 proxy used to forward only regular (TCP) connections; UDP traffic - such as QUIC (used by YouTube and many Google and video services) and DNS - bypassed the proxy and went out directly. UDP from LAN devices now goes through the upstream proxy as well, so a set can route a device's full traffic through it. The proxy must accept UDP.
- FIXED: **A device could appear twice on the Traffic page, and hand-added names were missing** - in the Aggregated view's Devices sidebar, one device sometimes showed up as two: once under its name and once under its bare IP address, because some of its connections arrived without a recognizable hardware address. Those connections are now matched back to the right device by its IP, so each device appears once. Devices you add by hand (Settings -> Devices), including ones behind another router, now show their assigned name here too instead of a raw IP.
- FIXED: **Command-line options like `--skip-tables` got saved into the config and stuck** - starting b4 with an extra command-line option used to write that option into the configuration file, so it kept applying on later starts even when you ran b4 without it. Command-line options now affect only the run you pass them to and are never saved into the config.

## [1.64.0] - 2026-06-01

- FIXED: **DNS redirect didn't work** - when a set sent its DNS lookups to a chosen server (the set's DNS tab), names failed to resolve and the internet seemed to hang. Redirected DNS now works as expected.
- IMPROVED: **MTProto Telegram routing is more reliable, especially for media** - the WebSocket routing (bridge mode) now works on phones as well as desktop, loads media more reliably, and always prefers WebSocket regardless of the proxy server's transport setting. Data centers without a WebSocket edge no longer stall when the shared fallback domains get rate-limited. New "Cloudflare Worker" support adds a free, per-user relay (no domain to buy) for accounts where media, reactions, or stickers still fail to load - see Settings -> MTProto Proxy.
- FIXED: **Copy buttons did nothing when the Web UI was opened over plain HTTP** - copying commands, set exports, or system info silently failed on installs reached by LAN IP (for example `http://192.168.1.1`), because browsers only allow the modern clipboard API over HTTPS or localhost. The fallback copy path now works in those cases too.
- FIXED: **Memory and CPU slowly climbed when the watchdog repeatedly repaired a site** - each watchdog repair left behind a background task that never stopped, piling up over days until b4 was killed. [#227](https://github.com/DanielLavrushin/b4/issues/227)
- FIXED: **Traffic page lumped every connection under "Unknown" in the Aggregated view** - on networks where b4 cannot map a client IP to a MAC address (for example devices behind another router), the Devices sidebar collapsed all traffic into a single "Unknown" device, even though the Raw feed clearly showed different source IPs. Connections without a known MAC are now grouped by their source IP, so each device appears separately.
- FIXED: **Filter box did nothing in the Traffic page's Aggregated view** - multi-term filters like `!github + !google` were treated as one literal text match, so the view usually showed nothing. The Aggregated view now uses the same filter syntax as the Raw feed: combine terms with `+`, exclude with `!`, and target a column with `field:value`.
- ADDED: **More checks in the DPI Detector** - the DPI Detector page can now also measure how quickly your DNS servers respond and test whether Telegram works (download speed, plus whether all of Telegram's servers are reachable). The existing checks were made more accurate at telling apart the different ways a provider can block or tamper with a connection, the list of sites and servers it tests was refreshed, and a new "Legend" explains what each result means. The DPI Detector is based on the [dpi-detector](https://github.com/Runnin4ik/dpi-detector) project.

## [1.63.0] - 2026-05-22

- FIXED: **Geo databases sometimes saved to a broken `b4` folder after install** - in rare cases the installer would record `b4/geosite.dat` instead of a full path like `/etc/b4/geosite.dat`, and the Web UI then refused to download new files. The installer now refuses non-absolute paths, b4 fixes any broken path it finds on startup, and the UI falls back to a safe default if the stored path looks wrong.
- ADDED: **Quick toggle for all sets** - new switch in the Sets page top bar that turns every set on or off in one click. Useful for temporarily disabling all bypass, or for isolating one set while debugging.
- ADDED: **"Test direct TCP" button for the MTProto proxy** - new button in Settings → MTProto Proxy next to "Test connection". Probes Telegram directly, bypassing the DC Relay. Use it to tell whether a problem is on your relay/VPS or somewhere between b4 and Telegram.
- ADDED: **Auto-update for geo databases** - new "Auto-update" section in Settings → Geo Databases. Optional "Refresh on startup" re-downloads both files every time b4 starts (handy when files are kept in `/tmp` and get wiped on reboot). A separate "Schedule" picker (Off / Daily / Weekly / Monthly) runs a background refresh on its own. Missing files are also re-downloaded automatically at startup whenever a source URL is set, even with both options off.
- ADDED: **Per-set MSS Clamping** - new "MSS Clamping" section in the TCP tab of each set, alongside the existing global and per-device options. Lets you set a custom MSS for the connections in one set - for example, a small MSS for a Smart TV streaming YouTube. The set must target by IP, GeoIP, or source device; SNI domain or GeoSite targets alone are not enough.
- FIXED: **Direct TCP path used internal Telegram backend addresses instead of public ones** - b4 was reading addresses from Telegram's `getProxyConfig` (a topology file for MTProxy operators) and dialling them directly. Those backends silently dropped the connection after the handshake because clients are not allowed there. b4 now uses the well-known public data center IPs for direct dialling, like real Telegram clients do.
- FIXED: **MTProto proxy ignored the DC Relay in Auto mode** - when the upstream transport was set to "Auto" and a DC Relay was configured, b4 still went straight over WebSocket and never used the relay. The relay is now tried first in Auto mode whenever one is configured; WebSocket stays as the fallback. The mode description updates to reflect this.
- FIXED: **MTProto media data center (DC 203) did not work through the DC Relay** - DC 203 traffic was sent to a port that does not exist on any relay setup, so loading media through the relay failed. DC 203 now reuses the DC 2 relay port (matching the existing WebSocket behaviour), and has a built-in default IP for direct TCP as well.
- FIXED: **Geosite/geoip updates didn't take effect until the next set edit** - after downloading or uploading a new geosite or geoip file from the Web UI, b4 kept matching against the old domain and IP lists. The new file is now applied to all sets immediately.
- FIXED: **Some sites would not load through the built-in SOCKS5 server** - sites like YouTube failed when accessed via the SOCKS5 proxy while working fine in direct mode.
- FIXED: **Per-device MSS could be overridden by Global MSS on iptables routers** - when Global MSS and per-device MSS were both enabled, the global value silently won on iptables-based routers (Merlin, OpenWrt without nftables, Keenetic). The more specific value now wins, matching how it has always worked on nftables.
- IMPROVED: **Watchdog keeps checking while it repairs a broken site** - previously, when b4 detected one site was blocked and started repairing it, the watchdog stopped checking every other site until the repair was done. If a second site broke at the same time, it had to wait its turn. The watchdog now keeps monitoring all sites during a repair, and if several sites break close together they are repaired in one go.
- IMPROVED: **MTProto connection test now detects upstream drops** - the existing "Test connection" button used to only check that an IP was reachable. It now also completes the MTProto handshake and watches whether Telegram (or your relay) closes the connection right after - the failure mode users actually hit. Results show which stage broke (connect, handshake, or dropped after handshake).

## [1.62.1] - 2026-05-11

- ADDED: **Memory limit setting** - new "Memory Limit" field in Settings → Logging. Caps how much memory b4 may use. Leave empty for auto (half of system RAM, the previous default). Useful on routers with little RAM where other services compete for memory. Accepts values like `128MiB`, `256m`, `1g`, or `off` to disable.
- ADDED: **MTProto proxy works in censored networks** - the built-in Telegram proxy can now reach Telegram over WebSocket, in addition to direct TCP. New "Upstream Transport" section in Settings → MTProto Proxy with three modes: Auto (WebSocket → TCP, the new default), WebSocket only, and Direct TCP. Existing installs are switched to Auto on upgrade, so networks without filtering see no change.
- ADDED: **Cloudflare fallback for the MTProto proxy** - optional "Cloudflare custom domain" field. If Telegram's own WebSocket endpoint is ever blocked too, pointing a Cloudflare zone at the Telegram data center IPs lets the proxy tunnel through Cloudflare instead.
- FIXED: **MTProto proxy was incompatible with some Telegram clients** - clients using transport variants other than padded-intermediate were silently dropped during the handshake. All three Telegram transport variants are now accepted, and the client's choice is forwarded to the data center.
- FIXED: **Upstream SOCKS5 proxy routing did not work on some OpenWrt setups** — traffic skipped the proxy and went straight to the internet on installs missing two required kernel modules. The modules are now installed and loaded automatically, and a clear error is shown if they are still missing. [#221](https://github.com/DanielLavrushin/b4/issues/221)
- IMPROVED: **Cleaner Import/Export for sets** - the exported JSON of a set now hides settings for features that are turned off, so you only see what actually matters. Easier to read, share and compare.

## [1.61.4] - 2026-05-10

- FIXED: **False "another b4 instance is already running" error** - b4 could refuse to start after a crash, after restarting from the Web UI, or when running inside containers (for example on MikroTik), even when no other b4 was actually running. The single-instance check is now reliable in those cases.

## [1.61.3] - 2026-05-09

- ADDED: **Custom payload for UDP fake packets** - new "Fake Packet Payload" picker in the UDP fake settings of each set. Choose a captured `.bin` (uploaded in Settings → Payloads, or auto-captured from live QUIC traffic) to use as the body of fake UDP packets. Empty = zero fill (previous behavior). The Settings → Payloads upload form now has an explicit TLS/QUIC protocol selector.
- ADDED: **Auto-generated QUIC Initial payload** - new "(auto: QUIC Initial)" option in the UDP Fake Packet Payload picker. b4 generates a fresh randomized QUIC Initial packet for every fake, with random connection IDs each time, so no upload is needed and the bytes can't be fingerprinted by repetition. Recommended packet size for this mode is 1200 bytes. Works with any Faking Strategy (None / TTL / Checksum).
- ADDED: **Bundled QUIC presets** - two ready-to-try options ("QUIC preset 1" and "QUIC preset 2") in the UDP Fake Packet Payload picker. Pick one if the auto option does not work for your provider; if neither helps, upload your own `.bin`.
- ADDED: **MTProto relay setup helper** — new "?" button next to the DC Relay field in Settings → MTProto Proxy. Opens a popup with the current Telegram data center list and ready-to-copy commands for the VPS, so nothing has to be calculated or guessed by hand.
- ADDED: **Single-instance enforcement** — b4 now refuses to start if another b4 process is already running on the same host, exiting with a clear message and the existing PID.
- ADDED: **Sequence overlap length for fragmentation** - new "Overlap Length" field for Combo and Disorder splitting. b4 prepends the configured number of pattern bytes to one of the real fragments with its TCP sequence number shifted back by the same amount, in addition to (or instead of) the existing fake-packet overlap.
- ADDED: **AI field explanations (experimental)** - small AI buttons next to some fields open a popup that explains what the field does. Work in progress: only a few fields are covered and answers can be wrong or incomplete. Not recommended to rely on yet.
- IMPROVED: **Web UI error messages** - when saving a configuration or a set fails, the Web UI now shows a clear reason (for example, a port is already in use).
- FIXED: **Upstream SOCKS5 routing failed on BusyBox routers** — sets routed through an upstream SOCKS5 proxy were missed by the 1.49.0 fix and still hit the `BusyBox` table-ID limit. Now kept within the safe range too.
- FIXED: **QUIC blocking sometimes does not work for YouTube on phones** - newer Chrome versions on Android use a QUIC variant b4 didn't recognize, so "block all QUIC" let those packets through and YouTube kept working in the browser. b4 now recognizes any QUIC packet, current or future.
- FIXED: **Routing fails on MikroTik containers when interface names contain dashes** - interface names with dashes were rejected by `nft`. b4 now quotes interface names in routing rules, so any name works.
- FIXED: **Interface routing did not work on MikroTik container bridges** - when several container interfaces shared one bridge with a single upstream gateway, the per-set default route was added without a gateway and traffic went nowhere. b4 now reuses the system gateway when it is reachable through the chosen interface.
- FIXED: **Backup file could not be selected in Safari on macOS** - the file picker greyed out the downloaded `.tar.gz` backup file. Selecting the backup now works in Safari.
- FIXED: **Voice calls, screen share and gaming could break in UDP fake mode** - b4 was splitting all matched UDP packets, which could disrupt voice, screen share and gaming traffic. Only QUIC packets are split now; the rest pass through untouched.

## [1.60.1] - 2026-05-02

- ADDED: **Per-set strategy escalation** - each set now has an "Escalation" tab with an "Escalate to" dropdown. Pick another set as the failover target: if the current set keeps failing for a destination, b4 switches that destination to the chosen set on the next connection. Tracking is per-hostname, so a problem with one site does not affect others that happen to share the same server IP. How quickly to escalate and how long to keep the switch are configurable per set (defaults: an hour, then retry). Lets you chain several strategies instead of giving up after the first one fails.
- ADDED: **Share MTProto proxy connection** - new "Share connection link" button in Settings > MTProto Proxy. Opens a dialog with a `tg://` connection link, a QR code (scan with your phone camera to add the proxy to Telegram), and Copy / Open in Telegram / Share buttons.
- FIXED: **Service crashed at startup on routers without `ipset`** - "Enable Packet Duplication" could prevent b4 from starting on routers where `ipset` is not installed (some Keenetic / Merlin setups). b4 now logs a warning and keeps running. For full-connection duplication, install ipset (Keenetic / Entware: `opkg install ipset`).
- FIXED: **"Update" button in Web UI didn't actually update b4** - on some setups clicking "Update" did nothing and the version stayed the same after restart.
- FIXED: **IPv6 addresses on the Traffic page were cut off** - when adding an IPv6 address from the Traffic page, the address was shown as just `2a01` and the suggested CIDRs included the port number. Adding to a set, IPInfo lookup, and ASN enrichment now work for IPv6 too.

## [1.50.1] - 2026-04-29

- FIXED: **Upstream SOCKS5 routing didn't actually proxy** — when a set was configured to route through a SOCKS5 server (local or remote), traffic was silently going direct instead of through the proxy. Connections from the same machine running B4 now reach the upstream proxy correctly.
- FIXED: **Duplicate masquerade rule when routing is enabled with IPv6** — on `nftables` setups with both IPv4 and IPv6 enabled, the per-set NAT chain ended up with two byte-identical `masquerade` rules. The rule is now scoped per address family (`meta nfproto ipv4` / `ipv6`), matching the split already used for the mark rules.
- IMPROVED: **Error log keeps history across restarts** - the error log was wiped on every B4 start, so any trace of a crash was lost as soon as B4 auto-restarted.
- CHANGED: **"Connections" page renamed to "Traffic"** - clearer name for the page that shows live network activity. Old `/connections` links still work and redirect to the new page.
- IMPROVED: **Traffic page works at any log level** — previously, raising the log level above `Info` made the Traffic page stop updating. The UI now gets traffic events through a separate channel, so you can keep the log level on `Warn` or `Error` to quiet down system logs without losing the live traffic view. Opening the Traffic page also instantly shows the last few hundred recent events instead of waiting for new traffic.

## [1.50.0] - 2026-04-27

- IMPROVED: **Refreshed UI** — the whole web UI has been redesigned: cleaner typography, tighter spacing, calmer colour palette, larger and easier-to-read numbers, and better keyboard accessibility throughout.
- ADDED: **Sequence overlap pattern in Combo fragmentation** — the Combo fragmentation strategy can again be tuned from the UI: choose a preset (TLS handshake, HTTP GET, zeros) or build a custom byte pattern for the overlap step.
- ADDED: **Upstream SOCKS5 routing** - per-set routing can now forward matched traffic to a SOCKS5 proxy (local or remote) instead of out a network interface. Use this to chain b4 with Xray, sing-box, or any SOCKS5-speaking proxy. Pick "Upstream SOCKS5 proxy" mode in the routing tab and set the host and port.

## [1.49.1] - 2026-04-20

- FIXED: **Routing stopped working after restarting tun2socks / sing-box** - if the proxy's network interface was recreated, or wasn't ready yet when B4 started, traffic routing silently broke until B4 was restarted too. B4 now detects these changes automatically and restores routing within a second.
- FIXED: **Connections page - Aggregated view layout** - the set name was shown twice when a set matched both by domain and by IP, and the SOCKS5 "proxy" label could overlap the domain column.

## [1.49.0] - 2026-04-18

- ADDED: **Manual devices** — you can now add IP addresses of devices behind another router that are not visible in the ARP table. Added devices appear in device lists and can be used for per-device filtering and MSS clamping. Find it in Settings → Device Filtering. ([#185](https://github.com/DanielLavrushin/b4/issues/185))
- ADDED: **Fake payload from domain** — new option in `TCP` fake settings to generate a fake TLS handshake from any domain you type (e.g. `example.com`).
- REMOVED: **Separate device alias file** — device names are now part of the main config. The old `mac_aliases.json` file is no longer used (aliases are migrated into the config on upgrade).
- FIXED: **Traffic routing fails on Keenetic routers** — routing table IDs could be generated above `255`, which is not supported on systems using `BusyBox`. Table IDs now stay within the safe range.
- FIXED: **Fake SYN used the wrong payload** — when `Syn Fake` was enabled, the fake `SYN` packet always used a built-in payload instead of the one selected in the set (custom, captured, or domain-generated).
- FIXED: **GeoSite routing not working with local DNS proxies** — when the router forwards DNS through a local proxy like `https-dns-proxy`, domains from `GeoSite` categories were not added to routing sets. B4 now intercepts DNS queries earlier so routing works in these setups.
- FIXED: **Log level "Error" reverted to "Info" after restart** — when the log level was set to `Error` in the UI, it was silently changed back to `Info` on the next start.
- FIXED: **MTProto images and files not loading** — Telegram uses extra "CDN" data centers (like DC 203) for media that B4 did not know about, so some pictures and files failed to load. B4 now fetches the current Telegram data center list on start (and on demand from Settings → MTProto Proxy → "Refresh Telegram DC list"). ([#190](https://github.com/DanielLavrushin/b4/issues/190))
- IMPROVED: **Discovery groups domains more consistently** — when one strategy works for all tested domains, they are shown as one group and can be applied together in a single click, instead of being split into separate groups.
- IMPROVED: **RST protection catches more fake resets** — added extra checks that help tell real resets from injected ones, so fewer connections are killed by DPI.
- FIXED: **Packet handling on routers with extra firewall marks** — when other services (VPN, QoS, policy routing) set their own marks on packets, B4 did not recognize its own mark. This could cause queued packets to loop and, when traffic routing was enabled, break masquerade on VPN interfaces.
- ADDED: **Connections page — Aggregated view** — new default view groups packets by device, protocol and domain (or IP when no domain is seen), with an activity chart per group and a side list of devices you can click to filter. The old table is still available under the "Raw feed" tab.
- ADDED: **Floating save button in set editor** — a save button now stays in the bottom-right corner while editing a set, so you can save without scrolling back to the top.
- ADDED: **Reset config to defaults is back** — the reset button in Settings → Core Controls has been reworked and returns. Your sets, web server settings, and geo file paths are preserved so the UI stays reachable.

## [1.48.1] - 2026-04-05

- ADDED: **UDP Reject mode** — new option for QUIC/UDP handling that sends an ICMP "Port Unreachable" response instead of silently dropping packets. Clients fall back to TCP almost instantly instead of waiting for timeouts.
- FIXED: **Duplicate devices in device list** — when a device changed its IP address, both old and new IPs appeared as separate entries. Now only the current IP is shown.

## [1.48.0] - 2026-04-05

- ADDED: **Config backup before update** — B4 now automatically saves a copy of your config file before updating (e.g. `b4.json.bak.v1.47.2`), so you can restore it if needed.
- ADDED: **Backup reminder in update dialog** — a warning in the update dialog reminds you to download a full backup from Settings → Backup before updating.
- ADDED: **Watchdog** — background service that monitors configured domains and automatically finds a working bypass when DPI starts blocking them. Add domains or URLs to monitor, and watchdog periodically checks connectivity. After consecutive failures, it runs a quick discovery, applies the first working config, and resumes monitoring. Multiple failed domains are healed together in a single discovery run. Domains with the same working strategy are grouped into one set. Configure intervals, retries, and cooldowns in Settings > Discovery. Live status on the new Watchdog page.
- ADDED: **IP block detection** — B4 can now detect when a destination IP is blocked entirely (not just by domain name). When detected, B4 immediately resets the connection so your device retries faster on a different server instead of waiting for a timeout.
- ADDED: **RST injection protection** — B4 can now detect and drop fake TCP RST packets injected by DPI systems to kill your connections. Enable per set in TCP settings. Uses three independent checks: TTL fingerprint mismatch, RST arriving before any server response, and multiple RSTs on the same connection.
- IMPROVED: **Fake strategy settings shown only when relevant** — Sequence Offset now only appears for `Past Sequence` and `Random Sequence` strategies, Timestamp Decrease only for Timestamp strategy. Reduces clutter in the UI.
- FIXED: **Error when manual IP overlaps with GeoIP** — adding an IP address manually that already exists in a `GeoIP` category within the same set caused a firewall error. Duplicate IPs are now filtered automatically.
- FIXED: **Traffic routing stops working after ~1 hour** — `GeoIP` addresses and static IPs were added to firewall sets with a `TTL` and silently expired. They are now permanent. DNS-resolved IPs are periodically refreshed before they expire.

## [1.47.2] - 2026-04-01

- IMPROVED: **Discovery tests multiple domains in parallel** — when checking several URLs, discovery now tests all domains at the same time instead of one by one, making it noticeably faster.

## [1.47.0] - 2026-03-31

- FIXED: **Routing breaks when VPN/WireGuard restarts** — B4 now automatically detects when a network interface changes and refreshes routing rules. Previously, restarting WireGuard (or any VPN) while B4 was running would silently break routing until B4 was restarted.
- IMPROVED: **Discovery no longer interrupts normal traffic** — discovery now runs on its own isolated flow, so your internet connection stays unaffected while discovery is testing strategies.
- IMPROVED: **Stopping discovery is now reliable** — cancelling a running discovery now properly cleans up all firewall rules and stops immediately.
- IMPROVED: **Discovery results grouped by strategy** — when multiple domains share the same bypass strategy, they are shown as one group instead of separate cards. One click applies the config to all domains at once.
- IMPROVED: **Discovery UI redesigned** — cleaner layout with responsive grid for large screens, simplified logs (button + modal instead of inline panel), and expandable per-domain details during live testing.
- IMPROVED: **Removed confusing speed numbers from discovery** — raw MB/s values that users mistook for browsing speed are replaced with ranked preset lists and improvement percentages.
- FIXED: **Discovery history always showed "just now"** — timestamps were not saved correctly, so all history entries appeared as "just now" regardless of when they ran.
- CHANGED: **Swagger UI moved to documentation site** — the embedded Swagger UI has been removed from the binary to reduce its size (~27MB → ~13MB). API documentation is now available at [docs.b4core.app/swagger](https://docs.b4core.app/swagger) with the ability to connect to a live B4 instance for testing. The `/swagger/` endpoint now redirects to the documentation site.
- ADDED: **System diagnostics in UI** — new "System Info" button in Settings shows full system diagnostics: OS, kernel, memory, network interfaces, firewall status with NFQUEUE test, kernel modules, installed tools, geodata status, storage, and all B4 paths. Copy as JSON for easy sharing.
- REMOVED: **Config reset button** — the "Reset to defaults" function has been removed from Settings.
- CHANGED: **Empty configuration sets allowed** — B4 no longer forces a default empty set to exist. You can now delete all sets, and the Sets page shows a clean empty state with a "Create Set" button.
- IMPROVED: **Config file is now compact** — only settings you changed are saved to the config file. Default values are no longer written, making the file much shorter and easier to read.

## [1.46.6] - 2026-03-26

- FIXED: **B4 fails to start with large configs** — configs with many sets and hundreds of thousands of IPs caused an error on restart because all IPs were inlined into a single firewall command.
- FIXED: **Routing skips IP ranges** — when IP ranges (e.g. `10.0.0.0/24`) were added to a set's target IPs, routing silently ignored them and only routed individual IPs. Now all IP ranges are routed correctly.
- FIXED: **Update from UI not working** — updating from the web interface appeared successful but the version stayed the same. Now updates from UI work correctly on all devices.
- FIXED: **Pre-release versions shown as downgrade** — selecting a pre-release version (e.g. `1.46.6rc`) in the update dialog incorrectly showed "Downgrade" instead of "Upgrade".

## [1.46.5] - 2026-03-23

- FIXED: **Domain-based routing not working with local DNS** — on routers running their own DNS (e.g. `dnsmasq` on `OpenWrt`), B4 couldn't learn which IPs belong to routed domains. Now it works correctly regardless of where DNS is handled.
- FIXED: **Routing stops working after toggling it off and on** — re-enabling routing on a set required waiting for fresh DNS traffic before it would take effect. Now B4 immediately resolves the domains and populates the routing table.
- FIXED: **Tables monitor not detecting missing DNS routing rule** — the firewall monitor now checks that the OUTPUT chain DNS queue rule is in place, and triggers re-setup if it gets removed.

## [1.46.0] - 2026-03-22

- ADDED: **MTProto proxy** — built-in Telegram `MTProto` proxy with fake-TLS obfuscation. Telegram traffic is wrapped in TLS that looks like a normal HTTPS connection. Configure in Settings with a Fake SNI domain and generated secret, then paste the secret into Telegram's proxy settings.
- ADDED: **DC relay** — when Telegram DCs are blocked by IP, route through an external relay.
- ADDED: **SOCKS5 first-packet fragmentation** — the SOCKS5 proxy now splits the first data packet for matched connections, improving bypass for non-TLS protocols.
- IMPROVED: **Non-TLS traffic handling** — learned IP matches now keep fragmentation and desync active even when traffic is not TLS.

## [1.45.1] - 2026-03-21

- ADDED: **API documentation** — interactive Swagger UI is now available at `/swagger/` to browse and test all REST API endpoints.

## [1.45.0] - 2026-03-21

- ADDED: **Traffic routing** — you can now route traffic for matched domains through a specific network interface. When a set matches a domain, B4 resolves its IPs and directs that traffic through the chosen output interface. Configure it in the `Routing` tab when editing a set.
- ADDED: **Position randomization** — SNI split position, OOB position, and TLS record position now support ranges. Each connection picks a random value within the range, making traffic harder for DPI to fingerprint.
- ADDED: **Strategy randomization pool** — you can now select multiple splitting strategies and B4 will randomly pick one per connection. Constantly changing how packets are split confuses stateful DPI.
- ADDED: **Combo/Disorder timing ranges** — First Segment Delay, Jitter Max, and Fakes Per Segment now support ranges. Each connection gets different timing and fake packet counts, preventing DPI from building a consistent traffic fingerprint.
- IMPROVED: **Fake packets now match real connection fingerprint** — fake desync packets (RST, FIN, ACK) now preserve the original TCP options (timestamps, window scale, SACK) instead of stripping them. This prevents DPI from detecting fakes by comparing TCP header fingerprints.
- IMPROVED: **Dynamic fake TTL** — fake packet TTL is now clamped to never exceed the real packet's TTL, preventing impossible TTL values that DPI systems use to identify forged packets.
- IMPROVED: **Dual fake-packet evasion** — fake packets now use both corrupted checksums and dynamic TTL together, making them harder for DPI to accept while ensuring the real server drops them.
- IMPROVED: **TLS info shown on all packets** — domain name and TLS version now appear in logs for every packet in a connection, not just the first one.
- IMPROVED: **Managing large domain/IP lists** — added a bulk text editor for domains and IPs, and long lists now collapse with a "+N more" button.
- FIXED: **Payload handling** — fixed incorrect filenames on upload/download and unnecessary loading errors when sets are disabled.
- FIXED: **Syslog crashes B4 in Docker** — enabling syslog in a `Docker` container caused B4 to crash-loop because the syslog socket doesn't exist. Now B4 logs a warning and continues without syslog.
- FIXED: **Time zone not applying on routers** — setting a time zone in `Settings` had no effect on some routers (e.g. `Keenetic`) because the device lacked timezone data.
- FIXED: **Log level resets to Info on restart** — changing the log verbosity in `Settings` did not persist across service restarts.
- FIXED: **Invalid TLS certificate crashes B4** — setting a wrong certificate or key path in `Settings` caused B4 to crash on next restart. Now it logs a warning and falls back to `HTTP`.
- IMPROVED: **ASN data now persists on the router** — enriched ASN information is stored server-side, so it stays consistent across all browsers and devices.
- ADDED: **Inline ASN enrichment** — you can now enrich destination IPs with ASN data directly from the connections table without opening the Add IP dialog.

## [1.44.1] - 2026-03-15

- ADDED: **Time zone setting** — log timestamps now use your system's local time by default instead of UTC. You can also pick a specific time zone in Settings > Logging.
- FIXED: **Payload download not working** — clicking "Download" on a captured payload in the web UI did nothing. Now downloads work correctly.
- FIXED: **Uploaded payload filename** — when uploading a payload file, the saved name included the file extension twice (e.g. `tls_payload_bin.bin`). Now it saves correctly (e.g. `tls_payload.bin`).
- FIXED: **Update from web UI not applying** — clicking "Update" in the web interface would appear to run, but B4 would restart with the same version.
- FIXED: **DPI bypass not working with TLS version set to "Any"** — when a set's TLS version filter was set to "Any", connections would break after the initial handshake. Selecting a specific version (1.2 or 1.3) worked fine. The issue was that encrypted data packets were being incorrectly processed as if they were new TLS handshakes.
- FIXED: **B4 crashing on some Xiaomi routers (BE7000, AX3200 and similar)** — B4 would fail to start with a "Could not process rule" error because it incorrectly chose nftables on devices where nftables isn't fully supported. B4 now tests whether nftables actually works before using it, and automatically falls back to iptables-legacy when needed. (issues [#132](https://github.com/DanielLavrushin/b4/issues/132) [#133](https://github.com/DanielLavrushin/b4/issues/133))
- ADDED: **Firewall engine selector** — a new "Firewall Engine" option in Settings lets you manually choose between `nftables`, `iptables`, or `iptables-legacy` if auto-detection doesn't work for your device.

## [1.43.0] - 2026-03-14

- ADDED: **Language selection** — you can now switch the web interface language in Settings. English and Russian are available.
- FIXED: **Update stuck on "Waiting for service to restart"** — when login protection was enabled, the update process would get stuck polling forever after the service restarted.
- FIXED: **Excessive logging under heavy traffic** — on busy routers, packet queue overflows could flood the log with repeated error messages, wasting CPU and potentially making the situation worse. These messages are now rate-limited.
- FIXED: **TLS version not shown for some UDP connections** — QUIC (UDP) connections would sometimes show a blank TLS version in the log and connections table, even though they always use TLS 1.3. Now all QUIC traffic correctly displays its TLS version.

## [1.42.1] - 2026-03-13

- FIXED: **B4 not capturing traffic on older routers** — on some routers with older kernels (e.g. ASUS RT-AC68U), B4 would start but show no connections. The fix ensures B4 properly registers itself with the system's packet filtering, so traffic is captured without needing any workarounds.
- FIXED: **Copy JSON not working on Safari/Mac** — the "Copy JSON" button in Import/Export now works reliably on Safari and other browsers that restrict clipboard access.

## [1.42.0] - 2026-03-11

- ADDED: **Backup & Restore** — you can now download a full backup of your B4 configuration and restore it later. Go to Settings > Backup to download a `.tar.gz` file with your config, discovery history, detector history, device aliases, and captured payloads. To restore, just upload a previously downloaded backup and restart B4.
- ADDED: **Web UI login protection** — you can now set a username and password to protect the B4 web interface. Useful when B4 is installed on a VPS or accessible from the internet. Set it up in Settings > Web Server > Authentication, or during installation. When enabled, a login page appears before you can access the dashboard.
- ADDED: **TLS version filter** — you can now assign a set to only handle TLS 1.2 or TLS 1.3 traffic. This is useful when the same domain uses different TLS versions on different devices (e.g., a smart TV using TLS 1.2 vs a browser using TLS 1.3) and each needs a different bypass strategy. Set the filter in `Targets > TLS Version Filter`. Leave it on "Any" to match all traffic as before.
- ADDED: **TLS version in Connections table** — the connections log now shows the detected TLS version (1.2 or 1.3) as a badge next to the domain name, so you can see which version each connection uses.
- ADDED: **Discovery sets TLS filter automatically** — when you run Discovery with a specific TLS version (TLS 1.2 or TLS 1.3), the resulting set will automatically have the matching TLS filter applied.
- IMPROVED: **Import/Export now shows a clear success message** — after importing a set configuration, a green banner confirms the import worked and reminds you to save. Previously, the page would silently switch to another tab, which was easy to miss.

## [1.41.0] - 2026-03-09

- ADDED: **DPI Detector results are now saved** — after running detection tests, your results are saved and shown in a "Previous Results" section. You can expand any past run to see full details, or delete individual entries.
- ADDED: **DPI Detector remembers your test selection** — the test toggles (DNS, Domains, TCP, SNI) are now remembered between visits.
- ADDED: **Discovery results are now saved** — when Discovery finishes testing a domain, the results are kept on the server. Refreshing the page or coming back later will show your previous results in a "Previous Results" section at the bottom.
- ADDED: **Auto-reconnect to running Discovery** — if you refresh the page while Discovery is running, it automatically reconnects and shows progress. No more lost sessions.
- ADDED: **Use strategies from past results** — you can apply a working strategy from a previous Discovery run without re-testing. Just expand the domain and click "Use This Strategy".
- ADDED: **Re-test and manage history** — each past result has a "Re-test" button to quickly run Discovery again for that domain. You can also delete individual results or clear all history.
- ADDED: **Duplicate domain warning** — when adding a domain or IP to a set, B4 now warns you if it already exists in another set (including domains inside GeoSite categories). This helps avoid accidental overlaps between sets.
- FIXED: **Wrong set assigned to traffic** — in rare cases, traffic could be matched to the wrong set when multiple sets had overlapping IP ranges (e.g., a set with specific IPs and another with broad geo-IP categories like "cloudflare"). The most specific match now always wins. Also, subsequent TCP packets now correctly use the set that was previously identified by domain name, instead of falling back to a less accurate IP-only match.
- FIXED: **DNS check no longer gives false "poisoned" results** — if your router uses DoH, DNSCrypt, or another encrypted DNS, Discovery would incorrectly report DNS as poisoned (because IPs didn't match exactly). Now it actually checks whether the IPs work, not just whether they match.
- FIXED: **ISP block pages no longer count as "success"** — previously, if the ISP returned a block page (with valid HTML), Discovery could mistakenly think the bypass worked. Now it detects Russian ISP block pages and correctly marks them as failed.
- FIXED: **Discovery hangs after detecting DNS poisoning** — when DNS bypass strategies (fragmented queries, alternative servers) got no response, the lookup had no timeout and would wait forever. Now properly times out after 10 seconds per attempt.
- IMPROVED: **Discovery is much faster** — removed unnecessary DNS lookups on every test, added early exit when a strategy clearly doesn't work, and limited IP fallback retries. Failed presets now take ~5s instead of ~35s.
- IMPROVED: **IP-blocked domains detected and skipped** — when a domain is completely blocked at the network level (like Instagram in some regions), Discovery now detects this early, shows a "Blocked" badge, and skips the extended search instead of wasting time testing 100+ strategies that can't work.
- IMPROVED: **Clearer error messages** — instead of raw technical errors like "context deadline exceeded", you now see human-readable messages like "connection timed out" or "connection reset by DPI/firewall".
- IMPROVED: **Better firewall error messages** — errors now show which rule failed and why, instead of just "exit status x".

## [1.40.0] - 2026-03-08

- ADDED: **Mass delete sets** — you can now select multiple sets at once and delete them all together. Click the "Select" button in the toolbar, check the sets you want to remove, then click "Delete". Includes "Select All" for quick bulk cleanup.
- ADDED: **Multidisorder mode** — sends fake overlap packets before every real segment (not just the first one), flooding DPI with garbage data so it can't reassemble your traffic correctly. Enable "Fake Per Segment" in Combo or Disorder fragmentation settings and choose how many fakes to send per segment.
- ADDED: **New fake payload types** — two new options in Faking settings: "All Zeros" (sends empty-looking data) and "Inverted Original" (sends a flipped copy of the real data). Some networks respond better to these than the default payloads.
- ADDED: New Discovery presets for multidisorder mode — Discovery can now automatically test these new techniques when searching for the best bypass configuration.
- ADDED: **Upload GeoIP/GeoSite files** — you can now upload `.dat` files directly from your computer using the "Upload" button in `Settings > Geo Databases`.
- ADDED: **TCP Port Filter** — B4 no longer only captures TCP port 443. You can now configure custom TCP ports per set (e.g., `80,5222,8000-9000`) in the TCP settings tab, just like UDP. Port 443 is always included. Firewall rules, packet processing, and the monitor all update automatically — no restart needed. Useful for services like Telegram (port 5222), WhatsApp (5222-5223), Signal (4433), XMPP, and others that use non-443 TCP ports.
- CHANGED: **Main Set removed** — there is no longer a special "main" set. All sets are now equal and independent. Your existing main set will be converted into a regular set automatically.
- CHANGED: **Connection Bytes Limits moved to Settings** — TCP and UDP connection bytes limits are now in `Settings > Core > Queue Settings` instead of being tied to a specific set.
- IMPROVED: **Device list sorting** — devices are now sorted alphabetically by name, with selected devices always shown at the top for easier access.
- FIXED: **Set Import not working on Android** — pasting a set configuration from the clipboard was not possible on Android devices.
- IMPROVED: **Set Import/Export** — the exported JSON is now much shorter and easier to read. Only settings you actually changed are shown; everything else is left out since it uses defaults. A `b4_version` tag is included so you can tell which B4 version a shared set was made with.
- IMPROVED: **Simpler import flow** — pasting a set JSON now applies it immediately (no more forgetting to click "Apply"). Added Copy and Paste buttons for quick sharing.
- IMPROVED: **Lower memory usage** — fixed several memory leaks and added automatic memory management.
- FIXED: **Network interface not visible on MikroTik** — `veth` interfaces (used by MikroTik containers) were incorrectly hidden. They now show up properly in the interface list.

## [1.39.1] - 2026-03-02

- ADDED: **MSS Clamping** — forces smaller packet sizes at the firewall level so that blocked content (like YouTube on smart TVs) can load correctly. Two options: enable **globally** for all devices in `Settings > Network > Global MSS Clamping`, or set a **per-device** size in the `Settings > Device Filtering` table (MSS column). Changes apply instantly without restarting.
- ADDED: **DPI Detector** — a new page in the sidebar that checks whether your ISP is tampering with your internet traffic. It runs three quick tests: DNS spoofing, blocked website detection, and connection dropping. Helps you see what your ISP is actually doing before and after enabling B4.
- ADDED: **NAT Masquerade** — B4 can now set up NAT masquerade automatically when running inside containers (Docker, LXC, MikroTik CHR). No more manual scripts — just enable `NAT Masquerade` in `Settings > Feature Flags > Firewall Features` and optionally pick an output interface. Works with both `iptables` and `nftables`. Rules are monitored and auto-restored if they disappear. Also available via CLI: `--masquerade` and `--masquerade-interface`.
- FIXED: **Custom payloads ignored during Discovery** — selecting custom payloads no longer silently falls back to built-in ones like duckduckgo. Your custom payloads are now properly used in the discovered configuration.
- FIXED: **Discovery results not working after adding** — configurations that passed during Discovery could fail when actually applied. The tested config now matches exactly what gets saved.
- FIXED: **TTL detection not working** — the optimal TTL search was not actually changing the fake packet's TTL, making all attempts look the same. Now correctly finds the minimum working TTL for your network.
- FIXED: **Custom capture payloads lost when adding from Discovery** — configurations using captured payload files (e.g., from the `Capture feature`) would lose the payload reference after being added, causing the bypass to fail. The payload type also didn't show correctly in the set editor until manually reselected. Both are now fixed.
- ADDED: **Discovery Cache** — Discovery now remembers which bypass strategies worked before. When you run Discovery for a new domain, it tries previously successful configurations first, so you often get a working result quicker.
- ADDED: **Multi-Domain Discovery** — you can now test multiple domains in a single Discovery run. Add domains or full URLs one by one (they appear as chips for easy management) and B4 will find the best bypass configuration for each one.
- IMPROVED: **Smarter Discovery** — reworked strategy testing to use real-world technique combinations instead of testing individual tricks in isolation. If Discovery says a strategy works, it should actually work when you add it.
- IMPROVED: **Smarter TTL in Discovery** — `Discovery` finds the optimal Fake TTL for Combo configurations automatically by scanning through preset TTL values, and tests all faking strategies (including `timestamp`) to pick the most reliable one.
- IMPROVED: **Fullscreen Discovery Logs** — added a button to view Discovery logs in a large popup window, making long log lines much easier to read.
- IMPROVED: **Set editor no longer jumps away after saving** — clicking "Save" now keeps you on the same tab and scroll position instead of going back to the sets list. This can help to speed up testing specific configurations.
- IMPROVED: **Auto-detect config file** — the `--config` flag is no longer required. When omitted, B4 automatically looks for a config file in `/etc/b4/` and `/opt/etc/b4/`. If no config exists yet, B4 picks the best default location and creates one on first run.
- IMPROVED: SOCKS5 updates (thanks @remmody [#PR64](https://github.com/DanielLavrushin/b4/pull/64))
- IMPROVED: **Device Discovery** — now works on all routers. B4 reads the system ARP table instead of DHCP lease files, so devices show up regardless of your router brand (Keenetic, MikroTik, OpenWrt, Asus, etc.). Device hostnames are still picked up from DHCP when available.
- REMOVED: **Decoy SNI Domains** setting from Combo strategy — the decoy packet now uses the same fake payload configured in your Faking settings instead of a separate list of domain names.

## [1.38.0] - 2026-02-27

- CHANGED: **Vendor Lookup is now optional** — the ~6MB device manufacturer database is no longer downloaded at startup. Enable it in `Settings > Device Filtering > Vendor Lookup` if you want to see device brand names.
- ADDED: **TLS Version selector in Discovery** — you can now choose which TLS version (`Auto` / `TLS 1.2` / `TLS 1.3`) to use when probing.
- ADDED: **SOCKS5 Proxy** — B4 now includes a built-in SOCKS5 proxy server. Apps like browsers, curl, or torrent clients can route traffic through B4 without any system-wide setup. Enable it in `Settings > Network Configuration > SOCKS5 Server`. Supports optional username/password authentication. ([#48](https://github.com/DanielLavrushin/b4/pull/48), thanks [@remmody](https://github.com/remmody))
- IMPROVED: **Docker support** — detects Docker environment and shows `docker pull` instructions instead of the update button.
- FIXED: **Settings page crash (white screen) in container environments** — network interface filtering is now container-aware, showing `veth` and other container interfaces when running inside Docker/MikroTik. ([#44](https://github.com/DanielLavrushin/b4/issues/44), thanks [@kakosmakos](https://github.com/kakosmakos))
- FIXED: **DNS check incorrectly reporting "DNS poisoned"** when everything is actually fine. The check could time out before even trying the system resolver, producing a false alarm. Also improved handling of CDN domains (YouTube, Google, etc.) where different resolvers return different but equally valid IPs. ([#46](https://github.com/DanielLavrushin/b4/issues/46))

## [1.37.0] - 2026-02-26

- ADDED: **HTTPS/TLS support** for the web interface ([#40](https://github.com/DanielLavrushin/b4/pull/40), thanks [@Shiperoid](https://github.com/Shiperoid)). Configure in Web UI (`Settings > Network > Web Server`) or in the config JSON. The installer auto-detects router certificates on **OpenWrt** and **Asus Merlin** and offers to enable HTTPS during installation.
- IMPROVED: **Reset Statistics** button on the dashboard — clears all counters without restarting the service. Replaces the redundant restart button (still available in `Settings > Core Controls`).
- FIXED: **Geo database download failing with 500 error** on routers with limited storage. Improved disk space handling and error reporting.

## [1.36.0] - 2026-02-16

- IMPROVED: **Connections Table** — moved the streaming/paused control from the top control bar to a floating play/stop button in the bottom-right corner of the table for a cleaner UI.
- IMPROVED: **Set Editor Redesign** — simplified the tab layout from 7 tabs down to 5. `Fragmentation` and `Faking` tabs are now part of the `TCP` section where they belong, since both techniques operate on TCP traffic.
  - **TCP** tab now has 3 inner tabs: `General` (connection limits, timing, duplication), `Splitting` (all packet splitting strategies), and `Faking` (all evasion techniques in one place).
  - `Faking` groups related settings into collapsible sections (Fake SNI, SYN Fake, Desync, Window Manipulation, Incoming Response Bypass, ClientHello Mutation) — each section shows its current status at a glance so you can see what's active without opening it.
  - `UDP`, `DNS`, `Targets`, and `Import/Export` tabs remain unchanged.

## [1.35.2] - 2026-02-16

- FIXED: All changes being lost and tabs resetting when creating a new `set`. Any edit (switching tabs, adding categories, changing settings) could be randomly undone.
- FIXED: Custom TCP window values (add/remove) not being saved when using `Oscillate` or `Random` window modes.
- FIXED: `Packet Duplication` connections showing incorrect data in the connections table.

## [1.35.0] - 2026-02-15

- IMPROVED: **Geo Settings** - `GeoSite` and `GeoIP` databases can now be downloaded independently from different sources. You no longer need both files — pick only what you need. Added [b4geoip](https://github.com/DanielLavrushin/b4geoip) as a built-in source option.
- ADDED: **Packet Duplication** - bypass ISP throttling that works by randomly dropping packets to specific IP ranges (e.g. Telegram subnets). When enabled, B4 sends multiple copies of each outgoing packet so your connection survives even when the ISP drops some of them. Create a separate set with the target IPs, go to TCP settings, and enable Packet Duplication. Set the copy count (2-5 is usually enough). Note: this replaces all other DPI bypass for that set and uses extra bandwidth.
- FIXED: Some connections to matched domains intermittently showing as "not matched". This was caused by an internal cache issue and learned IP associations being lost when saving settings or updating geo databases.

## [1.34.0] - 2026-02-10

- ADDED: **Randomized Segment 2 Delay** - instead of a fixed delay between TCP/UDP segments, you can now set a min–max range. Each packet picks a random delay within your range, making your traffic look more natural and harder for DPI to fingerprint. If both values are the same, it works exactly like before.
- ADDED: Redesigned `Dashboard` - the dashboard now shows what actually matters:
  - **Device Activity** - see which devices on your network are connecting to which domains (e.g., your PlayStation, iPhone, or laptop), with connection counts per domain.
  - **Domains Not In Any Set** - quickly spot domains that aren't covered by any bypass set yet, with one-click "Add to set" buttons.
  - **Bypass stats** - at a glance, see total connections, how many are being bypassed, and current throughput.
  - **Active Sets** overview with clickable chips to jump to set configuration.
- ADDED: The dashboard now tracks all network traffic, not just bypassed connections. This means device activity and domain lists populate even before you configure any sets.
- IMPROVED: `Set editor` now opens as a full page instead of a popup window, giving you much more space to work with when configuring your bypass sets.
- ADDED: `TCP Timestamp` faking strategy - a new way to make fake packets look wrong to the real server (so it ignores them) while still fooling DPI. Instead of using a low TTL or wrong sequence number, B4 sends fake packets with an outdated timestamp. Inspired by the [youtubeUnblock](https://github.com/Waujito/youtubeUnblock) project. Select `TCP Timestamp` in the Faking strategy dropdown to use it.
- FIXED: UDP/QUIC traffic stopping to match after some time. B4 now keeps server-to-domain associations active as long as traffic is flowing, preventing the "works at first, then stops" issue.
- IMPROVED: Rewrite TLS `ClientHello` payload generation.

## [1.32.0] - 2026-01-19

- ADDED: Validation tries setting in `Discovery` - require multiple successful connections before accepting a configuration as reliable (default: 1, configurable 1-5). Helps filter out unstable bypass methods.
- IMPROVED: UDP/QUIC filtering now correctly matches packets from domain-specified targets. When you add a domain like `youtube.com`, B4 now tracks which IP addresses belong to that domain and properly handles all UDP traffic to those IPs, not just the initial connection.
- IMPROVED: Connection logs now show domain names for `QUIC` traffic even when not actively filtering those connections.
- FIXED: UDP port filtering incorrectly matching unrelated traffic. Previously, specifying port 443 for one service could accidentally match all UDP traffic on port 443, including other services.

## [1.31.2] - 2026-01-16

- FIXED: Custom payload files not working in `Discovery` feature - old configurations with relative paths like `captures/payload.bin` now work correctly.

## [1.31.0] - 2026-01-11

- ADDED: TCP MD5 option support.
- ADDED: TCP MD5 preset in Discovery - automatically tests `TCP MD5` bypass strategy during configuration discovery.
- ADDED: `iptables` multiport module support - improves firewall rule efficiency when filtering multiple ports. The installer now detects and uses the `multiport` extension when available.
- IMPROVED: Import/Export now automatically migrates old set configurations (pre-v1.29) to the current format when importing.

## [1.30.1] - 2026-01-02

- FIXED: White screen when importing old set configurations (sets saved before v1.29 now automatically convert to current format).

## [1.30.0] - 2026-01-02

- ADDED: Custom payload support in `Discovery` - test bypass strategies using your own captured TLS payloads instead of built-in defaults.
- FIXED: Crash during `Discovery` when checking status (e.g. clicking "Create Set" or refreshing page while discovery is running. Reported by `Andrew B.`).

## [1.29.1] - 2026-01-01

- ADDED: Incoming response bypass - defeats TSPU throttling that blocks downloads after ~15KB. I nmost cases - select TCP incoming `fake` for all strategies.
- FIXED: Discovery progress bar sometimes showing over 100%.

## [1.28.1] - 2025-12-30

- ADDED: Web server bind address setting - control which network interface the web UI listens on (e.g., `127.0.0.1` for localhost-only access, `0.0.0.0` for all interfaces). Supports `IPv6`.
- ADDED: Added `Skip DNS` toggle in `Discovery` - useful when you know DNS isn't blocked and want faster results.
- ADDED: Support for `MIPS` devices with soft float.
- ADDED: Post-ClientHello RST injection - sends fake connection reset after the initial handshake to confuse DPI systems that track connection state. Enable with `Post-ClientHello RST` toggle in TCP Desync Set settings.
- IMPROVED: `Discovery` now finds the optimal TTL for a specific network, instead of using a fixed value.
- IMPROVED: Removed DPI fingerprinting phase from discovery - it was slow and unreliable. Discovery now starts testing bypass strategies immediately, making the process faster.
- IMPROVED: Shutdown behavior - B4 now waits up to 5 seconds for in-flight packet operations to complete gracefully before terminating.
- FIXED: DPI bypass not working for LAN devices on routers using `nftables` (e.g., OpenWrt with fw4 firewall).
- FIXED: TCP `desync` bypass methods (`rst`, `fin`, `ack`, `combo`, `full` modes) were sending malformed packets, causing them to fail or be ignored. Affects both `IPv4` and `IPv6`.
- FIXED: Restored missing `Packet Mark` setting in Network Configuration - allows customizing the firewall mark used for traffic routing.
- FIXED: Potential connection hangs caused by packets getting stuck in the processing queue without verdicts being set.
- FIXED: Memory leak where packet injection operations could continue running after service shutdown, potentially causing crashes or instability during restart.
- FIXED: Race condition in network interface name caching that could cause incorrect packet filtering decisions on multi-interface systems.
- FIXED: Fragmented IP packets now bypass B4 processing entirely to prevent incomplete packet inspection and potential protocol violations.
- REMOVED: Fragmentation strategy `overlap` — functionality merged into `combo`.
- CHANGED: Fragmentation `combo` now supports decoy packets. When `enabled`, B4 sends a fake `ClientHello` with a whitelisted domain (e.g., ya.ru, vk.com) before sending the real fragmented request. Can be found in Fragmentaiton Tab set settings.

## [1.27.2] - 2025-12-27

- FIXED: Adding multiple services with many UDP ports (Discord, WhatsApp, etc.) could cause `iptables` firewall rules to fail, preventing B4 from starting or restarting properly.

## [1.27.1] - 2025-12-27

- FIXED: DNS poisoning detection should correctly compare IP lists from system resolver vs encrypted DNS, even when IPs are returned in different order.
- FIXED: UDP/QUIC traffic for IP-matched services (like YouTube, Google) could be incorrectly handled when another set had port-based filtering, potentially breaking video streaming.
- FIXED: A bug when downgrading to a previous version from the web UI would report success but stay on the current version.

## [1.27.0] - 2025-12-27

- ADDED: `Discovery Logs` panel shows real-time progress during configuration discovery.
- ADDED: Smart CDN detection for [20+ major services](https://github.com/DanielLavrushin/b4/blob/main/src/discovery/dns.json) (`Instagram`, `Facebook`, `YouTube`, `Twitter/X`, Teleg`r`am, `Discord`, `TikTok`, `Netflix`, `Spotify`, etc). Discovery now uses full IP ranges instead of single DNS results. [View supported services](https://github.com/daniellavrushin/b4/blob/main/src/discovery/dns.json)
- ADDED: Automatic DNS bypass for CDN services. When geo-blocked IPs are detected, B4 uses fragmented DNS queries to external resolvers to find working servers.
- IMPROVED: `Discovery` Logs panel now shows more detailed real-time progress during DPI fingerprinting, DNS checks, and strategy testing.
- IMPROVED: `Discovery` now uses DNS-over-HTTPS (encrypted DNS via `Google`/`Quad9`/`Cloudflare`) to detect when your ISP returns fake IP addresses for blocked sites. When DNS poisoning is detected, B4 connects directly to the real server to continue testing bypass strategies.
- FIXED: `QUIC` traffic (used by `YouTube`, `Google`, and many modern sites) was ignored when custom UDP ports were configured for any target set. Now UDP port `443` is always monitored regardless of other port settings.
- FIXED: UDP traffic to specific IPs now correctly uses the set that defines both the IP and port filter together, instead of being handled by a different set that only matches the port.

## [1.26.3] - 2025-12-26

- FIXED: UDP port ranges (e.g., `50000-50032`) now work correctly on both `iptables` and `nftables` systems. Previously, port ranges could cause startup failures on older devices.
- IMPROVED: Paste multiple domains at once when adding to `Targets` (separated by spaces, commas, or pipes).

## [1.26.2] - 2025-12-25

- FIXED: Normalize UDP port filter format by replacing dashes with colons before creating UDP rules in the `iptables`.
- FIXED: `Discovery` now detects when DPI blocks downloads mid-transfer (e.g., cuts connection after 16KB), potentially preventing false "success" reports.
- FIXED: `Payload` capture always showing "timeout" even when visiting the target site correctly.

## [1.26.1] - 2025-12-24

- ADDED: **Upload Custom Payloads** - upload your own binary payload files instead of capturing from live traffic (avilable in the `Settings` -> `Capture` tab).
- ADDED: **Use Captured Payloads** - new `My own Payload file` option in `Faking` settings lets you use previously captured or uploaded payload binaries.
- ADDED: When adding a configuration from `Discovery`, B4 automatically finds and includes matching geosite categories (e.g., discovering `youtube.com` will also add the `youtube` category with related domains from it).
- ADDED: Custom UDP port filtering - configure specific ports per set using the UDP port filter option, and B4 will automatically listen only on those ports.
- ADDED: Live firewall rule updates - changing UDP ports, connection limits, or other core settings in the web UI now takes effect immediately without restarting the service.
- ADDED: Discovery now accepts full URLs. Paste a complete URL like `https://youtube.com/watch?v=xyz` or `https://cdn.example.com/large-file.js` instead of just a domain name.
- ADDED: Sequence Overlap Pattern - `seqovl`. Configure a custom byte pattern in `Fragmentation` settings that gets mixed into TCP segments to confuse deep packet inspection systems while your real data reaches the server intact. Works with `disorder` and `combo` strategies.
- CHANGED: UDP traffic now only listens on port `443` (QUIC) by default instead of all UDP ports, reducing unnecessary packet processing.
- CHANGED: Device names in Connections tab now only appear when `Device Filtering` is enabled in `Settings`.
- FIXED: B4 no longer crashes on startup when geodat files (`geosite.dat`/`geoip.dat`) were manually deleted.
- FIXED: Re-downloading geodat files now properly reloads all domain and IP targets without requiring a service restart.
- FIXED: `IPv6` bypass settings now work correctly - disabling IPv6 in config actually disables IPv6 packet processing.

## [1.25.4] - 2025-12-21

- FIXED: installer failing to download on `OpenWRT` devices - improved compatibility with minimal `BusyBox` environments.
- FIXED: repeated kernel module errors flooding syslog on `OpenWRT` routers.

## [1.25.3] - 2025-12-21

- FIXED: `nftables` forwarded traffic not working on `OpenWRT` - changed hook from `POSTROUTING` to `FORWARD` to capture packets before NAT.

## [1.25.2] - 2025-12-19

- FIXED: add validation for fake SNI bounds and fallback to TCP fragments in `overlap` handling.
- FIXED: crash on 32-bit ARM devices caused by uint32 to int overflow in `overlap` fragment handling.

## [1.25.0] - 2025-12-19

- REMOVED: `--skip-local-traffic` as it did actually nothing causing connection issues and solving real problem.
- ADDED: Device names in `Connections` - identify which device (phone, laptop, etc.) is making each connection, with optional per-device filtering (whitelist/blacklist) in `Settings`.
- ADDED: DHCP Device names:
  - **Device Names**: See which device (phone, laptop, TV) is making each connection instead of cryptic IP addresses
  - **Device Filtering**: Choose which devices on your network should use DPI bypass (whitelist or blacklist mode)
  - **Custom Names**: Give your devices friendly names in `Settings → Devices`
- ADDED: Advanced `Connections` table filtering:
  - Combine filters with `+` (e.g., `tcp+youtube`)
  - Exclude with `!` prefix (e.g., `!google` or `tcp+!udp`)
  - Field-specific filters: `domain:`, `asn:`, `device:` (e.g., `domain:youtube+!asn:cloudflare`)

## [1.24.0] - 2025-12-17

- ADDED: `--skip-local-traffic` option to exclude router-originated traffic from processing, enabling compatibility with transparent proxies (Xray, Clash, Sing-Box etc.) running on the same device. By default is `on`. Can be found in `Core` settings. Requires service restart when changing.
- ADDED: Network interface filtering — optionally restrict B4 to specific interfaces (e.g., `eth0`, `tun0`). Empty selection = all interfaces. Configurable via UI without service restart (can be found in `Core` settings).
- FIXED: ensure only `Panic` errors are logged into errors.log.

## [1.23.1] - 2025-12-16

- FIXED: crash in `overlap` fragmentation strategy when SNI extends beyond payload bounds (index out of range panic).
- FIXED: `panic` errors not being captured to `errors.log` file.

## [1.23.0] - 2025-12-16

- ADDED: dd error logging functionality with configurable error log file (default is `/var/log/b4/errors.log`) for crash diagnostics.
- ADDED: `ASN` filtering in `Connections` table - filter by ASN name globally or with `asn:` field filter.
- IMPROVED: prevent memory leaks in UI.
- IMPROVED: subordinate sets cannot exceed main set's TCP/UDP `connection byte limits` both iun UI and backend.

## [1.22.1] - 2025-12-09

- FIXED: `Discovery` UI showing "0 of 0 checks" and "NaN%" during DNS detection phase.
- FIXED: `Discovery` completing too fast causing "Failed to fetch discovery status" error.
- FIXED: DNS poisoning detection falsely triggering for CDN domains by comparing IP addresses literally instead of testing actual connectivity.

## [1.22.0] - 2025-12-08

- ADDED: `DNS Redirect` - bypass ISP DNS poisoning by transparently rewriting queries to clean resolvers. Available at set level, allowing per-domain DNS redirect control.
- ADDED: DNS discovery and configuration management with support for custom DNS servers.
- ADDED: `Enter` hotkey to start discovery. ([#5](https://github.com/DanielLavrushin/b4/pull/5)).
- IMPROVED: enhance `Discovery` fragmentation configurations and add new presets for combo and disorder strategies.
- IMPROVED: backup handling for existing binaries during B4 installation (`installer.sh`).

## [1.21.1] - 2025-12-07

- ADDED: TCP `SYN Fake TTL` slider option.
- FIXED: SYN fake packets sent with full TTL when using non-TTL faking strategies (randseq/pastseq), causing fake packets to reach servers and break handshakes.
- FIXED: Config validation incorrectly comparing set ConnBytesLimit against stale MainSet defaults instead of loaded JSON values.
- FIXED: UI bug when ui crashes on creating a set with `Overlap` fragmentation option.

## [1.21.0] - 2025-12-07

- FIXED: Slow set save operations - improve performance.
- ADDED: New `FRAG` strategies designed for modern DPI (TSPU):
  - `Combo` (recommended) - multi-technique: first-byte delay + extension split + SNI split + disorder
  - `Disorder` - sends real segments out-of-order with timing jitter, no fake packets
  - `Overlap` - overlapping TCP segments where second overwrites first (RFC 793 behavior)
  - `Extension Split` - splits TLS ClientHello within extensions array before SNI
  - `First-Byte Desync` - sends 1 byte, delays, sends rest (exploits DPI timeouts)
  - `Hybrid` - evasion strategy combining desync, fake SNI, and disorder techniques

- IMPROVED: Skip private destination IP packets processing.

## [1.20.3] - 2025-12-03

- FIXED: `Discovery` false positives - now detects mid-transfer DPI blocking (throttling, stalls, resource blocking) instead of trusting initial HTTP 200.
- ADDED: `Discovery` network baseline - measures reference domain speed first, requires target to achieve 4KB+ downloaded at 30%+ of baseline speed. Configurable reference domain in `Settings` → `Discovery` (default: yandex.ru).
- ADDED: `Discovery` binary search optimization for `TTL` and fragmentation position parameters - reduces Phase 2 tests from ~50 to ~15 while finding optimal values. Uses fingerprint hints when available.
- IMPROVED: `Sets` editor save button now shows immediate loading feedback with spinner and disabled state to prevent double-submit during slow saves.
- IMPROVED: `Sets` manager now supports drag-and-drop reordering instead of up/down arrows.

## [1.20.1] - 2025-12-03

- FIXED: IPv4 UDP fragmentation using incorrect fragment offset encoding (wrong bit shifts corrupted offset field).
- FIXED: IPv6 IP-level fragmentation not adjusting split position relative to IP payload, causing incorrect fragment boundaries.
- FIXED: IP-level fragmentation (IPv4/IPv6) ignoring Smart SNI Split option - now uses middle_sni when enabled.
- FIXED: QUIC fragmentation (IPv4/IPv6) had inverted ReverseOrder logic - was sending in reverse when disabled and normal when enabled.
- FIXED: OOB fragmentation strategy was sending OOB byte in real packet that reached server, breaking TLS handshake. Now sends OOB as fake packet with low TTL/hop limit that dies in transit - DPI sees garbage, server receives clean data.
- FIXED: `Discovery` service testing all presets with single hardcoded fake SNI payload - if user's DPI only responds to alternate payload, most strategies would incorrectly fail.
- FIXED: OOB seg2 sequence number calculation was incorrect (added +1 for fake byte), causing TCP reassembly failures.
- ADDED: QUIC SNI-aware fragmentation - fragments now split at the actual SNI position within encrypted QUIC payloads, defeating DPI systems that decrypt QUIC Initial packets to inspect SNI.
- ADDED: OOB now supports `tcp_check` and `md5sum` faking strategies for checksum corruption fallback when TTL-based faking is unreliable.
- ADDED: [EXPERIMENTAL] `DPI Fingerprinting` in Discovery - attempts to identify DPI type and blocking method before testing, prioritizing likely-effective strategies based on failure mode analysis. May produce inaccurate results; falls back automatically to full preset scan when fingerprint is inconclusive.
- ADDED: Multiple built-in fake SNI payloads (`FakeSNI1`: google (default, AKA classic), `FakeSNI2`: duckduckgo) - different DPI systems respond to different payloads.
- ADDED: Discovery now auto-detects working payload early and applies it to all subsequent tests; if neither works initially, tests both per-strategy and learns dynamically.
- ADDED: User-selectable fake payload type in Faking settings (Random, Custom, or built-in presets).
- IMPROVED: `Fragmentation` UI - renamed "Middle SNI" to "Smart SNI Split", added visual packet diagram, moved manual position to collapsible advanced section.

## [1.20.0] - 2025-12-03

- FIXED: IPv4 UDP fragmentation using incorrect fragment offset encoding (wrong bit shifts corrupted offset field).
- FIXED: IPv6 IP-level fragmentation not adjusting split position relative to IP payload, causing incorrect fragment boundaries.
- FIXED: IP-level fragmentation (IPv4/IPv6) ignoring Smart SNI Split option - now uses middle_sni when enabled.
- FIXED: QUIC fragmentation (IPv4/IPv6) had inverted ReverseOrder logic - was sending in reverse when disabled and normal when enabled.
- FIXED: OOB fragmentation strategy was sending OOB byte in real packet that reached server, breaking TLS handshake. Now sends OOB as fake packet with low TTL/hop limit that dies in transit - DPI sees garbage, server receives clean data.
- FIXED: OOB seg2 sequence number calculation was incorrect (added +1 for fake byte), causing TCP reassembly failures.
- ADDED: QUIC SNI-aware fragmentation - fragments now split at the actual SNI position within encrypted QUIC payloads, defeating DPI systems that decrypt QUIC Initial packets to inspect SNI.
- ADDED: OOB now supports `tcp_check` and `md5sum` faking strategies for checksum corruption fallback when TTL-based faking is unreliable.
- IMPROVED: `Fragmentation` UI - renamed "Middle SNI" to "Smart SNI Split", added visual packet diagram, moved manual position to collapsible advanced section.

## [1.19.1] - 2025-12-01

- FIXED: New sets with geosite/geoip categories not matching traffic until service restart.
- IMPROVED: Discovery service now tests ~150+ configurations.
- CHANGED: Replace Fragmentation SNI split position Slider field with textbox number field.

## [1.19.0] - 2025-11-29

- ADDED: Filter for configuration sets - search by `name`, `SNI` domains, `geosite` categories, or `geoip` categories.
- ADDED: Compare sets feature - side-by-side diff view showing differences between two configuration sets, grouped by section (TCP, UDP, Fragmentation, Faking, Targets).
- ADDED: `Discovery` now names new sets after the preset configuration (e.g., `tcp-frag-rev-fake`) instead of the domain.
- ADDED: `Discovery` detects similar existing sets and offers to add the domain to an existing set instead of creating a new one.
- ADDED: `Discovery` short-circuits when baseline succeeds - skips optimization phases if no DPI is detected.
- ADDED: `Discovery` shows results progressively as tests complete - users can see working configurations and apply them without waiting for the full scan to finish.
- ADDED: Dedicated Sets API endpoints (`/api/sets`) for CRUD operations - create, update, delete, and reorder sets independently.
- FIXED: Settings tab navigation losing selected tab on page refresh.
- CHANGED: New configuration sets are now added to the top of the list instead of the bottom.
- CHANGED: `Discovery` configuration refactoring component.
- CHANGED: Configuration Sets promoted from Settings submenu to top-level navigation item.
- CHANGED: Set operations (create, edit, delete, duplicate, reorder, enable/disable) now save immediately via API instead of requiring manual "Save Changes".

## [1.18.5] - 2025-11-27

- FIXED: revert back the Fake SNI payload for improved compatibility.

## [1.18.4] - 2025-11-25

- CHANGED: update set default `desync` mode to 'off'.
- FIXED: simplify package handling.

## [1.18.3] - 2025-11-25

- FIXED: `Domains` table not updating with new packets due to React state reference issue.
- FIXED: `Domains` table columns layout being cramped with fixed widths.

## [1.18.2] - 2025-11-25

- ADDED: `ClientHello Mutation` support for `IPv6`.
- FIXED: Discovery presets missing default values for `SNI Mutation`, `TCP Window`, and `Desync` settings.

## [1.18.1] - 2025-11-25

- FIXED: Unable to change `ClientHello Mutation` mode in the Faking settings.

## [1.18.0] - 2025-11-24

- IMPROVED: Overall performance (frontend and backend).
- ADDED: `TCP Window Manipulation` (`--tcp-win-mode`) - sends fake packets with manipulated TCP window sizes to confuse stateful DPI. Modes: `oscillate` (cycling window values), `zero` (zero-window probe attack), `random` (randomized windows), `escalate` (gradually increasing windows).
- ADDED: `TCP Desync Attack` (`--tcp-desync-mode`) - injects fake TCP control packets (RST/FIN/ACK) with low TTL and corrupted checksums to desynchronize DPI connection tracking. Modes: `rst`, `fin`, `ack`, `combo`, `full`.
- ADDED: `SNI Mutation` for ClientHello fingerprint evasion - modifies TLS handshake structure to bypass DPI fingerprinting. Modes: `duplicate` (inject fake SNIs), `grease` (add GREASE extensions), `padding` (add padding extension), `reorder` (shuffle extensions), `full` (all mutations combined), `advanced` (TLS 1.3 features like PSK/key_share).

## [1.17.1] - 2025-11-23

- ADDED: `Out-of-Band` (OOB) data handling with configurable position, reverse order, and character (`--frag=oob`).
- ADDED: `Out-of-Band` (OOB) strategies to `B4Discovery`.
- ADDED: `TLS Record Splitting` fragmentation strategy (`--frag=tls`) - splits ClientHello into multiple TLS records to bypass DPI expecting single-record handshakes.
- ADDED: `SACK dropping` (`--tcp-drop-sack`) - strips Selective Acknowledgment options from TCP headers to force full retransmissions and confuse stateful DPI tracking.
- UPDATED: Fake `SNI` payload now uses TLS 1.3 ClientHello structure with `staticcdn.duckduckgo.com`.
- IMPROVED: `SNI` fragmentation for long domains (>30 bytes). Now splits 12 bytes before SNI end instead of middle, ensuring domain suffixes like `googlevideo.com` are properly fragmented across packets.
- IMPROVED: `Matcher` performance with LRU caching for large geosite/geoip categories (70-90% CPU reduction for sets with big data inside).
- IMPROVED: `Geodat` download workflow - files now immediately available in sets manager without restart, config auto-reloads after download.
- IMPROVED: Set `Fragmentation` tab refactored.
- FIXED: Logs level can be switched witout reloading the app.
- FIXED: Config validation bug where Main Set was compared against itself, causing startup failure with `TCP ConnBytesLimit greater than main set` error.
- FIXED: update default fake SNI payload to use new format.
- CHANGED: Renamed `--frag-sni-reverse` to `--frag-reverse` and update related configurations.

## [1.16.1] - 2025-11-20

- ADDED: Asynchronous packet injection for TCP and UDP traffic. Verdict is now sent to kernel immediately, with packet manipulation performed in parallel. Eliminates kernel queue blocking that previously caused video streaming hangs and site loading delays.
- FIXED: Critical performance bottleneck where each QUIC/UDP packet with default configuration (FakeSeqLength: 6) would block the kernel for 6ms minimum. This caused YouTube and other video services to buffer or hang intermittently.
- FIXED: IPv6 QUIC packet processing incorrectly used TCP delay settings instead of UDP delay settings.
- FIXED: New configuration sets created from the Domains page were not saving custom names, defaulting to generic "Set 1/2/3" names instead.
- IMPROVED: Removed unnecessary `1ms` sleep delays when `Seg2Delay` is set to `0`, reducing packet processing latency by up to `6ms` per QUIC packet.

## [1.16.0] - 2025-11-17

- ADDED: Configuration sets can now be enabled/disabled without deletion.
- ADDED: Clear button next to the IP/CIDR list in the set configuration.
- ADDED: Download `GeoSite`/`GeoIP` database files directly from Settings UI with preset sources.
- IMPROVED: Redesigned `/test` page UX - domains are now managed directly on the test page.
- IMPROVED: Refactor `Discovery` presets generation logic and add new test strategies.
- FIXED: Resolved severe performance bottleneck on `/domains` page when adding ASN filters (caused by expensive ASN lookup operations executing on every render).
- REMOVED: Test domain configuration from `Settings` - domains are now managed exclusively on the Test page.

## [1.15.0] - 2025-11-16

- ADDED: SYN fake packet functionality for advanced DPI bypass. Sends fake SYN packets with configurable payload length to confuse DPI systems before the real connection is established. Configure via `--tcp-syn-fake` and `--tcp-syn-fake-len` flags, or through the TCP settings in Web UI.
- ADDED: IP information enrichment via `IPInfo` API integration. When IPInfo token is configured in Settings → API, click on any destination IP in `/domains` monitoring page to view detailed geolocation, ASN, organization, and network information.
- ADDED: `RIPE` [Stat integration](https://www.ripe.net/) for network intelligence. View ASN prefix announcements and detailed network information directly from the Web UI. Helps identify IP ranges for precise targeting.
- ADDED: Configuration set import/export functionality. Share working bypass configurations between devices or users by exporting sets as JSON files. Import proven configurations with one click to quickly replicate successful setups across multiple installations.
- IMPROVED: Discovery test results now include individual configuration cards per domain instead of single recommended configuration, making it easier to analyze which specific settings work best for each target domain.

## [1.14.0] - 2025-11-13

- ADDED: Select target configuration set when adding domains or IP/CIDR addresses from `/domains` monitoring page. Allows precise control over which configuration set receives the new entry.
- ADDED: One-click configuration adoption from `Discovery` test results. Apply the best-performing configuration directly to your configuration list without manual copying.
- CHANGED: Complete overhaul of `Discovery` testing service with improved reliability and performance. Now they should work as expected.
- FIXED: Memory leaks and overall memory management improvements for better long-term stability.
- FIXED: Update process through the Web UI.

## [1.13.0] - 2025-11-10

- ADDED: Click on destination IP addresses in `/domains` monitoring page to add them to configuration. Modal allows adding either exact IP or CIDR notation for broader site coverage. This does not require to reload or restart B4, works on the fly.
- ADDED: Toggle switch in `/domains` monitoring page to view all packets or only those with identified SNI/domain. Useful for monitoring and debugging `UDP` traffic.
- ADDED: Geodat domain/IP counters at configuration sets.
- ADDED: a new tab under `/test` menu. `Discovery` test results now show individual configuration cards per domain instead of a single recommended configuration, making it easier to see what works best for each specific domain.
- CHANGED: `UDP` port filtering now uses a single flexible field instead of separate "from" and "to" fields. Supports comma-separated ports and ranges (e.g., `80,443,2000-3000`).
- CHANGED: Packages count badge in `/domains` menu now only counts packets processed by B4 targets.
- CHANGED: Replaced `--udp-dport-min` and `--udp-dport-max` flags with single `--udp-dport-filter` flag for flexible port filtering.
- CHANGED: Refactored UDP/QUIC packet handling and UDP-related UI tab in the set configuration.
- FIXED: `UDP` entries are now logged even when UDP packets are configured to be ignored in the configuration
- FIXED: `UI` crash when using filter in /domains monitoring page.
- FIXED: Manually added domains no longer require service restart when geodat files are not configured.
- FIXED: Test Suites now correctly report success when DPI is bypassed regardless of `HTTP status code`. Any HTTP response (including non-200 codes) indicates successful DPI circumvention.

## [1.12.0] - 2025-11-09

- ADDED: Configuration Sets - fine-grained bypass control for different targets
  - Create multiple configuration sets, each with independent TCP/UDP/fragmentation/faking settings
  - Target packets by SNI domain, destination IP/CIDR ranges, or UDP port ranges
- ADDED: `geoip.dat` support.

## [1.11.0] - 2025-11-05

- ADDED: DPI Bypass Test feature to verify that circumvention is working. The feature tests configured domains and measures download speeds to ensure B4 is functioning correctly. Visit the `/test` page to run tests and `/settings/checker` to configure test settings (define which domains to test, etc.).
- ADDED: New feature to reset B4 settings to their defaults. The reset button is located in the `Core` tab on the `Settings` page.
- CHANGED: Moved `RESTART B4 BUTTON` to the `Core` tab on the Settings page (under the `Core Controls` section).
- IMPROVED: Enhanced `flowState` struct to track `SNI` detection and processing status.
- FIXED: Service restart functionality in the UI for different service managers (`Entware`/`OpenWRT`/`systemctl`).
- FIXED: Pause shortcut (pressing down the `P` key on the domains and logs pages) interfering with search input.

## [1.10.1] - 2025-11-03

- IMPROVED: Intermittent connection failures where blocked sites would randomly fail to load in certain browsers (`Safari`, `Firefox`, `Chrome`). Connections _should_ now be more stable and reliable across all browsers by optimizing packet fragmentation strategy.

## [1.10.0] - 2025-11-02

- ADDED: Automatic `iptables`/`nftables` rules restoration. B4 now automatically detects this and restores itself without requiring a manual restart.
- ADDED: New `--tables-monitor-interval` setting to control how often B4 checks if its rules are still active (default: `10` seconds). Set to `0` to disable automatic monitoring.

## [1.9.2] - 2025-11-02

- IMPROVED: Increase TTL and buffer limit for flow state management.
- IMPROVED: enhance SNI character validation.

## [1.9.1] - 2025-11-02

- FIXED: Return back missing `geosite path` field to the settings.

## [1.9.0] - 2025-11-02

- ADDED: Hotkeys to the `/domains` and `/logs` page. Press `ctrl+x` or `Delete` keys to clear the entries. Press `p` or `Pause` to pause the stram.
- ADDED: Parse regex entries from the geosite files.
- ADDED: Connection bytes limit configuration for TCP and UDP in network settings
- FIXED: Wrong total number of total domains in the settings.

## [1.8.0] - 2025-11-01

- ADDED: `nftables` support.
- CHANGED: `--skip-iptables` and `--clear-iptables` renamed to `--skip-tables` and `--clear-tables`.
- IMPROVED: TCP flow handling by fragmenting packets after SNI detection.

## [1.7.0] - 2025-10-31

- ADDED: 'RESTART SERVICE` Button in the Settings to perform the B4 restart from the Web UI.
- ADDED: Add `quiet` mode and `geosite` source/destination options to installer script. Use `b4install.sh --help` to get more information.
- ADDED: Sort Domains by clicking the columns.
- ADDED: Update a new version from the Web Interface.
- REMOVED: iptables `OUTPUT` rule.

## [1.6.0] - 2025-10-29

- FIXED: `Dashboard` works again.
- REMOVED: `--conntrack` and `-gso` flags since they both are not used in the project.
- IMPROVED: Installation script now handles a geosite file setup.

## [1.5.0] - 2025-10-28

- ADDED: `--clear-iptables` argument to perform a cleanup of iptable rules.
- ADDED: `IPv6` support.
- ADDED: `--ipv4` (default is `true`) and `--ipv6` (default is `false`) arguments to control protocol versions.
- IMPROVED: Handling of geodata domains.
