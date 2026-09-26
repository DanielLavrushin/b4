---
sidebar_position: 5.5
title: Discovery
---

The **Discovery** tab of the set editor, between **Escalation** and **Import/Export**, holds the pages [Discovery](../discovery) tests for this set, the result of the last search for it, and the switch that puts the set under the [watchdog](../watchdog).

A set with routing enabled sends its traffic to a proxy or an interface and applies no bypass strategy, so for such a set the tab is read-only and says that Discovery does not apply to it.

## Discovery addresses

The list holds the pages Discovery fetches when it searches a strategy for this set, and the pages the watchdog checks when it watches the set. They are stored with the set as `sets[].discovery.urls`.

| Rule | Effect |
| --- | --- |
| At most 5 addresses | The field is disabled once the list is full. |
| One address per host name | A second address on a host already in the list is refused. |
| A bare domain | Becomes `https://<domain>/`. |
| A full URL | Keeps its path and query. The fragment (`#...`) is dropped. |
| Scheme | Only `http` and `https`. |
| Credentials | A user name or password in the URL is refused. |
| Local and private hosts | Refused: `localhost` and addresses in the loopback, private, link-local and CGNAT ranges. |

A strategy found on these addresses is written into the set, and from then on applies to every domain the set targets, although only these addresses were tested. For a set built on a GeoSite category that is every entry of the category. The useful addresses are pages that represent the set, such as the main page of each service it covers, and pages that do not open without b4: an address that loads without a bypass cannot show whether the set works.

Besides typing them here, addresses reach the list in these ways:

- a set created by [Discovery](../discovery#creating-a-set) keeps the addresses it was found on;
- **Use these addresses as the set's discovery URLs** in the Discovery apply dialog stores the addresses of a run on an existing set;
- **Remove them from this set's Discovery addresses** on a partial verdict drops the addresses the best partial strategy did not cover;
- **Watch through &lt;set&gt;** on the [older per-domain list](../watchdog#older-per-domain-list) of the watchdog adds that domain's address;
- the `b4_watchdog` tool of the [MCP server](../settings/mcp#tools) adds and removes addresses of a set.

### What stays on the router

Discovery addresses describe what a person checks on their own network, so they do not travel with the strategy:

| Where | What happens to the addresses |
| --- | --- |
| [Sharing](./sharing) and publishing to the Community Hub | Never sent. A shared set carries none, and the preview lists them among the settings left out. |
| **Update to version N** or **Reapply** of a set applied from the hub | The local addresses, and the watchdog switch, are kept. |
| Resetting the set | The addresses and the watchdog switch are kept. |
| The **Import/Export** JSON | The addresses are carried, the watchdog switch is not: a set imported from that JSON starts unwatched. |
| MCP | `b4_set_config_value` cannot write them or the watchdog switch. `b4_watchdog` with a set changes both under its own permissions. |

## Suggestions

**Suggest** loads up to ten candidate addresses, one per host, in this order:

1. the addresses already stored on the set;
2. addresses of earlier Discovery results that ran for this set or were applied to it, newest first;
3. sites from the [DPI Detector](../detector) list that this set matches;
4. the set's own domain names, as `https://<domain>/`; `regexp:` entries and entries that are not host names are skipped;
5. the main page of built-in services whose GeoSite category the set uses.

Each suggestion names its source, and **Add** puts it into the list. When the engine hands the host to another set, the suggestion names that set: a strategy written into this set does not affect that host. A set whose targets are only large lists, such as `ru-blocked`, usually gets no suggestion; the tab then says that no addresses are known for the set, and a page opened on those sites has to be added by hand.

The same suggestions fill the address field on the Discovery page when a set without addresses is picked there. **Suggest** and **Find a strategy** need a saved set.

## Finding a strategy

**Find a strategy** opens the Discovery page with this set picked and the addresses of the list in its field. A run for a set tests the set's current strategy first, confirms a strategy that loads every address on all of them together, and ends with one verdict for the set; see [A run for a set](../discovery#a-run-for-a-set). Unsaved changes to the set are lost when Discovery opens; the addresses in the list are passed to the search.

The **Find a strategy** item of the set card's menu on the Sets page does the same.

## Last search

**Last search for this set** shows the most recent Discovery run for the set: its verdict, when it ended, the strategy it found, and which addresses it covered and did not cover. **Open in Discovery** opens the Discovery page with the set picked. The record is kept in the Discovery history, one per set; see [History](../discovery#history).

## Watchdog

**Keep this set working with the watchdog** puts the set under the [watchdog](../watchdog). Its Discovery addresses are then checked on a schedule; when they keep failing, the watchdog runs Discovery for the set, writes a confirmed strategy into it, checks the addresses again, and undoes the change if they still fail.

The global watchdog switch and its timings under **Settings, Discovery** apply to every watched set. While the watchdog is off there, the set's switch can stay on but nothing is checked, and the tab says so.

A set is watched while all of these hold:

| Condition | When it does not hold |
| --- | --- |
| The set is enabled | The switch stays on; the set is not checked until it is enabled again. |
| The set has Discovery addresses | The switch stays on; nothing is checked until an address is added. |
| Routing is off | The switch is turned off when the set is saved. A routed set sends its traffic away from the direct path the watchdog heals. |
| The set is not limited to specific devices by an include list | The switch is turned off when the set is saved. The router's own check carries no device MAC and never matches such a set. An exclude list does not count. |
| The set has domain or GeoSite targets | The switch stays on, but the set is not checked. The watchdog decides which set handles an address by its host name, which a set of only IP addresses, GeoIP categories or ASNs cannot confirm. |

While a condition fails, the switch cannot be turned on, and a caption under it names the reason.

Once the set has been saved with the switch on and the global watchdog is on, the tab shows the set's watchdog status with its reason, when it was last checked, the last heal with the strategy it wrote, and a link to the Watchdog page. On the Sets page the card of a watched set carries a **Watchdog** badge coloured by the same status, with the status and reason in its tooltip.

:::warning
A heal writes into the set while the editor may be open on an older copy of it. The editor sends the revision of the set it loaded, and when the set was changed on the router since then the save is refused with a message offering a reload. Unsaved changes are lost on reload; the heal is not silently undone.
:::

:::info
The checks, the heal and its limits, and the statuses shown for a watched set are described on the [Watchdog](../watchdog) page.
:::
