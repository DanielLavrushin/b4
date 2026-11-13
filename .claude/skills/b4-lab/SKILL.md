---
name: b4-lab
description: Test a b4 change on another kernel or userland in a QEMU guest from toolkit/labs - which rung a question belongs on (namespace tests, a lab guest, a test box), starting a guest, running the netns tests there against main (lab test, lab ab), running the real binary, reading the results, adding a guest, and what a lab cannot prove. Use when a change adds, deletes or reorders ip rules, routes or firewall rules and has to hold on old kernels or an old iptables, when asked for a smoke test without a real router, when a report names a kernel or an iptables version the development machine does not have, and before saying that a kernel-version case cannot be tested.
argument-hint: "<guest> [test pattern]"
---

# Labs

`toolkit/labs` boots real kernels under QEMU. `toolkit/labs/README.md` is the reference for
the `lab` script; this file is how to use it for a b4 change.

## Pick the rung

Start on the lowest rung that can show the thing. A result from one rung does not stand for
another.

| Rung | Shows | Cannot show |
| --- | --- | --- |
| `make test-netns` | Rules and traffic on the development machine's kernel, in seconds | Another kernel, iptables or iproute2 |
| A lab guest | A real old kernel and userland, the real binary | Vendor firmware, a real DPI, hardware offload |
| A test box (`b4-deploy`) | The firmware's own quirks, real traffic | Anything quick or repeatable |
| A censored network (`b4-field-test`) | Whether a strategy gets through | Rule logic |

## Guests

| Guest | Kernel | Userland | What differs there, measured |
| --- | --- | --- | --- |
| `debian8` | 3.16 | iptables 1.4.21, iproute2 3.16, ipset 6.23 | `ip rule add` takes an identical rule a second time. No `/proc/thread-self` |
| `debian9` | 4.9 | iptables 1.6.0, iproute2 4.9, ipset 6.30 | `ip rule add` refuses a duplicate with `File exists` |

On both, `ip rule del fwmark V lookup T` without a mask deletes a rule stored as `V/<mask>`,
`nft` is not installed (tests of the nftables backend skip or fail), and `iptables-legacy` is
a wrapper around the only iptables.

## Run the tests

```sh
toolkit/labs/lab up debian8
toolkit/labs/lab ab debian8 'TestNetnsAPolicyRule|TestNetnsARebuilt'
toolkit/labs/lab ab debian9 'TestNetnsAPolicyRule|TestNetnsARebuilt'
toolkit/labs/lab down debian8
```

- **Use `ab`, not `test`.** About twenty netns tests fail on `debian8` and ten on `debian9`
  on main as well. `ab` builds the checkout's tests twice, as they are and with every changed
  non-test Go file under `src/` taken from `origin/main`, runs both in the guest and lists
  what fails only on one side. A new test that fails only on the base there is the proof a fix
  needs.
- **Exit status 2 is not a result.** It means a run did not finish (a crash, a lost
  connection), so the counts are partial and `ab` compared nothing. Read the log it names.
- **Give it a pattern.** A traffic test run inside the whole suite on these kernels loses a
  few connections with nothing changing, from state earlier tests leave in the namespace. A
  traffic test has to measure a quiet phase first and skip when it is not clean; run it on its
  own pattern for the number.
- From a worktree, run that worktree's `toolkit/labs/lab`, or point any copy at it with
  `B4_LAB_REPO=<path>`. `LAB_BASE` changes the base, `LAB_PKG` the package (`./nfq/`,
  `./ctnl/`).
- The first `lab up` of a guest downloads its image (about 700 MB) into `~/.cache/b4-lab`;
  later starts reuse it. A running guest is given 2 GB of memory: `lab down` it when done.
- In a worktree session every command has to be one plain call. `lab run <name> <script>`
  pipes a local script into the guest as root, so a scenario goes into a script file.

## Run the real binary

When the question is what the service does over time (the monitor, a rebuild, a restart), run
the binary: `scripts/b4-netns.sh` starts it in a namespace of its own inside the guest. Give
it a config with its own `system.logging.directory` and a short
`system.tables.monitor_interval`, and drive it from a script through `lab run`: build the
interfaces, start b4 in the background, read rule counts, delete a rule by hand and wait for
it, save a set through the API to force a rebuild, count again.

- Start a second instance for a long run, `lab up debian8 d8long`, so the first stays free
  for tests.
- Count what leaks after routing: a capture on the fake uplink's peer interface, or a counter
  in mangle `POSTROUTING`. An `-o` match in filter `OUTPUT` still shows the interface chosen
  before mangle rerouted the packet.
- A connection loop from the router side stays under 200 connections a second, the rate guard
  of a proxy set's `_out` chain; above it the excess leaves unmarked by design.
- Run the same scenario with a binary built from main. A scenario that cannot tell the two
  apart proves nothing.

## What a lab cannot show

Merlin's and KeeneticOS's own firewall rewrites, Broadcom's flow cache, a firmware `ip` that
differs from iproute2, and anything about a DPI. Say which rung a result comes from, and take
the question to a test box when it is about those.

## Adding a guest

Add one when a real question needs it, not ahead of time: a cloud image with a pinned
checksum, the seed kind it reads, a provision script (`toolkit/labs/README.md`, "Adding a
guest"). Put what was measured on it in the README's guest table and in the table above.
