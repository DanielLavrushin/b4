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

`qemu-system-x86`, `qemu-utils`, `xorriso`, `openssh-client`, `curl`, and for `lab test` and
`lab ab` also `go` and `git`. With access to `/dev/kvm` a guest runs at close to native speed;
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
guest's own. `lab run <name> <script>` pipes any local script into the instance as root, which
is the way to drive a longer scenario: interfaces, a proxy set, rules deleted by hand, counts
read back.

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

## Limits

- Guests reach the internet through QEMU's user networking: outbound only, TCP and UDP, no
  ICMP. Nothing in a guest sits behind a DPI, so a lab says how b4's rules behave, never
  whether a strategy works.
- x86-64 guests only: the script starts `qemu-system-x86_64`.
- A firmware's own behaviour, such as KeeneticOS rewriting its firewall or Broadcom's flow
  cache, exists only on the real device.
- The packages `debian8` installs are not verified. Every key its apt trusts has expired, so
  provisioning turns the signature check off there, and they arrive over plain HTTP: do not
  provision it on a network you do not trust. `debian9` checks signatures.
