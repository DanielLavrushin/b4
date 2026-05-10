# B4 - Bye Bye Big Bro

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
- CHANGED: **Swagger UI moved to documentation site** — the embedded Swagger UI has been removed from the binary to reduce its size (~27MB → ~13MB). API documentation is now available at [daniellavrushin.github.io/b4/swagger](https://daniellavrushin.github.io/b4/swagger) with the ability to connect to a live B4 instance for testing. The `/swagger/` endpoint now redirects to the documentation site.
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
