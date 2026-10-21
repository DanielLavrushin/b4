---
sidebar_position: 6
title: Importing from zapret or byedpi
description: Turning a zapret or byedpi command line or configuration file into b4 sets.
---

b4 reads a byedpi or zapret command line, or a whole configuration file, and turns it into sets. b4 is an independent implementation that shares no code, configuration format or option names with either tool, so the import is a translation: some options land exactly, some approximately, and some have no counterpart. The report lists what became of each option.

This is not the **Import/Export** tab of the set editor, which moves a b4 set between b4 installations, see [Sets](./index.md#import-and-export).

## Requirements

- The command line or configuration file of byedpi (`ciadpi` 0.13 or 0.15 to 0.17) or zapret (`nfqws`). Any other tool is refused with a message that names it. zapret2 is refused too: it keeps its strategies in Lua scripts, not in options.
- The domains the source tool works on. A host list is a file on the other machine, and the import cannot read it.

## Setup

1. Open **Sets** and click **Import**.
2. Paste the source into **Command line**. It accepts:
   - an option list, such as `-Ku -a1 -An -s1+s`, with or without the program name;
   - the `ExecStart=` and `Environment=` lines of a systemd unit;
   - a variable such as `NFQWS_OPT="..."`;
   - a whole configuration file, unedited, such as `/opt/zapret/config`, `/opt/etc/nfqws/nfqws.conf` or the Windows `.bat` that starts `winws`.
3. Leave **Source tool** and **Version** on **Detect automatically**. When the report says **Version guessed** and the result looks wrong, pick the version by hand: byedpi changed the meaning of several options between 0.13 and 0.15.
4. Click **Analyze**. Nothing is saved yet.
5. Read the report, see [The report](#the-report).
6. Enter the domains, see [Domains](#domains). **Shared domains** go to the first set that takes targets and has no host list of its own, and each set has its own **Domains (optional)** field. The report warns when no set takes the shared domains.
7. Click **Create N sets**.

:::warning Importing moves domains away from existing sets
A domain works in one set only. When an imported set claims a domain that an enabled set already targets, the domain is removed from that set. The message after the import names the sets that lost domains. The imported sets are placed ahead of the existing ones.
:::

## Profiles become sets

byedpi and zapret split a configuration into profiles, and each profile becomes a separate set, in the same order.

- **byedpi** starts a profile with `-A/--auto`: "try this when the previous attempt failed". The sets are linked with [Escalation](./escalation.md), and only the first one needs domains. byedpi's triggers on an HTTP redirect or a TLS error are reported as approximated.
- **zapret** starts a profile with `--new`, an alternative chosen by its own filters. These sets are independent.
- A byedpi profile limited to UDP, without filters of its own, joins the set that handles the rest of the traffic, since one b4 set covers both protocols.

A configuration file is read the way its launch script reads it, so the sets follow the profiles the source machine runs. Commented-out alternatives, variables of another daemon such as tpws, and host list placeholders such as `<HOSTLIST>` are reported, not converted. An alternative is imported by uncommenting it and analyzing again.

## Domains {#domains}

A b4 set acts only on its [targets](./targets.md). A byedpi command line usually has no host list, and a zapret one points at a file on its machine, so the import asks for domains per set. A set left without targets is created disabled, unless it selects UDP packets by protocol.

- **A fallback set gets no domain field.** It is reached through escalation from the set before it, and the same domains would make it compete with that set.
- **A set that selects Discord or STUN packets gets no domain field either.** Such a set comes from a zapret profile with `--filter-l7` naming `discord` or `stun`. The profile covers UDP only and has no host list or ipset of its own. The set gets **(auto: STUN request)** as its fake payload. With a `--filter-udp` port list, it [selects UDP packets by protocol](./udp.md#protocol-selection) and is created enabled when the profile sends fakes. Without a UDP port list, it is created disabled and without protocol selection. Its report row says to add the ports, select the protocols in **Protocols** and enable the set. The Discord line `--filter-udp=19294-19344,50000-50100 --filter-l7=discord,stun --dpi-desync=fake` becomes the voice set of [Discord voice calls](../guides/discord-voice.md).
- **Only one set acts on a domain.** Profiles that share a host list and differ only by port become sets that are created disabled and named in the report. Each needs its own domains, or it can be deleted.
- **A host list file** (`--hostlist=...`, `-H ...`) is named next to the domain field of its set. Its contents are copied from the other machine and pasted there.
- **A fake payload file** (`--dpi-desync-fake-tls=/opt/.../tls_clienthello.bin`) is replaced by a built-in payload. The original can be uploaded under [Settings > Payloads](../settings/payloads.md). An all-zero hex pattern becomes b4's all-zero payload.

## The report {#the-report}

Every option gets one row that says what became of it. The percentage above the report weighs mapped options fully and approximated ones partially, and leaves out the options that do not apply.

| Result | Meaning |
| --- | --- |
| **Mapped** | b4 has the same thing. The b4 fields it set are listed under the explanation. |
| **Approximated** | The closest b4 behaviour was used. The row says how it differs. |
| **No equivalent** | b4 cannot express this. |
| **Not applicable** | The option configures the other tool's own process, such as its listening port or daemon mode, and means nothing for b4. |
| **No effect** | The option was read but changes nothing, in the source tool as well, or a later copy of it overrides it. |
| **Not recognized** | Not a known option for this tool and version. |
| **Invalid** | The value could not be read. |

These do not carry over:

- **Automatic TTL** (`--dpi-desync-autottl`, byedpi's hop count probing). b4 sends fakes with a fixed TTL, taken from `--dpi-desync-ttl` when the source has one.
- **Changes to plain HTTP** (`--hostcase`, `--domcase`, byedpi's `--mod-http`).
- **Exclusion lists** (`--hostlist-exclude`, `--ipset-exclude`). b4 has no exclusion list, so those entries are kept out of the set targets by hand.
- **Packet ranges** (`--dpi-desync-start`, `--dpi-desync-cutoff`, byedpi's `--round`). b4 has a per-connection packet limit on the [General](./tcp/general.md) sub-tab.
- **Negated port filters** (`--filter-tcp=~80`). A b4 port filter lists the ports to act on.
- **Proxy settings** of byedpi, which is a SOCKS5 proxy: its listening address, buffers and connection limits.

## Checking the result

The new sets appear on the **Sets** page ahead of the existing ones. The result is a starting point, not a verified configuration: the provider, the route and the blocking differ between the source machine and this one. A [Discovery](../discovery.md) run on the imported domains confirms whether the strategy works here, and finds a better one when it does not.

## When it does not work

**No set carries a strategy.** The report says that none of the options that do the bypass were recognized. This happens with a variant of the tool with a different option syntax. Such a configuration is worth sending to the [b4 issue tracker](https://github.com/DanielLavrushin/b4/issues).

**A set passes traffic unchanged.** The report warns when sets got no bypass options at all. Such a set changes nothing until a strategy is set on its [TCP](./tcp/index.md) tab or found by Discovery.

**The sites do not open.** The imported strategy does not work on this network. [Unblocking a site](../guides/unblock.md) shows the next steps.
