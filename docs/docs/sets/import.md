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
- a systemd line: `ExecStart=/opt/zapret/nfqws --qnum=200 ...`
- a shell variable from a config file: `NFQWS_OPT="--dpi-desync=fake ..."`
- several lines, with `#` comments and `\` line continuations

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

## The report

Every option gets one row saying what became of it.

| Result | Meaning |
| --- | --- |
| **Mapped** | b4 has the same thing. The b4 fields it set are listed under the explanation |
| **Approximated** | The closest b4 behaviour was used. The row says how it differs |
| **No equivalent** | b4 cannot express this |
| **Not applicable** | The option configures the other tool's own process (listening port, buffer sizes, daemon mode) and means nothing for b4 |
| **No effect** | The option was parsed, but does nothing in the source tool either. For example `--md5sig` in a byedpi profile that sends no fake packet |
| **Not recognized** | Not a known option for this tool and version |
| **Invalid** | The value could not be read |

The percentage above the report weighs mapped options fully and approximated ones partially. Options marked **Not applicable** are left out of it, since dropping a proxy listening port is not a loss of fidelity.

## What does not carry over

The larger gaps, in both directions:

- **Automatic TTL** (`--dpi-desync-autottl`, byedpi's hop count probing). b4 uses a fixed TTL for fake packets. Set it by hand or let [Discovery](../discovery) find it.
- **Plaintext HTTP tampering** (`--hostcase`, `--domcase`, byedpi's `--mod-http`). b4 does not modify plaintext HTTP.
- **Exclusion lists** (`--hostlist-exclude`, `--ipset-exclude`). Leave those entries out of the set targets instead.
- **Packet ranges** (`--dpi-desync-start`, `--dpi-desync-cutoff`, byedpi's `--round`). b4 uses a per-connection packet limit on the [TCP](./tcp/general) tab.
- **Negated port filters** (`--filter-tcp=~80`). A b4 port filter lists the ports to act on.
- **Proxy operation.** byedpi is a SOCKS5 proxy, so its listening address, buffer sizes, connection limits and daemon options describe a process b4 does not have.

## After importing

The result is a starting point, not a verified configuration. The provider, the route and the blocking method differ between the machine the configuration came from and this one.

Run [Discovery](../discovery) on the imported domains to confirm the strategy works here, and to find a better one if it does not.
