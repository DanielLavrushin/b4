---
sidebar_position: 6
title: Import from another tool
---

b4 reads a byedpi or zapret command line and turns it into sets. Open the **Sets** page and press **Import**.

This is separate from the **Import/Export** tab inside the set editor, which moves a b4 set between b4 installations as JSON.

:::info b4 is not a fork
b4 is an independent implementation. It does not share code, configuration format, or a bypass engine with byedpi or zapret, and the three tools name almost nothing the same way. The import is a translation, not a transfer: some options land exactly, some land approximately, and some have no counterpart at all. Every option is listed with which of those happened to it.
:::

## What can be pasted

The import accepts the command line in the forms it is usually shared in:

- a bare option list: `-Ku -a1 -An -s1+s`
- with the program name: `ciadpi -s1+s` or `nfqws --dpi-desync=fake`
- a systemd unit: its `ExecStart=` and `Environment=` lines
- a shell variable from a config file: `NFQWS_OPT="--dpi-desync=fake ..."`
- a whole configuration file, pasted unedited: `/opt/zapret/config`, `/opt/etc/nfqws/nfqws.conf`, or the Windows `.bat` that starts `winws`

A configuration file is read the way its launcher reads it. Values that span several lines, `$VAR` and `%VAR%` references, `\` and `^` line continuations, and `#`, `rem` and `::` comments are all resolved before anything is parsed. Variables that carry no options, such as `USER`, `TCP_PORTS` or `CONFIG_VERSION`, are left alone rather than read as arguments.

### How a configuration file becomes profiles

A zapret configuration rarely holds a single command line. nfqws-keenetic keeps the TCP, QUIC, UDP, ipset and custom strategies in separate variables and joins them in a fixed order with `--new` between them. The import reproduces that order, so the sets match the profiles the source machine actually runs. The report names the layout that was recognised and the variables it used.

Three things in the same file are reported rather than converted:

- **Commented-out alternatives.** A configuration usually keeps several `#NFQWS_ARGS=` lines above the active one. The report counts them. Uncommenting the wanted one and analysing again imports it instead.
- **Variables belonging to another daemon.** `TPWS_OPT` and `TPWS_SOCKS_OPT` configure tpws, which has its own option syntax. They are excluded from the nfqws conversion and named in the report.
- **Host list placeholders.** `<HOSTLIST>` and `<HOSTLIST_NOAUTO>` are substituted by the zapret scripts before nfqws starts. They become a host list the import cannot read, and the set asks for its domains.

:::info An unknown tool is refused, not guessed
Only byedpi and zapret are understood. A command line for a different tool is rejected with a message naming it, rather than being read against whichever option table happens to match best. zapret2 is recognised and refused by name: it keeps its bypass strategies in Lua scripts (`--lua-desync`, `--payload`) rather than in options, so there is nothing in the command line to translate.
:::

Supported tools:

| Tool | Versions |
| --- | --- |
| byedpi (`ciadpi`) | 0.13, and 0.15 - 0.17 |
| zapret (`nfqws`) | current |

The tool and version are detected from the options used. byedpi changed the meaning of several letters between 0.13 and 0.15, so when the line contains nothing that identifies the version, the import says the version was guessed and you can set it by hand.

## Steps

1. Paste the command line and press **Analyze**. Nothing is saved at this point.
2. Read the report. Each profile from the source becomes one set, listed with every option it contained.
3. Fill in the domains for each set (see below).
4. Press **Create sets**.

:::warning Importing moves domains away from existing sets
A domain resolves in one set only. When an imported set claims a domain that an existing enabled set already targets, the domain is removed from that set, and the message after the import names the sets that lost domains. Imported sets are also placed ahead of the existing ones in the set order.
:::

## Profiles become sets

byedpi and zapret both split a configuration into profiles. Each one becomes a separate b4 set, in the same order.

The two tools use profiles differently, and the import follows each:

- **byedpi** starts a profile with `-A/--auto`, which means "try this if the previous attempt failed". That is a retry chain, so the sets are linked with [Escalation](./escalation). b4 escalates on repeated RSTs; byedpi's HTTP-redirect and TLS-error triggers have no counterpart and are reported as approximated.
- **zapret** starts a profile with `--new`, which means "an alternative, selected by its own filters". Those sets are independent, with no escalation between them.

A byedpi profile restricted to UDP with no filters of its own is merged into the set that handles the rest of the traffic, because one b4 set covers both protocols.

## Domains

A b4 set acts on the [targets](./targets) you give it. A byedpi command line usually carries no host list at all, and a zapret one normally points at a file on the machine it runs on, so the import asks for domains per set and creates a set without targets disabled.

Two rules explain what the report shows:

- **A set that exists only as the fallback of another one gets no domain field.** It is reached through the escalation link, not by matching. Giving it the same domains would make it compete with the set it is meant to back up.
- **Only one set can act on a given domain.** b4 picks one set per destination and does not fall back to the next one when the port filter does not match. Where several profiles share a host list and differ only by the port they filter, the later sets are created disabled and named in the report. Give each the domains it should handle, or delete the ones you do not need.

### Host list files

`--hostlist=/opt/zapret/ipset/zapret-hosts-user.txt` and `-H /etc/byedpi/hosts.txt` point at files on the machine running that tool. The import cannot read them. The set whose profile referenced the file shows the path next to its domain field. Open that file on the other machine and paste its contents in.

The same applies to a fake payload loaded from a file (`--dpi-desync-fake-tls=/opt/.../tls_clienthello.bin`). A built-in payload is used instead. To use the original, upload it under [Settings -> Payloads](../settings/payloads).

A payload written as a hex pattern is read where b4 has the same thing: `--dpi-desync-fake-tls=0x00000000` becomes b4's all-zero fake payload, and an all-zero `--dpi-desync-fake-quic` becomes the zero fill a set uses when no UDP payload is set. Any other pattern has no counterpart, since b4 carries a generated ClientHello, a preset, an all-zero payload or an uploaded capture.

## The report

Every option gets one row saying what became of it.

| Result | Meaning |
| --- | --- |
| **Mapped** | b4 has the same thing. The b4 fields it set are listed under the explanation |
| **Approximated** | The closest b4 behaviour was used. The row says how it differs |
| **No equivalent** | b4 cannot express this |
| **Not applicable** | The option configures the other tool's own process (listening port, buffer sizes, daemon mode) and means nothing for b4 |
| **No effect** | The option was parsed, but changes nothing. Either it does nothing in the source tool either, such as `--md5sig` in a byedpi profile that sends no fake packet, or the same option appears again later and only the last value applies |
| **Not recognized** | Not a known option for this tool and version |
| **Invalid** | The value could not be read |

The percentage above the report weighs mapped options fully and approximated ones partially. Options marked **Not applicable** are left out of it, since dropping a proxy listening port is not a loss of fidelity.

## What does not carry over

The larger gaps, in both directions:

- **Automatic TTL** (`--dpi-desync-autottl`, byedpi's hop count probing). b4 uses a fixed TTL for fake packets, taken from `--dpi-desync-ttl` when the source sets one and left untouched when it does not. Set it by hand or let [Discovery](../discovery) find it.
- **Plaintext HTTP tampering** (`--hostcase`, `--domcase`, byedpi's `--mod-http`). b4 does not modify plaintext HTTP.
- **Exclusion lists** (`--hostlist-exclude`, `--ipset-exclude`). Leave those entries out of the set targets instead.
- **Packet ranges** (`--dpi-desync-start`, `--dpi-desync-cutoff`, byedpi's `--round`). b4 uses a per-connection packet limit on the [TCP](./tcp/general) tab.
- **Negated port filters** (`--filter-tcp=~80`). A b4 port filter lists the ports to act on.
- **Proxy operation.** byedpi is a SOCKS5 proxy, so its listening address, buffer sizes, connection limits and daemon options describe a process b4 does not have.

## After importing

The result is a starting point, not a verified configuration. The provider, the route and the blocking method differ between the machine the configuration came from and this one.

Run [Discovery](../discovery) on the imported domains to confirm the strategy works here, and to find a better one if it does not.
