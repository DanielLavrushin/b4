# B4 - Bye Bye Big Bro

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
