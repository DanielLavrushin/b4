# b4 Community Hub - Design

Status: proposal, 2026-09-12, revised the same day after Daniel's answers (see Decisions after review). Produced from a read of the b4 source at 1.82.0 (branch 1-82-fixes) plus three independent architecture drafts and three critiques (security, resilience and operations, product). File and line references point into the b4 repository; the hub itself lives in `hub/` at the repository root.

## Verdict

The idea is good and worth building, with one change of shape. What the Telegram group does today is the right loop (find a set, try it, say whether it worked), and the JSON it passes around is the wrong artefact: today's export leaks an enabled routing block verbatim, including upstream credentials, and keeps dns.pins and dns.target_dns (ImportExport.tsx:67, 90-101, 133), carries no version requirement even though an older b4 drops unknown fields and collapses unknown enum values silently (no DisallowUnknownFields anywhere; config/types.go:98-104), and loses payload captures, so the applied strategy silently becomes the built-in google ClientHello (sock/payload.go:72-98). The hub should therefore not be "a website with a database that routers query". It should be a publisher of one small signed catalogue that any dumb host can mirror, plus a tiny ingest endpoint for signed records. The catalogue is what gives search, apply and discovery candidates when hub.b4core.app is blocked, which is a certainty for some ISPs. Ranking must be per ASN with decay because "works" is per ISP and time-varying. Expect the evidence to be thin for months; the UI must say so rather than show a green number.

Recommended architecture:

- One Go binary `b4hub` at hub.b4core.app: ingest of device-signed records, SQLite ledger, a moderation page (a set is listed only after a moderator approves it), a builder that publishes a signed catalogue every 5 minutes, server-rendered `/s/<id>` pages with copy-JSON so Telegram links work from day one.
- The box downloads the catalogue from hub.b4core.app or any URL in its own list, verifies it against the hub public key compiled into b4 (or the one set in config for a self-hosted or development hub), and answers search, apply and discovery candidates locally. Searched domains never leave the box by default. Mirrors on other hosts are a later option that the format already allows; the proxy boxes are not involved.
- Writes (share, vote, report) are ed25519-signed by a per-box key, idempotent by content hash, sealed to the hub key only when they travel through an untrusted relay or a pasted file, and valid over any channel: HTTPS, fleet relay, outbox retry, or a file pasted in the group.
- One Go scrubber, strategy hash and envelope in the b4 module, used by the box and imported by the hub, with a build-time test that every SetConfig field is classified as crosses or never-crosses.
- Votes keyed by (strategy hash, ASN, device) with tier weights, a 14-day half-life and a Bayesian prior; ASN derived at ingest from the connecting address, never trusted from the client for ranking.

## Decisions after review

Daniel's answers of 2026-09-12, folded into the sections below:

- The hub source lives inside the b4 repository as `hub/`, its own Go module, never part of the router binary. The router-side client is `src/hub/`, the shared leaf is `src/hubwire/`.
- Only hub.b4core.app for now (DNS already points at an Azure address). proxy.b4core.app and proxy2.b4core.app are unrelated and stay untouched; they appear nowhere in the bootstrap list.
- Canonical geo sources for coverage labels: Loyalsoldier geosite and b4geoip geoip.
- Routing never crosses the wire, with one exception: block mode (a set that blackholes ads or trackers) is worth sharing and carries no private data.
- Every submitted set is moderated before it is listed. The first hub release needs a moderation page, not only a CLI. Fake SNI names are only displayed there for the moderator; DNS pins get a certificate check the hub performs itself.
- No key ceremony before anything is public. Development runs against a generated key that b4 trusts through `system.hub.public_key`; the production key is created when the first public b4 release with hub support is cut.
- Search is by site; categories are how the hub works out coverage behind the scenes. The category count question is answered by the seed import and needs no decision.
- The device key gets a recovery code so a reflash or a new router keeps the same author identity.
- One switch, `system.hub.enabled`, gates every hub surface in the UI: browse, share, vote, discovery candidates, sync.

## Status

Phase 0 was implemented on 2026-09-12, in the b4 tree only:

- `src/hubwire/`: field classification with the build-time test, scrub, fingerprint, envelope build and open, version compare. Baseline version 1.82.0, the open changelog block; `Since` entries start with the first field added after it.
- `config.SetConfig.Hub` (`hub`, omitted when unset, denied over MCP) and `system.hub.enabled` as the single switch.
- `POST /api/hub/envelope` and `POST /api/hub/import`; `hub_state` on every set in `GET /api/config`.
- UI: Share section under the set editor's Import/Export tab, envelope detection in Import, the Shared set chip on the card, the Community sets card under Settings, Integrations; EN and RU strings; docs page `sets/sharing` in both locales; changelog entry under 1.82.0.
- Import applies the same pin rule as sharing, drops the DNS redirect when the DoH host is not in the public-resolver list embedded in `hubwire` (the signed manifest takes that role in phase 1), and warns about private addresses and pattern-matching block sets; both hub endpoints are same-origin only and size-capped.
- Not yet: the device key and recovery code, the sync scheduler, local search, the `/hub` page, discovery candidates, telemetry, lineage across re-shares, and the hub service itself.

Fake SNI names are displayed for the moderator only, never flagged, as decided after review.

## The eight questions

### 1. Service language

Decision: Go, for the hub service and for everything on the box. Reason: performance is irrelevant at this scale (thousands of sets, a few requests per second); what matters is byte-identical semantics with the router. The hub must sparsify against DefaultSetConfig (config/sparse.go:30-58, config/config.go:24-191), run config.Validate (config/validation.go:131-287), normalise enums (types.go:91-104), match domains with Go RE2 and the four relations (sni/domain.go:29-94), and flatten geosite records exactly as geodat does (geodat/helpers.go:145-153). All of that exists in Go and is imported; in TypeScript it would be reimplemented and drift, and JS RegExp is not RE2. Rejected: Node/TS service (five reimplementations, none of them free).

### 2. Search by domain and geosite

Decision: no 70 MB unpack anywhere. The hub indexes only categories that published sets reference, keyed by (source, release tag, category), in SQLite, with Loyalsoldier geosite and b4geoip geoip as the canonical sources for coverage labels; a set that references a category absent from the canonical source is still listed through its explicit domains and shows that category as unresolved. The box answers coverage against its own geosite.dat, which is the only authoritative answer for what will match there. Users search by site, never by category. Reason: measured today, RUNET geosite.dat is 73.7 MB and 3.15 M records, 2.75 M of them in two 99%-identical blanket categories; the union of every category up to 100k entries is 115,688 values (1.7 MB of bytes). Category contents differ per source (cn: 4,178 in RUNET, 111,168 in Loyalsoldier) and per daily tag (ru-blocked doubled since Nov 2025), so an unkeyed index gives wrong answers. Rejected: a full in-memory reverse index (150-200 MB heap, dominated by categories no search needs).

### 3. Votes and degradation

Decision: append-only signed votes with a kind (manual, detector, watchdog, discovery, upload), weighted by trust, decayed with a 14-day half-life, aggregated per (strategy, ASN), then country, then global, with a prior so one vote never yields 100%. Sets with fresh negative evidence and no positive evidence are demoted, not deleted. Reason: a bare counter cannot decay, cannot be re-weighted, and a global number contradicts the field facts (STUN fake with TTL 7 killed Cloudflare on Redcom while working elsewhere). Rejected: global works/broken counters.

### 4. Geo categories and payload files

Decision: geosite and geoip files never cross the wire, only category names plus the uploader's source URL, tag and sha256 (b4 starts recording tag and hash at download). Payload .bin files do cross the wire as content-addressed attachments (sha256, protocol, domain, bytes, 16 KB cap), validated on the hub and again on the box with b4's own parsers (sni/tls.go:63, sni/quic.go:9), because without them the shared strategy is silently a different strategy. A bad actor's real channels are the set fields that redirect traffic, so those are refused at ingest, not stripped (see Safety). Rejected: uploading .dat files with sets (70 MB, still expanded against the receiver's own file anyway).

### 5. Identity

Decision: a per-box ed25519 keypair in a sidecar file, no accounts, no auth provider. The key id is the pseudonym; the hub shows authors as an HMAC of the key, never the key. Reason: OAuth providers are themselves blocked on some of these ISPs, cannot complete a redirect to a LAN address, tie a pseudonym to a person in a censorship context, and do not stop Sybil accounts since accounts are free. A signed record is valid however it travelled, which is what makes the blocked write path survivable. The key's 32-byte seed is shown once, at creation, as a recovery code (base32, 52 characters); entering it on another box restores the same author identity, and the hub stores nothing for this. Rejected: GitHub or Telegram login for v1 (an optional verified-author claim can be added later as a nullable column).

### 6. Provenance

Decision: keep the link, keep sets editable, compare a strategy hash. A Go field `hub` on SetConfig records {id, version, hash, applied_at}; the box recomputes the hash of the live set whenever it matters and shows "from hub" or "edited since applied". Votes are disabled when the hash differs; "share as new version" uploads with `derived_from` so lineage survives. Reason: users tune sets and the watchdog overwrites strategy blocks in place (watchdog/applier.go:95-133); read-only would force copies and lose the vote link, and breaking the link on edit loses lineage. Rejected: read-only hub sets; breaking the link on first edit.

### 7. Discovery integration

Decision: inject hub candidates into the existing Phase 0 loop (discovery.go:228-263) as ConfigPreset values with Phase=PhaseCached, after the box's own cache entries, capped at 3, taken only from the local catalogue. Reason: cache entries are evidence from this box on this ISP ranked by SuccessCount (cache.go:154-182); hub evidence is from other boxes. PhaseCached already ranks 0 (discovery.go:1979-1991) and has a UI step, so no new phase plumbing. Each candidate costs ConfigPropagateMs plus fetches per domain (config.go:274-279), hence the cap. Never touch the network inside a run: discovery is exclusive and blocks a tables refresh for up to 5 minutes (main.go:194-207). Rejected: community candidates before cached; a new PhaseCommunity; fetching candidates from the hub at run start.

### 8. Blocking resilience

Decision: the read path is a signed static catalogue on many dumb hosts; the write path is signed, encrypted, idempotent records over any channel; "slave hubs" collapse into two roles that need no registration: a static mirror (cron fetch of the catalogue) and a stateless relay (one nginx proxy_pass). Reason: reachability is per-ISP (one Moscow ISP blocks the Oracle box, another blocks nothing), so many independent hosts for one small artefact is the right shape, and this is exactly how b4 already ships binaries and geodata. The first release runs on hub.b4core.app alone; the format is what makes a later mirror a cron job instead of a redesign. Rejected: p2p between routers (NAT, no transport in b4, worst possible peers); self-registering replicas with sync (each needs the master reachable to add resilience against the master being unreachable, and adds a poison vector).

## Architecture

Three roles and one artefact.

- Publisher (trusted, one instance, the host behind hub.b4core.app). Holds the ledger (SQLite), the hub signing key and the hub encryption key. Runs `serve` (ingest, moderation page, `/s/<id>`), `build` (catalogue every 5 minutes), `geo` (daily geosite snapshots), `moderate` (CLI twin of the page). The signer runs as a separate process with no inbound HTTP; ingest never holds the signing key.
- Mirror (untrusted, any number, none in the first release). Serves `/b4/health`, `/b4/hub/manifest.json`, `/b4/hub/catalogue-<epoch>-<seq>.json.gz`, `/b4/hub/blob/<sha256>` by plain GET. Populated by `b4hub mirror --from --out` on cron or by rsync from the publisher. GitHub release assets and SourceForge are mirrors with zero server code. A mirror can only withhold: the manifest is signed, the catalogue and blobs are hash-listed by the manifest.
- Relay (untrusted, optional on any mirror, none in the first release). POST `/b4/hub/v1/msg` forwarded verbatim. It sees ciphertext and a source address, nothing else.

Trust boundaries on the box: nothing received from a mirror is used before the manifest signature verifies against the two compiled keys and the catalogue hash matches; blobs are re-parsed on apply; the browser never talks to the hub (the React UI makes no external calls today and a LAN browser may not reach the hub). Hub HTTP from the box carries config.SelfDialMark 0x40000 (types.go:122-125) so a user's proxy set cannot divert it, while the capture chains still give it DPI bypass (tables/iptables.go:449-451).

Where the critiques disagreed: the product critique wanted the central online service as the spine for immediate visibility. The security and resilience critiques wanted the signed catalogue. The catalogue wins, and the freshness objection is answered with a 5-minute build, an hourly box sync, an immediate sync after every share, a "Sync now" button, and the `/s/<id>` page from the first hub release. Online search exists as an explicit "check online" action that sends the domain and says so.

## Data model

### Sharable set projection

The shared object is `set` inside the envelope, sparse against DefaultSetConfig with the same reset-disabled-blocks rule as ImportExport.tsx:52-68, produced only by the Go scrubber.

Crosses the wire:

- `tcp`: conn_bytes_limit, seg2delay, seg2delay_max, syn_fake, syn_fake_len, syn_ttl, drop_sack, http_methodeol, dport_filter, incoming{mode,min,max,fake_ttl,fake_count,strategy}, desync{mode,ttl,count,post_desync}, win{mode,values}, duplicate{enabled,count}, rst_protection{enabled,ttl_tolerance}.
- `udp`: mode, fake_seq_length, fake_len, faking_strategy, fake_payload_file (rewritten to `sha256:<hex>` or kept as `@quic_initial`, `@preset:quic1`, `@preset:quic2`, or empty), dport_filter, filter_quic, filter_stun, conn_bytes_limit, seg2delay, seg2delay_max.
- `fragmentation`: every field, including strategy_pool, seq_overlap_pattern, combo{...}, disorder{...}; arrays keep their order.
- `faking`: every field; payload_file rewritten to `sha256:<hex>` or absent; custom_payload capped at 8 KB; payload_domain and sni_mutation.fake_snis subject to the SNI policy.
- `targets`: sni_domains (max 500, canonical form per sni.CanonicalDomainEntry), ip (max 200 public CIDRs), geosite_categories, geoip_categories, domain_only, tls, ip_version.
- `dns`: enabled, doh_url (allow-listed hosts only), fragment_query, strict, pins (only for domains among the set's own sni_domains, only public addresses, every address certificate-checked by the hub, see Safety).
- `routing`: only enabled, mode = block and block_action, and only together; any other routing field or mode is refused.
- `mss_clamp`: enabled, size.
- Envelope-level: title (replaces name), description, b4_version, engine, geo{source_url, tag, sha256}, derived_from.

Never crosses the wire, and its presence is a 400 `scrubbed_field` at ingest with the body discarded: `id`, `enabled`, `stats`, `hub`, `escalate.*`, `tcp.ip_block_detect.*`, `targets.source_devices`, `targets.source_devices_exclude`, `dns.target_dns`, every `routing` field except the block-mode triple (egress_interface, egress_ip, upstream.*, fwmark, table, source_interfaces, ip_ttl_seconds, router_traffic, kill_switch), and every `json:"-"` field. Block mode crosses because blocking ads and trackers is worth sharing and exposes nothing; a block set whose targets are blanket (a category over 200,000 entries or a bare regexp) is flagged for the moderator and warned about in the apply dialog, since it is a self-DoS from a nice-looking card.

### Strategy fingerprint

`fp = sha256(canonical JSON)` of tcp minus dport_filter, udp minus dport_filter, fragmentation, faking, mss_clamp and the dns strategy fields, after ApplySetDefaults and the disabled-block reset, sorted keys, no whitespace, arrays ordered, payload refs replaced by blob hashes. Computed by one function in the b4 module and used for dedup on upload, "edited since applied" on the box, and vote pooling. The existing 7-field cache key (cache.go:95-103) and the 4-field watchdog key are too coarse and stay untouched.

### Versions and lineage

`sets(id ULID, author_hmac, title, description, current_version, derived_from, created_day, hidden_reason)` and `set_versions(set_id, version, fp, record_json, b4_min, family, geo_source, geo_tag, flags_json, created_at)`. Only the latest version is listed; evidence is not inherited across versions except the author's upload vote. `b4_min` is derived on the hub from a field first-appearance table (http_methodeol and udp coalesce untagged as of 2026-09-12; kill_switch 1.80.0; egress_ip and dns.strict 1.79.0; dns.pins 1.76.0; domain_only 1.72.0; ip_version 1.67.0; rst_protection 1.48.0; strategy_pool 1.45.0; mss_clamp 1.39.1; and so on). The listed versions were checked against b4's git tags on 2026-09-12; the table is generated from git history, not maintained by hand.

### Votes

`votes(id, set_version_id, fp, device_hmac, kind, weight, asn_observed, asn_claimed, country, origin_verified, domain, b4_version, engine, received_at)`. Kinds: manual_works, manual_broken, upload, detector_fixed, detector_broken_by_b4, detector_still_blocked, watchdog_verified, watchdog_degraded, discovery_confirmed, discovery_failed. One effective vote per (device, fp, asn) per 7-day bucket, newest wins. Weight is copied from a `vote_weights` table at insert so retuning does not rewrite history. Raw votes are kept for 60 days, then folded into aggregates with device linkage dropped.

## Wire protocol

Mirror contract (GET, no auth, cacheable): `/b4/health`, `/b4/hub/manifest.json`, `/b4/hub/catalogue-<epoch>-<seq>.json.gz`, `/b4/hub/blob/<sha256>`.

Manifest (the only signed object): `{v, key_id, epoch, seq, generated_at, expires_at, catalogue:{file, sha256, size}, mirrors[], geo_sources[], doh_allowlist[], revoked_keys[], sig}`. Two hub public keys are compiled into b4 (active and spare); a manifest signed by the spare may carry a revocation of the active key, which the box persists. `system.hub.public_key` in config overrides the compiled keys for a self-hosted or development hub. The production pair is generated when the first public b4 release with hub support is cut; until then everything runs against a key from `b4hub keygen`. The box orders by (epoch, generated_at), accepts a new epoch once even with a lower seq, refuses older generated_at within an epoch, and takes the newest valid across every source it can reach. `expires_at` is 14 days: past it the box keeps the file, shows "catalogue is N days old" and stops automated votes. `min_b4_version` is per record only, never a gate on the whole catalogue.

Catalogue: `{epoch, seq, generated_at, sets:[record...], categories:[{src, tag, name, count}], blobs:[{sha256, size, protocol, domain}], asn_names:{}}`. A record is the sharable projection plus id, version, fp, family, b4_min, flags (needs_payload, needs_ipv6, tun_unverified, blanket, dns_dropped, catch_all, has_pins, block), status (active, new, demoted) and scores {global, asn:{at most 32 by mass}, cc:{}}. Size: 5,000 sets at roughly 700 bytes plus scores is about 4.5 MB raw, 1 MB gzip; the box refuses over 8 MiB and streams it to disk through the temp-file, fsync, rename helper (handler/common.go:245-296).

Write message: `{v:1, kind: share|vote|report|revoke, key, ts, nonce, body}` signed with ed25519 over the canonical JSON with the domain prefix `b4hub/1`. Direct HTTPS to the publisher sends the signed message as is. Sealing the signed message with nacl/box to a hub encryption key (x/crypto is already a dependency) hides the domain, ASN hint and key linkage from relay operators and from anyone reading a pasted file; it lands together with relays in phase 2 and is not needed for phase 0 or 1. Id is sha256 of the canonical signed object; ingest dedupes by id forever, so replay is harmless and there is no tight timestamp window (routers boot at the epoch and the outbox retries hours later; the hub uses received_at for everything and treats ts as a hint). Relay contract: POST `/b4/hub/v1/msg`, body under 96 KB, forwarded verbatim; responses 202 `{id, set, version}`, 200 `{duplicate}`, 400 `{code, error, fields[]}` in b4's APIError shape (handler/apierr.go:12-71), 409 for an identical fp already listed (counted as a vote), 429 with retry_after.

Online read API on the publisher, optional for the box: GET `/v1/search?domain=&geo_source=`, `/v1/sets/{id}`, `/v1/geo/coverage?domain=`, `/v1/blob/{sha256}/exists`; HTML `/s/{id}` and `/?domain=`. Online search results are only trusted for ids that verify against the local catalogue; otherwise the box triggers a sync.

Versioning: `format` on the envelope, `v` on messages and manifest; additive changes only; a new required field is a new format number.

## Ranking

Each vote gets an effective weight

`w = w_kind * m_origin * m_age * 0.5^((now - received_at) / 14 days)`

with w_kind: manual 1.0, upload 1.0, detector_fixed 0.8, detector_broken_by_b4 -1.0, detector_still_blocked -0.6, watchdog_verified 0.7, watchdog_degraded -0.5, discovery_confirmed 0.4, discovery_failed -0.2; m_origin 1.0 when the ASN was observed at ingest, 0.25 when unverified (relayed through an untrusted relay or pasted in the group); m_age 0.25 for keys younger than 7 days.

Per key K in {(fp, asn), (fp, country), (fp, global)}: P = sum of positive w, N = sum of |negative w|, n = P + N, devices = distinct device_hmac, and

`score_K = (P + 1) / (P + N + 2)`

Automated kinds contribute at most 50% of n in any cell; beyond that they are stored but not counted, so one human "broken" always moves the score.

Display for a box on ASN A in country C: use the ASN cell when n_A >= 1.0 and devices >= 2, else the country cell under the same rule, else global, else "no reports". Every card states the bucket in words with the ASN name from asn_names, the distinct device count and recency. Sort within coverage class (targeted before blanket, exact before covered before regexp): bucket, then score, then n, then newest version.

Cold start: an upload counts as one manual vote in the author's observed ASN, so a fresh set is "1 report on your ISP" at home and "new" elsewhere; the author's vote shows as "1 report" not a percentage. Demotion: global score below 0.3 with n >= 3, or no vote of any kind in 60 days with decayed n below 0.25, hides the set from default listings and from discovery candidates; it stays fetchable by id and under "show all", with the reason shown. Curated revoke hides regardless of score. Nothing is deleted automatically.

Unverified-origin votes never enter a per-ASN cell, only the global pool. The two fleet proxy boxes are trusted relays (fixed IPs, mTLS between nginx and ingest, no header secret) whose forwarded address is honoured; third-party relays are unverified. Relay-signed ASN attestations are deferred.

## Safety

Payload attachments: 16 KB cap; must parse as one TLS handshake record carrying a ClientHello or as a QUIC Initial with a readable ClientHello, using sni.ParseTLSClientHelloSNI and sni.ParseQUICClientHelloSNI; extracted SNI must equal the declared domain; pre_shared_key, early_data and non-empty session_ticket are refused; random and legacy_session_id are replaced with fresh random bytes at ingest and the hash recomputed. The box re-parses before writing through the capture manager and refuses a same-name file with a different hash rather than overwriting (SaveUploadedCapture writes unconditionally, capture/manager.go:463-470); blobs live under `.hub/blobs/<sha256>` and are copied into captures only on explicit apply. Preview says a capture reveals the client fingerprint and offers payload_domain instead.

Fake SNI: `faking.payload_domain`, `sni_mutation.fake_snis` and the server name inside a custom payload or an attachment decide which domain b4 writes into the fake ClientHello it sends ahead of the real one. That name is the uploader's choice, and it leaves every applier's router on every matched connection. A hostile uploader can pick a name nobody else uses, so the DPI can tag every router running that set, or a name on a blacklist, so the applier's flows get punished. Decision for now: no rule and no flag, since users already share such sets freely; the hub computes the names a set will emit and shows them on the moderation page and in the apply dialog, and the moderator judges. A flag can be added later if it ever matters.

DNS pins: a pin sends every applier to the listed addresses for that domain, so it is the most dangerous field a set can carry and also the most valuable one to share, since Discovery's address scan finds addresses the block does not cover. A pin is accepted only when the domain is one of the set's own sni_domains, every address is public, and the hub has connected to each address on port 443 with that server name and validated the certificate chain against the system roots for that name. The check runs at ingest and daily afterwards; a failing address hides the set version with the reason shown. Certificates are what make this safe: nobody can present a valid certificate for microsoft.com from an address they control. A domain with no valid certificate on 443 cannot be pinned. Pins are listed on the moderation page.

Geo categories: names only; the box's own expansion decides, EmptyGeoSite is surfaced in the apply dialog (handler/sets.go:675-683), categories are dropped on a box with no database as the discovery add path does (handler/discovery.go:298-305). Categories over 200,000 entries are flagged blanket, ranked last, excluded from discovery candidates, and the apply dialog warns with the local count because such a set costs a router well over 100 MB.

DNS: doh_url must match the allow-list in the signed manifest; otherwise the dns block is dropped and flagged dns_dropped. Refuse, never warn.

Ingest also runs config.Validate against a stub Config with non-empty geo paths, and caps fake_snis at 8, regex patterns at 512 bytes, duplicate.count at 3.

Rate limits: per key 5 shares, 20 votes, 20 blobs per day; per HMAC(daily salt, /24 or /48) 5 new keys per day and 2,000 requests per hour, in memory only; relayed traffic is keyed by device key, not address. Moderation: every share enters `pending` and reaches the catalogue only after a moderator approves it, so the first release carries a moderation page (`/admin`, behind a password from the environment, moderator keys later): the queue, each set's projection, its flags (pins, unknown DoH host, blanket categories, private-looking domains, payload attachment with its parsed server name, duplicate fingerprint, block mode) and the fake SNI names it will emit, approve, reject with reason, hide, ban key. Signed report records from users auto-hide an approved set after three reports from distinct keys in distinct ASNs, pending review. `b4hub moderate hide|unhide|ban|delete` mirrors the same actions on the CLI, bound to a unix socket.

Privacy of the database: no access log with client addresses; connecting IP is used for ASN derivation (offline RIB table refreshed daily, Team Cymru fallback) and a salted /24 hash kept 7 days, then discarded; authors and voters are stored as HMAC(hub secret, key); timestamps published at day granularity; the catalogue carries aggregates only, never per-device votes; there is no public per-record feed (the resilience critique wanted one for rebuilds; the security critique's objection wins, and rebuilds come from the nightly encrypted ledger copy on the Oracle box instead). Automated votes carry a domain only when it is literally in the set's sni_domains. The share preview flags private-looking targets (.local, .lan, single-label names, IP literals) and requires removal.

On the box: hub write endpoints require a same-origin request regardless of web auth, because http/cors.go:9-14 reflects any Origin with credentials and authMiddleware passes everything when no password is set (http/auth.go:257-260); all hub state lives under `<configdir>/.hub/` so backup.go's dot-directory exclusion keeps the private key out of backup archives; system.hub is outside mcpWritableRoots with a deny hint (mcp_write.go:43-48, 119-138); there is no MCP share tool; nothing is ever auto-applied or auto-shared.

## b4 client changes

- `src/config/types.go`: `Hub HubOrigin json:"hub,omitempty" mcp:"deny"` on SetConfig (types.go:496-511) and `Hub HubConfig json:"hub"` on SystemConfig (types.go:338-354). HubConfig: `enabled bool`, `urls []string omitempty`, `public_key string omitempty`, `telemetry bool`, `discovery_candidates bool`, `sync_interval string omitempty` ("" = hourly). `enabled` is the single switch: when it is off the Community page, the share and vote controls, discovery candidates and the sync scheduler are all absent, and the settings card is the only hub surface. All zero values are the defaults, so an untouched install writes nothing and no migration is needed. No secret and no last_sync in config.json.
- `src/config/config.go`: DefaultConfig entry; `make gen-defaults`.
- `src/hubwire/` (new leaf package, imported by the hub): Scrub, Fingerprint, Envelope, sign/verify/seal, first-appearance table, family classification, the two hub public keys. A table test fails the build when SetConfig gains a field absent from the classification.
- `src/config/sparse.go`: export the sparsifier for hubwire.
- `src/geodat/geosite.go`: exported `ScanGeositeCategories(path, tags, fn)`, the tag-filtered retain-nothing scan (streamGeoSite at :12-45 retains; ScanGeositeEntries cannot filter).
- `src/http/handler/geodat.go` RefreshGeodat and `src/geodat/config.go`: record final redirect tag, ETag and sha256; `handler/geodat.json` and `installer/features/geosite.sh:5-6` move the RUNET preset to the tag-pinned releases/latest/download URL.
- `src/detector/network.go`, `asn.go`: exported LookupNetwork as a hint source only.
- `src/mirrors/` (new): lift mirrorClient, normalizeMirror, mergeMirrors, mirrorAlive, fetchBytes, fetchBytesMirrored, downloadFile out of handler/mirrors.go and common.go; add postBytesMirrored and a SourceForge URL mapping; cache the last reachable base with a TTL instead of probing per call.
- `src/hub/` (new): identity (ed25519 sidecar), client (SelfDialMark via netprobe.HTTPClient plus ProxyFromEnvironment), store (.hub sidecars), scheduler copied from geodat/scheduler.go:12-165 with a Stop that never waits on the network (main.go:513-531), search (sync-time index: canonical manual entry to set index, category to set index, regex list; membership for uncached categories via ScanGeositeCategories with an LRU keyed by geosite fileStamp), rank, apply, outbox, signals.
- `src/http/handler/hub.go`, `hub_types.go`, `hub_test.go`: RegisterHubApi from RegisterEndpoints (common.go:166-197) with /api/hub/status, sync, search, sets/{id}, sets/{id}/apply, sets/{id}/vote, sets/{id}/test, share/preview, share, import, identity; swag annotations; errors via writeAPIError. Apply reuses the handleAddPresetAsSet steps (handler/discovery.go:242-339) with a createSet branch for geosite-only and ip-only sets, installs blobs first, stamps Hub, and returns moved domains, missing categories and installed blobs. `test` runs watchdog.ProbeHost once through b4 and once bypassing (the b4_test_domain_now pattern, mcp_probe.go:90-155) and records a detector-tier vote. `updateSet`, the MCP write path and watchdog applyGroup recompute nothing; the hash is compared live on read.
- `src/http/handler/config.go:131-225`: `hub_state: unmodified|modified` on SetWithStats.
- `src/discovery/runtime.go:37-45`: `HubCandidates []ConfigPreset`; discovery.go:228-263 appends them after cachedPresets; finalize emits per-candidate outcomes to a callback.
- `src/watchdog/watchdog.go:183-199, 343-398` and `src/detector/sites.go:468-503`: injected reporter interface, called outside the tick lock, only for sets whose Hub hash matches, only when telemetry is on.
- `src/main.go:462-481, 513-520`: construct, Start, Stop next to geoScheduler.
- `src/http/handler/mcp_hub.go`: b4_hub_search (local catalogue, no gate), b4_hub_apply (allow_writes, mcpWriteMu, mcpChange for revert), b4_hub_vote (allow_writes); no share tool.
- UI: `/hub` route and nav item (App.tsx:68-77, 111-122, 281-293), `components/hub/*`, `api/hub.ts`, `hooks/useHub.ts`, `models/hub.ts`, `barrels/hub.ts` plus the alias in vite.config.ts and tsconfig; SetCard "Share to hub" menu item (SetCard.tsx:256-291), "from hub" chip and vote buttons in the footer with the EscalationChip pattern (:385-419, 442-477); Manager.tsx share dialog and "Browse community" button; ShareToHub.tsx shows the Go-built preview and stripped fields; ApplyDialog reused with an ApplyTarget and one-click undo in the snackbar (Discovery.tsx:209-224); Hub card as the fourth B4IntegrationCard in settings/Api.tsx modelled on MCPCard; Compare.tsx IGNORE_KEYS and buildExportJson keep `hub` (exports carry lineage; the hub id inside a pasted copy stamps provenance on import since mergeWithDefaults keeps unknown keys).
- i18n en.json and ru.json in the same commit; docs/docs/settings/hub.md with the exact enumeration of what leaves the box, RU mirror; changelog EN and RU; CLAUDE.md MCP list.

## Hub service

Location: `hub/` at the b4 repository root, its own Go module, so one repository holds the service, the client and the shared types, while the router binary never links any of it.

```
hub/go.mod                  module github.com/daniellavrushin/b4hub, replace github.com/daniellavrushin/b4 => ../src
hub/cmd/b4hub/main.go       serve | build | geo | mirror | moderate | keygen
hub/internal/api/           net/http, Go 1.22 patterns, APIError shape, rate limits
hub/internal/ingest/        open, verify, scrub (hubwire), config.Validate, blob checks, SNI and pin checks, ASN derivation
hub/internal/store/         modernc.org/sqlite, WAL, embedded migrations, ledger tables
hub/internal/geo/           tag-pinned Loyalsoldier geosite and b4geoip geoip, referenced-category index, blanket policy
hub/internal/match/         sni relations, punycode (x/net/idna), coverage assembly
hub/internal/score/         weights, decay, aggregation job, demotion
hub/internal/catalogue/     builder, manifest, signing via unix socket to the signer
hub/internal/signer/        holds the signing key, no inbound HTTP
hub/internal/web/           html/template: /s/{id}, /?domain=, /admin moderation page
hub/internal/mirror/        fetch, verify, atomic rename (later release)
hub/deploy/                 systemd unit, nginx vhost for hub.b4core.app
hub/DESIGN.md               this document
```

The module name is not `github.com/daniellavrushin/b4/hub` because that import path is taken by the router-side client package `src/hub/`.

Sharing types with b4: `src/go.mod` declares `module github.com/daniellavrushin/b4` but lives at `src/`, so `go get` cannot resolve it. Because both live in one repository, `hub/go.mod` carries `replace github.com/daniellavrushin/b4 => ../src` and always builds against the checked-out tree; CI adds one job with working-directory `hub`. No `go.work`: a workspace would let the hub's requirements bump the versions the router binary is built with. `go -C src test ./...` stays exactly as it is, and router builds never see hub code or its dependencies. The hub imports only `hubwire`, `config` (Validate, DefaultSetConfig), `sni` and `geodat`; `go list -deps ./config` pulls cobra, pflag, engine, netif and tlsgen into the binary, which is acceptable on a VPS. Makefile targets at the repository root: `hub-build`, `hub-test`, `hub-run` (local hub against a development key), `hub-deploy`.

Storage: SQLite in WAL mode, one writer, nightly encrypted copy to the Oracle box over the existing ssh path; the corpus is small enough to rebuild from that copy or from the last catalogue in minutes. Deployment: hub.b4core.app resolves to 20.215.224.201 (Azure). Which box that is, its nginx vhost and its certificate are settled at the first deploy, after the hub has run locally against a development key. Nothing changes on the proxy boxes. The unit and vhost are versioned under `hub/deploy/` even though applied by hand.

## Resilience plan

- Bootstrap list on the box, first release: `system.hub.urls`, then https://hub.b4core.app, then the mirror list carried by the last valid manifest (empty at first). The proxy boxes are not on it.
- Channels the format already allows and that are deferred until there is a reason: a GitHub release asset and SourceForge (a workflow attaching manifest and catalogue to a rolling release, as sourceforge-mirror.yml does for binaries), a Cloudflare custom-domain front for the static path (custom hostnames measured reachable where workers.dev is throttled), user mirrors via `b4hub mirror`, and a weekly file posted in the Telegram group.
- Offline import: `POST /api/hub/import` accepts a manifest plus catalogue pair or a single envelope file; both verify before anything is used. The envelope path is also phase 0's only mechanism and the permanent fallback.
- Write path when blocked: hub URLs, outbox under `.hub/outbox` retried hourly for 30 days, and finally "download envelope" for the group, where the maintainer CLI ingests it as unverified-origin. Relays come with mirrors, later.
- Master down: reads continue from any mirror until expires_at; shares and votes queue; a rebuild on a new box imports the ledger copy, starts a new epoch, and the box's (epoch, generated_at) rule accepts it without a fleet-wide freeze. Key compromise: the spare signs a revocation manifest that boxes persist; a b4 release ships a new spare.

## Phased delivery

Estimates assume one person working on this full time; halve the pace for evenings and weekends.

- Phase 0, b4 only, about one week: hubwire with the classification test, HubOrigin field, share preview dialog producing an envelope, import through the apply dialog with blob install and missing-category warnings, geosite tag and hash recording, ScanGeositeCategories. Ships to users as a safer replacement for pasting exports: no more credential and pins leaks, a minimum b4 version on every shared set, payloads travel with the set.
- Phase 1, four to six weeks: publisher `serve` with ingest, ledger, geo indexer over Loyalsoldier and b4geoip, catalogue builder and signer against a development key, `/s/<id>` pages with copy-JSON, the moderation page, seed import of the group corpus; on the box: identity with recovery code, client, scheduler, local search, `/hub` page behind `system.hub.enabled`, apply with undo, Hub settings card with `public_key`, Test now. Everything runs locally first (b4 on this PC or the ASUS pointed at a hub on localhost), then on hub.b4core.app. Ships the whole find, try, say-if-it-worked loop, with manual votes and moderation.
- Phase 2, one to two weeks: user reports and ban tooling, `b4hub mirror` and docs for self-hosting, the production key pair and its compile into b4, the sealed write message.
- Phase 3, one to two weeks: discovery candidates from the local catalogue, opt-in telemetry from discovery, watchdog and detector, MCP tools, weight tuning from the first weeks of data.
- Phase 4, only if usage or blocking justifies it: additional catalogue channels (GitHub, SourceForge, user mirrors, Cloudflare front), relays, online search API as a freshness layer, relay ASN attestations, verified-author claims, the static website over the catalogue.

Measure on the ASUS box during phase 1, before local search becomes the default: catalogue index build time and RSS, and the filtered geosite pass wall time for an uncached category.

## Open questions

- Who moderates besides Daniel, and how moderators log in once there is more than one: a password from the environment first, moderator keys later.
- Whether the seed import of the group corpus should be done by hand from the Telegram history or through a bot; by hand is enough for the first hundred sets.

## Rejected alternatives

- TypeScript hub: reimplements sparsify, defaults, Validate, RE2 matching and the geosite scanner, and drifts every release.
- Full in-memory geosite reverse index: 150-200 MB for two blanket categories nobody searches.
- Bearer device tokens through mirrors: any relay harvests credentials and can vote as the device.
- Public per-record feed: a complete per-pseudonym activity log for the ISP to correlate with its own flow logs.
- Warn-only DoH policy: an attacker-owned https resolver hijacks DNS for every applier.
- Refusing DNS pins outright: it would throw away the most useful thing Discovery finds; certificate validation plus moderation makes them safe to carry.
- Read-only hub sets or breaking the link on edit: loses either the ability to tune or the lineage.
- Global vote counters, Wilson intervals on weighted decayed counts, ELO: either wrong per ISP, undefined, or unexplainable on a card.
- Bare monotonic seq without epoch: a publisher restore freezes the fleet.
- Self-registering replicas, multi-master sync, p2p between routers: each needs the master reachable or adds a poison surface.
- Cloudflare Worker as origin: workers.dev throttled to tens of bytes per second on at least one Moscow provider; fine as one relay for KB-sized messages.
- OAuth accounts: blocked providers, LAN redirect problem, no Sybil benefit.
- Hub state or device key in config.json: backups, GET /api/config and MCP would carry it; background writes race cfgPtr.Store (main.go:470-478).
- Fetching discovery candidates from the hub inside a run: exclusive, time-boxed, and pays 6 s per unreachable base.
- Routing modes other than block on the wire: they carry addresses and credentials and mean nothing on another router. Block mode crosses, flagged when its targets are blanket.
- Moving b4's go.mod to the repo root to make the module fetchable, or a separate repository with a submodule: disruptive or duplicated for no runtime gain; `hub/` in the same repository with a `replace` to `../src` does the job.