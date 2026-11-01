---
sidebar_position: 50
title: API and MCP tools
description: The REST API behind the web interface, the endpoints the documentation refers to, and the tools and resources of the MCP server.
---

The web interface works through a REST API that b4 serves on the web server's port, under `/api/`. The [API reference](/swagger) on this site lists every endpoint with its parameters and responses. With the address of a running b4 in its **B4 Instance** field, that page sends its requests to that instance; without one, it only shows the reference.

## Authentication {#authentication}

While a username and a password are set under **Settings > System > Web Server**, every request under `/api/` needs a login token, except those to `/api/auth/` and `/api/version`. Without a username and password, no token is needed. A page at another address or port then gets `403` for requests that change something and for connections under `/api/ws/`. A reverse proxy in front of b4 must pass the original `Host` header and `X-Forwarded-Proto`. Without them, the **Logs** and **Traffic** pages stay empty, and saving settings can fail.

| Step | Request |
| --- | --- |
| Log in | `POST /api/auth/login` with `{"username": "...", "password": "..."}` returns `{"token": "..."}` |
| Use the token | An `Authorization: Bearer <token>` header on every request. WebSocket connections under `/api/ws/` can pass it as the `token` query parameter instead |
| Log out | `POST /api/auth/logout` revokes the token |

A login token stays valid while it is used at least once every 24 hours, and is discarded when b4 restarts. After repeated failed logins from one address, b4 answers `429` for a while. The MCP endpoint has a token of its own, see [MCP server](#mcp-server).

## Endpoints the documentation refers to {#endpoints}

| Request | What it does |
| --- | --- |
| `GET /api/metrics` | The dashboard snapshot |
| `GET /api/metrics/summary` | A short summary of the snapshot, with the connection totals since the last counter reset |
| `/api/ws/metrics` | WebSocket: a full snapshot, then one update per second, as the dashboard receives them |
| `POST /api/metrics/reset` | **Reset counters** on the dashboard |
| `POST /api/escalations/clear` | **Clear** on the dashboard's **Active Escalations** panel: every escalated host goes back to its set |
| `GET /api/ipbans` | The [banned addresses](../sets/tcp/general.md#banned-addresses) the dashboard's **Banned addresses** panel lists |
| `POST /api/ipbans/clear` | **Unban** on that panel with `{"key": "<address>:<port>"}`, or **Clear all** without a key |
| `/api/ws/logs` | WebSocket: the live log the **Logs** page shows |
| `POST /api/logs/trace/start`, `POST /api/logs/trace/stop` | **Start trace** and **Stop & save** on the **Logs** page |
| `GET /api/logs/trace/status` | Whether a trace is recording |
| `GET /api/logs/trace/download` | The last trace file, as **Download last trace** saves it |
| `GET /api/config/download` | The configuration as it is, as **Download as is** under **Settings > System > Backup** saves it |
| `GET /api/config/download?safe=true` | The safe copy, as **Download safe copy** saves it |
| `GET /api/system/addresses` | The ports the **Expose to internet** switches opened, their firewall chains and errors, and the [reason](#exposure-reasons) a switch that is on opened nothing |
| `GET /api/watchdog/status` | The watchdog's state for every watched set and domain, with the [reason codes](#watchdog-reason-codes) |
| `POST /api/discovery/start` | Starts a Discovery run. A run for a set that routes its traffic is refused with `400` and the code `routed_set`, and any run while b4 is still applying its DSCP rules with `409` and the code `dscp_rules_pending` |
| `POST /api/discovery/replace`, `POST /api/discovery/add`, `POST /api/sets/{id}/add-domain` with `tested` | Write a Discovery result into a set, create a set from it, or add its domains to a set. With the run's `dscp_path`, a set whose traffic takes another DSCP path is refused with `409` and the code `dscp_path_changed`, and a write while b4 is still applying its DSCP rules with `dscp_rules_pending`. `replace` and `add` take the [page hosts](../discovery.md#page-hosts) to add in `hosts`. An entry that is not a host name on the internet is refused with `400` and the code `bad_host`, and more than 64 hosts with `too_many_hosts` |
| `POST /api/mcp` | The MCP server, see [below](#mcp-server) |

### Watchdog reason codes {#watchdog-reason-codes}

The watchdog reports why a set is in its state, or how its last heal ended, as one of these codes. The [Watchdog](../watchdog.md) page shows the same reasons in words.

| Code | Meaning |
| --- | --- |
| `not_owned` | The set's addresses are handled by another set or by none, so a strategy written into this set does not affect them |
| `escalated` | An escalation sends the addresses to another set |
| `unusable` | A certificate error, a required client certificate, or a private or local destination. Never healed |
| `current_works` | Tested alone, the set's own strategy loads the addresses, yet the live check fails |
| `not_needed` | Tested alone, the addresses load without a bypass, yet the live check fails |
| `partial` | The search found a strategy for only some of the addresses, so nothing was written |
| `none` | The search found no working strategy |
| `incomplete` | The search ended without a confirmed result |
| `budget` | The search ran longer than 20 minutes and was canceled |
| `start_failed` | The Discovery run could not start |
| `verify_failed` | The strategy written did not pass the live check and was undone |
| `edited` | The set was changed during the check or the search, so nothing was written |
| `busy` | Another Discovery run holds the runtime, and the heal waits |
| `rules_changed` | b4 was applying or rewriting the firewall rules the search depends on, such as its DSCP rules, so nothing was written; the heal is tried again later |

### Exposure reasons {#exposure-reasons}

For an **Expose to internet** switch that is on but opened nothing, `GET /api/system/addresses` gives the reason as one of `no_auth`, `web_no_auth`, `open_relay`, `shared_port`, `loopback`, `invalid_bind` and `not_listening`. [Securing the web interface](../settings/security.md) describes the conditions of each switch.

## MCP server {#mcp-server}

`POST /api/mcp` serves the [Model Context Protocol](https://modelcontextprotocol.io/) to AI applications, as JSON-RPC 2.0 over HTTP. [Connecting an AI assistant](../settings/mcp.md) covers the setup. Only `POST` is served: `GET` and `DELETE` return `405`, which is normal for this transport.

| Situation | Response |
| --- | --- |
| **Enable MCP server** is off | `404`, `mcp server is disabled` |
| An MCP token is set, and the request carries none or another one | `401`, `invalid or missing MCP token` |
| No MCP token is set | The web interface's login token is required, as for the rest of the API, while a username and password are set |
| The request carries an `Origin` header that is not allowed | `403` |

A request with an `Origin` header is accepted when that origin is b4's own address written as an IP address or `localhost`, or is listed in `system.web_server.mcp.allowed_origins`, where a single `*` accepts any origin. A host name is never accepted automatically: under DNS rebinding, a page on an attacker's name reaches b4 with matching `Origin` and `Host` headers. AI applications send no `Origin` header.

### Tools {#mcp-tools}

The server offers tools according to the two permission switches on the **MCP server** card and rebuilds the list when they change, without a restart. An application that was connected before the change keeps its old list until it reconnects. A tool that is not permitted is not offered at all, and the server tells the model which permission is off, so the model can say what to turn on.

| Permitted | Tools offered |
| --- | --- |
| Nothing (default) | 14 |
| **Allow configuration changes** | 18 |
| **Allow active probes** | 16 |
| Both | 20 |

| Tool | What it does | Needs |
| --- | --- | --- |
| `b4_status` | Version, capture engine, firewall backend, how many sets exist and are enabled, the state of Telegram over WebSocket, uptime, the connections counted since the last counter reset, and which permissions are on | - |
| `b4_metrics` | Connections in the last complete minute, in sets and not in a set, the totals since the last reset, dropped resets, blocked DNS lookups and connections, b4's CPU and memory use. Connections count as on the [dashboard](../dashboard.md#what-a-connection-count-covers) | - |
| `b4_get_config` | The configuration, or one section of it | - |
| `b4_list_sets` | Every set in priority order, with domain counts and primary strategy | - |
| `b4_get_set` | One set in full | - |
| `b4_check_domain` | Which sets target a domain, how the match was made, and whether the set is enabled | - |
| `b4_recent_connections` | Connections b4 processed, with the set that matched each | - |
| `b4_logs_tail` | The tail of `errors.log`, or of `update.log` with `file=update` | - |
| `b4_diagnostics` | OS, kernel, interfaces, firewall backend and the rule groups b4 installed | - |
| `b4_get_topic` | The description of a setting, see [Topics](#topics) | - |
| `b4_list_writable_paths` | Which settings can be changed, with types, current values and accepted values | - |
| `b4_geo_lookup` | Which GeoSite and GeoIP categories exist, what one holds, and which cover a domain or an address | - |
| `b4_api_reference` | The REST API's operations, or one of them with its parameters, body and responses, see [below](#b4-api-reference) | - |
| `b4_watchdog` | The watchdog's verdicts, and its actions, see [below](#b4-watchdog) | Depends on the action |
| `b4_set_config_value` | Changes one setting and applies it at once, reporting the previous and the new value | Allow configuration changes |
| `b4_revert_last_change` | Restores the configuration from before the last change made over MCP | Allow configuration changes |
| `b4_edit_set_targets` | Adds or removes domains, addresses, geo categories, ASNs or source devices of one set | Allow configuration changes |
| `b4_manage_set` | Creates, duplicates, moves, enables, deletes or resets a set | Allow configuration changes |
| `b4_test_domain_now` | Fetches a domain through b4 and again with b4 bypassed, and says which of the two works | Allow active probes |
| `b4_find_bypass_strategy` | Runs Discovery for a domain, a URL or a set, and applies the result | Allow active probes to start a run, Allow configuration changes to apply it |

`b4_check_domain` answers whether a domain is configured in a set. `b4_recent_connections` answers whether traffic for it arrived and which set matched. The difference separates a targeting mistake from a routing one.

A ready-made prompt, `diagnose_domain`, comes with the tools. It takes a domain and walks the model through the status, the coverage, the configuration and the firewall rules, in that order. Applications that support prompts list it separately.

### `b4_watchdog` {#b4-watchdog}

Reading the watchdog's verdicts emits no traffic and changes nothing, so the tool is offered at every level, and each action is permitted on its own. Without `set`, the actions work on the [older per-domain list](../watchdog.md) and the global watchdog switch:

| Action | Effect | Needs |
| --- | --- | --- |
| `status` | The last verdict for every watched set and domain | - |
| `add` | Adds a domain to the list | Allow configuration changes and Allow active probes |
| `remove` | Removes a domain from the list | Allow configuration changes |
| `enable` | Turns the global watchdog switch on, for the watched sets and the list | Allow configuration changes and Allow active probes |
| `disable` | Turns the global watchdog switch off | Allow configuration changes |
| `check` | Checks a domain of the list now | Allow active probes |

With `set`, a set id or exact name, the actions work on that set's own watchdog and its [Discovery addresses](../sets/discovery.md), passed as `url`:

| Action with `set` | Effect | Needs |
| --- | --- | --- |
| `status` | The set's watchdog status, reason and per-address results | - |
| `enable` | Switches the set's watchdog on and schedules a check; refused when the set cannot be watched | Allow configuration changes and Allow active probes |
| `disable` | Switches the set's watchdog off | Allow configuration changes |
| `add` | Adds `url` to the set's Discovery addresses | Allow configuration changes and Allow active probes |
| `remove` | Removes `url` from them | Allow configuration changes |
| `check` | Schedules a check of every address of a watched set and clears its cooldown | Allow active probes; a give-up is cleared only when Allow configuration changes is on as well |

### `b4_find_bypass_strategy` {#b4-find-bypass-strategy}

`domains` takes up to five domains or full URLs, separated by commas, as the [Discovery](../discovery.md) input does: a domain is fetched as `https://<domain>/`, a URL as given. A host that only serves files, such as an image or video CDN, can answer `/` with HTTP 400 and leave the run nothing to test. Such a host is probed by the URL of a file it serves. A run also tries the strategies the [community hub](../community/index.md) publishes for the domains, as a run from the Discovery page does when the hub is on.

`action=start` with `set` runs Discovery for that set: on the domains given, or on the set's Discovery addresses, at most five. The set's current strategy is tested first, and the search stops at the first strategy confirmed on every address. `action=status` reports the verdict, and `action=apply` with `set` writes the result into the set, the strategy only and only for the verdict `covered`. The same applies to `action=apply` without `set` when the run it refers to was a run for a set. A set that `action=apply` creates for free-form domains keeps the addresses they were found on as its Discovery addresses. A set with routing enabled is refused, and so is a result whose probes took another DSCP path than the set's traffic takes now, as on the [Discovery page](../discovery.md#applying).

A run also tests the other hosts each confirmed page loads, as the [page hosts](../discovery.md#page-hosts) step on the Discovery page does. For a found domain, `action=status` names the hosts that fail without b4 and load with its strategy, and those that stay blocked with it. `action=apply` does not add them. `b4_edit_set_targets` with `kind` `sni_domains` does. While that step runs, `action=apply` is refused until the run ends, and `action=cancel` ends only the step: the run completes with every result kept.

### Changing settings {#writable-settings}

With **Allow configuration changes** on, these become writable:

- every setting inside a set: targets, fragmentation, faking, TCP and UDP, DNS, escalation and routing;
- the MTProto and SOCKS5 settings, the Telegram over WebSocket switch `system.mtproto.bridge.enabled` among them;
- the logging settings, so the model can raise the log level, reproduce a problem and read the result.

`b4_list_writable_paths` reports the exact paths with their types, current values and accepted values. A change goes through the same validation and live-apply path as a save in the web interface. An invalid result is rejected and nothing is saved, and an accepted one applies at once, firewall rules included. Writing a list, such as a set's domains, replaces the whole list, so a model reads the current value first and sends it back in full.

`b4_edit_set_targets` adds and removes single entries instead, with `kind` naming the list: `sni_domains`, `ip`, `geosite_categories`, `geoip_categories`, `asns` or `source_devices`. An `asns` entry is written as `AS15169` or `15169` and stored as the bare number; reserved numbers are refused. An ASN whose prefixes b4 has not fetched yet is added anyway and fetched at once, and the reply names it, because until the fetch succeeds the set matches none of its addresses.

Whatever the switches say, the credentials of the web interface, SOCKS5, MTProto and a set's upstream proxy are refused, and so are these:

| Refused | Reason |
| --- | --- |
| Web server settings | Moving or locking the interface removes the way to undo the change |
| The MCP settings | The model cannot widen its own permissions |
| Capture engine and TUN | Switching it under a live network can cut the machine off |
| Firewall backend | A wrong value leaves the machine with no rules at all |
| Every **Expose to internet** switch, and any write that would open a port while one of them is on, such as turning the MTProto proxy on or changing its port | An open port is reachable from the whole internet at once, and a revert does not undo what reached it |
| Packet marks and routing tables | b4's own traffic depends on them |
| **Set DSCP** (`system.tables.dscp`) and a set's own DSCP (`sets[].dscp`) | Rules on the next router act on the value, and a wrong one can send traffic down another route |
| A set's id | Escalation targets refer to it |
| A set's Discovery addresses and watchdog switch | Both make the router fetch sites on a timer; `b4_watchdog` with `set` changes them under its own permissions |
| The log directory and the geodata file locations | A wrong log directory silently stops file logging, and a wrong geodata path empties every category at once |

The dividing line is recoverability: a wrong value inside a set breaks some sites, visibly and reversibly, while a wrong web server port or capture engine can leave the machine unreachable. The writable areas are named as whole subtrees, and the exclusions are marked on the fields themselves, so a setting added to b4 later stays unwritable until it is opted in. Refusing a location does not refuse the contents: `b4_logs_tail` reads both log files wherever the directory points.

`b4_revert_last_change` restores the configuration from before the most recent change made over MCP since b4 started, and applies it. Repeating it walks further back. It is refused, and the change stays on the list, when the configuration changed after that change, by a watchdog heal, the web interface or another tool. It is also refused when it would switch a set's watchdog or the global watchdog on, or bring back watched domains or the Discovery addresses of a watched set, while **Allow active probes** is off.

### Topics {#topics}

Several b4 settings have names that read as something other than what they do, and a zero usually means "use the fixed value" rather than "off". b4 carries a written description of such settings, and the server tells the model to read it before explaining or changing anything. The descriptions come from `src/ai/topics.json`, built into the binary; the web interface's AI explanations use the same text.

The descriptions are published twice:

- the tool `b4_get_topic`, with `topic` for an exact key, `path` for the setting a `b4_set_config_value` path names (in `sets[video].tcp.win.mode` the set is ignored), or `query` for a search. With no arguments it lists every documented key;
- the resources `b4://topics/<key>`, with the same text.

Most applications never show resources to the model; LM Studio and the OpenAI-compatible bridges list them for the user. The tool makes the text reachable for the model. A question about a setting without a description returns a note saying so, with the documented settings nearby. The note tells the model to say it is unsure rather than guess the setting's unit, default or meaning from its name.

### `b4_api_reference` {#b4-api-reference}

The tool gives the model the reference that the [API reference](/swagger) page shows, from a copy built into b4 that matches the installed version. It covers what the other tools leave out, and the model makes the API calls itself. The tool only describes the API.

| Arguments | Answer |
| --- | --- |
| None | Every operation, by tag |
| `query` | The operations whose path, summary or description contain every word given |
| `operation`, as `PUT /api/sets/{id}` | The operation's parameters, request body and responses, with the types they use. A path with a real value, such as `/api/sets/abc123`, names the same operation |
| `schema` | One type from an earlier answer, with its fields |

A type too large for one answer is listed by name, for a later call with `schema`. An operation that another tool also performs names that tool. A REST write passes b4's own validation, but not the checks and refusals that the MCP tools add.

Apart from `schema`, each answer also explains how to call the API. While a username and a password are set, that is the [login](#authentication), and the model is told to ask the user before using them. The MCP token is refused outside `/api/mcp`.

### What b4 records {#what-b4-records}

Every tool call writes one line to b4's log, shown on the **Logs** page: the tool, the caller's address and how long it took. A refused call is logged as a warning. A request turned away at the endpoint is logged too: the server off, a wrong or missing token, or a browser origin that is not allowed. The token itself is never written. Repeated refusals are collapsed into one line every 30 seconds, with a count. These lines go to the live log and the console, not to `errors.log`, which holds errors only.

A change made or reverted over MCP also appears under **Recent changes** on the [dashboard](../dashboard.md#recent-changes) as **AI agent changed a setting**, with the path of the setting and without its value.

### What is stripped {#what-is-stripped}

Tool output may be forwarded to a third-party model, so b4 removes these values from it or replaces them with `[redacted]`:

- the username and password of the web interface;
- the MCP token;
- the SOCKS5 username and password;
- the names and values of the MTProto secrets;
- the IPinfo token;
- the reference to the stored AI API key;
- the username and password of a set's upstream proxy;
- the path of the web server's TLS key.

Host names and URLs are returned as configured. **Download safe copy** under [Settings > System > Backup](../settings/system.md#safe-copy) masks those as well. `b4_diagnostics` holds no credentials, but it reports the hostname, every interface address and the live firewall ruleset, which is enough to identify the network.
