# b4 labs

Virtual machines for testing b4 where a network namespace on the development machine is not
enough: another kernel, another iptables or iproute2, a userland as old as a router's. Each
guest is a small definition in `guests/`; the `lab` script downloads its image once, boots it
under QEMU and gives a root shell over SSH.

Where each kind of test belongs:

| Tool | What it covers | What it cannot |
| --- | --- | --- |
| `make test-netns` | Rules and traffic on the development machine's own kernel, in seconds | Any other kernel or userland |
| A lab guest | A real kernel and userland of another age, with the real binary running on it | Vendor firmware (Merlin, KeeneticOS), a real DPI, hardware offload |
| A test box | The firmware's own quirks, and on a box in a censored network a real DPI | Anything you need quickly or often |

## Requirements

`qemu-system-x86`, `qemu-utils`, `xorriso`, `openssh-client`, `curl`, `flock` (util-linux), and
for `lab test` and `lab ab` also `go` and `git`. With access to `/dev/kvm` a guest runs at close to native speed;
without it QEMU falls back to emulation and is much slower.

## Quick start

```sh
toolkit/labs/lab list                      # guests and running instances
toolkit/labs/lab up debian8                # download once, boot, provision, wait for ssh
toolkit/labs/lab ab debian8 TestNetnsAPolicyRule   # netns tests there: this checkout against origin/main
toolkit/labs/lab test debian8              # every netns test of ./tables/, this checkout only
toolkit/labs/lab ssh debian8               # root shell
toolkit/labs/lab ssh debian8 'uname -r'    # one command
toolkit/labs/lab down debian8              # stop, keep the disk
toolkit/labs/lab destroy debian8           # stop and delete the disk
```

`lab up <guest> <name>` starts a second instance of the same guest under another name, for
example one for a long run while the first runs a test suite.

Images, instance disks and the lab's SSH key live in `~/.cache/b4-lab` (`B4_LAB_HOME` moves
them). Nothing is written into the repository. `LAB_MEM` and `LAB_CPUS` override a guest's
memory in MB and its CPU count for one `lab up`.

## Guests

| Guest | Kernel | Userland | Use it for |
| --- | --- | --- | --- |
| `debian8` | 3.16 | iptables 1.4.21, iproute2 3.16, ipset 6.23 | The oldest kernels b4 meets. It takes an identical `ip rule` a second time instead of refusing it, and has no `/proc/thread-self` |
| `debian9` | 4.9 | iptables 1.6.0, iproute2 4.9, ipset 6.30 | A kernel between the old and the current ones: it refuses a duplicate rule, unlike `debian8`, but `ip rule del` without a mask still deletes a rule stored with one, as on `debian8` |

`nft` is not installed in either guest, their versions being too old for b4's nftables
backend, so tests of that backend skip or fail there. Both guests have `iptables-legacy` and
`ip6tables-legacy` as wrappers around their only iptables, so tests and configs that name
that engine run.

## Running b4's tests in a guest

`lab test <name> [pattern]` builds the test binary of `./tables/` from the checkout, copies it
into the instance and runs the tests matching the pattern (default `TestNetns`) as root in a
private network and mount namespace, the way `make test-netns` does on the development
machine. It prints the counts and the failed tests, and exits non-zero when one failed. A
subtest counts and is listed by its full path, `TestParent/child`.

`lab ab <name> [pattern]` is the one to use on these guests. On a kernel this old some tests
fail for reasons of their own, about twenty on `debian8` and ten on `debian9`, so a failure
means something only next to the same run on the base. `ab` builds the checkout's tests
twice: as they are, and with every changed Go file under `src/` that is not a test taken from
the base. It runs both and lists what fails only on one side. It exits non-zero when a test
fails only with the checkout.

A run that does not finish (the test binary crashes, the connection to the guest drops) has
partial counts. Both commands say so and exit with status 2, and `ab` compares nothing.

One `test` or `ab` runs on an instance at a time, because they share the instance's logs and
the binary's place in the guest. A second one is refused: start a second instance for it
(`lab up debian8 second`).

```sh
toolkit/labs/lab ab debian8 'TestNetnsAPolicyRule|TestNetnsTheStaleSweep'
LAB_PKG=./nfq/ toolkit/labs/lab test debian9 TestNetnsAnOverlapStaysOff
B4_LAB_REPO=/path/to/a/worktree toolkit/labs/lab ab debian8
```

- `LAB_BASE` is the revision to compare with, `origin/main` by default.
- `LAB_PKG` is the package whose tests run, `./tables/` by default.
- `B4_LAB_REPO` is the checkout to build, by default the one this script lives in.
- The full logs are in `~/.cache/b4-lab/logs/<name>-change.log` and `<name>-base.log`.
- Only Go files are swapped for the base build. A change to `go.mod` or to an embedded file
  is in both builds.

A traffic test is trustworthy when it has measured a quiet phase first: inside a whole-suite
run on these kernels a tight connection loop loses a few connections with nothing changing,
from state earlier tests leave in the namespace. Run such a test on its own pattern.

## Running the b4 binary in a guest

Build it for the guest, push it, and start it as root in a private namespace, with a config
whose `system.logging.directory` points somewhere of its own and a short
`system.tables.monitor_interval`:

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go -C src build -o ../out/b4.lab .
toolkit/labs/lab push debian8 out/b4.lab b4.json /root/lab
toolkit/labs/lab run debian8 toolkit/labs/scripts/b4-netns.sh /root/lab/b4.lab /root/lab/b4.json --verbose=debug
```

`scripts/b4-netns.sh` starts the binary in a network and mount namespace of its own inside
the guest and stays in the foreground until b4 exits. It makes the mounts private first: on a
guest this old `unshare -m` leaves them shared, and a tmpfs on `/run` would replace the
guest's own. The run also gets a `/tmp` of its own, where b4 keeps `/tmp/b4_sysctl_snapshot.<netns-inode>.json`
(or `/tmp/b4_sysctl_snapshot.json` on an older base build in `lab ab`) for the sysctl values
it will put back. Keep the binary and its config outside `/tmp` for that reason; `netns-test.sh` does
the same for a test binary. `lab run <name> <script>` pipes any local script into the instance as root, which
is the way to drive a longer scenario: interfaces, a proxy set, rules deleted by hand, counts
read back.

## A KeeneticOS stand-in

`scripts/keenetic-standin.sh` builds a router that looks like KeeneticOS where b4 meets it:
its internet access policies, the chain that marks a policy's devices, the per-WAN rules,
the run directory that makes b4 take the system for KeeneticOS, and the RCI on port 79. It
runs the real b4 binary there, so a question such as "does a device in a policy follow an
interface set" gets an answer from b4 itself, not from a model of b4's rules.

`scripts/b4-netns.sh` gives its binary an empty network namespace with a `/run` and a `/tmp`
of its own, and b4 decides whether it runs on KeeneticOS once, at start. So the stand-in is
the binary that `b4-netns.sh` starts: it builds the router in that namespace, makes
`/var/run/ndm`, starts the fake RCI, and then runs b4 as its child with the config and the
arguments that follow. Build b4 as for any run in a guest. The config needs one set with
routing on, output interface `nwg1` and `192.0.2.80` as its IP target; this `b4.json` is enough:

```json
{
  "version": 52,
  "sets": [
    {
      "id": "vpn",
      "name": "VPN sites",
      "enabled": true,
      "routing": { "enabled": true, "egress_interface": "nwg1" },
      "targets": { "ip": ["192.0.2.80"] }
    }
  ],
  "system": {
    "logging": { "directory": "/root/lab/kn-logs" },
    "tables": { "monitor_interval": 5 }
  }
}
```

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go -C src build -o ../out/b4.lab .
toolkit/labs/lab up debian9
toolkit/labs/lab push debian9 out/b4.lab b4.json toolkit/labs/scripts/keenetic-standin.sh /root/lab
toolkit/labs/lab run debian9 toolkit/labs/scripts/b4-netns.sh /root/lab/keenetic-standin.sh /root/lab/b4.json /root/lab/b4.lab --verbose=debug
```

The push keeps the script's executable bit, which `b4-netns.sh` needs. Without a b4 path
after the config the router runs alone, which gives the baseline. That run stays in the
foreground; drive it from a second terminal with the same script piped in:

```sh
toolkit/labs/lab run debian9 toolkit/labs/scripts/keenetic-standin.sh check
toolkit/labs/lab run debian9 toolkit/labs/scripts/keenetic-standin.sh rewrite
toolkit/labs/lab run debian9 toolkit/labs/scripts/keenetic-standin.sh rewrite 30
toolkit/labs/lab run debian9 toolkit/labs/scripts/keenetic-standin.sh rewrite stop
toolkit/labs/lab run debian9 toolkit/labs/scripts/keenetic-standin.sh show
toolkit/labs/lab run debian9 toolkit/labs/scripts/keenetic-standin.sh in router curl -s 127.0.0.1:7000/api/config
toolkit/labs/lab run debian9 toolkit/labs/scripts/keenetic-standin.sh stop
```

- `check` fetches `192.0.2.80` (the set's address) and `192.0.2.81` (an address outside it)
  on ports 80 and 443 from the default-policy device, the policy device and the router. Both
  addresses answer behind every uplink, and each listener replies with its uplink's name and
  the source address it saw: `vpn 10.250.20.2` came through `nwg1`, `isp2 198.51.100.10` by
  `eth2.5`, `isp1 203.0.113.10` by `eth3`.
- `rewrite` does what an NDMS rewrite does to b4: it replaces the whole mangle table, IPv4 and
  IPv6, with `iptables-restore` of the NDMS chain alone, so b4's chains and jumps are gone
  until its monitor puts them back. `rewrite <seconds>` repeats it in the background.
- `show` prints the ip rules with their tables, mangle `PREROUTING` and the hotspot chain with
  counters, the connections nat `POSTROUTING` counted per output interface, `rp_filter`, and
  the rewrites done.
- `in <where> <command>` runs a command in `router`, `default`, `policy`, `isp1`, `isp2` or `vpn`.
- `stop` stops b4, which removes its rules, and then the router. The control commands find the
  run by the pid in `/var/tmp/keenetic-standin`, which also holds the helpers' logs and
  `rci.log`, every request the fake RCI answered. One stand-in runs in a guest at a time.

What it models, and where each value comes from:

| What | In the stand-in | Source |
| --- | --- | --- |
| Policies | Five, n from 0 to 4: `fwmark 0xffffaaa+n lookup 4096+2n` at priority 100+2n, then `fwmark <the same> blackhole` at 101+2n; also `fwmark 0xffffa00 lookup main` at 10 | The rules of KeeneticOS 5.1.8 in issue 526 (on 4.x the same pairs sat at 1150 and 1151) |
| Routes | The main table's default route leaves by `eth3`; policy 0's table by `eth2.5`, the other policies' by `eth3`, and each policy table also holds the LAN route | Chosen for the stand-in: the report has the rules, not the routes |
| Per-WAN rules | `from <address> lookup` 16386 (`eth2.5`) at 1154, 16387 (`eth3`) at 1156, 16395 (`nwg1`) at 1172 | Issue 526 |
| Interfaces | `br0` 192.168.10.1/24, `eth3` 203.0.113.10/23, `eth2.5` 198.51.100.10/22, `nwg1` 10.250.20.2/32 | The anonymised System Info of issue 526 |
| Hotspot chain | `_NDM_HOTSPOT_PREROUTING_MANG` at the end of mangle `PREROUTING`, listing the policy device by MAC: `MARK --set-xmark 0x0ffffaaa/0xffffffff`, `CONNMARK --save-mark --nfmask 0xffffffff --ctmask 0xffffffff`, `RETURN` | `docs/docs/guides/marks.md` documents the whole mark by MAC address and its copy into the connection mark. The masks, the `RETURN` and a jump with no other match are assumed. The real name ends in `MANGL`, 29 characters; `iptables-restore` 1.6.0 on `debian9` refuses chain names over 28, so the stand-in drops the last letter. b4 never looks the name up |
| Detection | A `/var/run/ndm` directory inside the run | `src/tables/dnskernel.go` also takes `-ndm` in the kernel release or a `raw_out` table, which a stock kernel cannot show |
| RCI | `GET /rci/show/interface`, the batch `POST /rci/` for system names, `GET /rci/show/ip/hotspot` on 127.0.0.1:79, with the labels "Home network", "Provider", "LTE router" and "VPN" and two named devices | The reply shapes of `src/netif/keenetic_test.go`; the names are made up |
| NAT | `MASQUERADE` on `eth3` and `eth2.5` in nat `POSTROUTING`, after a counting rule per output interface | Chosen for the stand-in |
| ARP upstream | The uplinks answer ARP only for the addresses of their own interface (`arp_ignore` 1), so a route without a next hop through `eth3` or `eth2.5` fails, as it does in front of a real gateway | Chosen for the stand-in: with the default, an uplink answered ARP for the site addresses it holds, and a set table without a gateway still worked |

What it cannot model:

- NDMS itself: it is closed source. Its real chains and their order, the chains the stand-in
  leaves out (`_NDM_OUTPUT_DELAY`, which queues to 64511 with an `ndmmark` match stock
  iptables lacks; the mangle `INPUT` drop of TCP 443 to the router; NAT that depends on its
  own `FORWARD` mark), when and how often it rewrites, and which tables a rewrite touches.
- What RCI says about policies. The fake RCI answers only the three calls b4 makes and knows
  nothing of which policy a device is in.
- The WAN ping check, failover between WANs, and any route NDMS changes on its own.
- WireGuard. The guest kernels have no WireGuard module, so `nwg1` is a TUN device, carried
  by a small forwarder to `nwgpeer` in the `vpn` namespace. b4 counts both kinds as
  encapsulated and routes the LAN's connections alike, but not the router's own traffic:
  with `routing.router_traffic` on `auto` it leaves that traffic on the normal route for a
  TUN output interface and says so at start, and with `include` it adds a rate guard to the
  set's mangle `OUTPUT` chain and warns of a routing loop, which it does not for WireGuard.
  For the set's `OUTPUT` rules that b4 had on the router of issue 526, set
  `routing.router_traffic` to `include`.
- IPv6 routing: no IPv6 addresses or rules, only the same chain in the IPv6 mangle table.
- The `4.9-ndm` kernel itself and its `raw_out` table.

The stand-in has run on `debian9` only.

## Adding a guest

Add `guests/<name>.env`:

```sh
GUEST_SUMMARY="one line for lab list"
IMAGE_URL="https://.../image.qcow2"
IMAGE_SHA256="..."
GUEST_MEM=2048
GUEST_CPUS=4
GUEST_DISK=8G
GUEST_USER=root
GUEST_SEED=nocloud
GUEST_GOARCH=amd64
GUEST_PROVISION=<name>.provision.sh
```

- `IMAGE_URL` is a cloud image in qcow2 that runs cloud-init. Pin the checksum the publisher
  lists; `lab` refuses an image that does not match.
- `GUEST_SEED` is how the image takes its SSH key: `nocloud` (a `cidata` disk) or `configdrive`
  (an OpenStack `config-2` disk). Debian 9 reads the first, Debian 8 only the second. An image
  that waits two minutes at boot and then refuses the key wants the other one; its console is
  in `~/.cache/b4-lab/instances/<name>/console.log`.
- `GUEST_PROVISION` runs once as root after the first boot. `debian-archive.provision.sh`
  points an end-of-life Debian at `archive.debian.org` and installs what b4's tests use.

## Changing the lab

`sh toolkit/labs/selftest.sh` runs `lab` and the two namespace scripts against stubbed `ssh`,
`qemu` and `mount`, so it needs no guest and takes a second. It covers what a real run cannot
show on demand: a run that dies midway, a failing subtest, a setup step that fails, a port
taken between the check and the start. Run it after any change here; `b4-check` lists it.

## Limits

- Guests reach the internet through QEMU's user networking: outbound only, TCP and UDP, no
  ICMP. Nothing in a guest sits behind a DPI, so a lab says how b4's rules behave, never
  whether a strategy works.
- x86-64 guests only: the script starts `qemu-system-x86_64`.
- A firmware's own behaviour, such as KeeneticOS rewriting its firewall or Broadcom's flow
  cache, exists only on the real device. The KeeneticOS stand-in imitates the parts listed
  under it, no more.
- The packages `debian8` installs are not verified. Every key its apt trusts has expired, so
  provisioning turns the signature check off there, and they arrive over plain HTTP: do not
  provision it on a network you do not trust. `debian9` checks signatures.
