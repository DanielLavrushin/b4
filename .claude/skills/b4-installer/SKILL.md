---
name: b4-installer
description: Change b4's installer - the POSIX shell components in installer/ that make up install.sh (platforms, services, features, actions, lib helpers) - so it still runs on busybox routers (ASUS Merlin, Keenetic, OpenWrt, Padavan, MikroTik containers). Covers the busybox and POSIX rules, the download and checksum helpers, rebuilding install.sh, safe ways to test functions, and why installer changes ship without a changelog entry. Use before editing anything under installer/ or install.sh, or when an install, update, removal or --sysinfo run fails on a router.
---

# Installer

`install.sh` is served straight from `main`: users run
`curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh`. A change
reaches every new install and every in-UI update the moment it merges, with no release in
between. So installer-only changes get no changelog entry and no version bump, and they are
tested before the merge, not after.

## Layout

`installer/_build.sh` concatenates the components into `install.sh` and drops their
comment-only lines. Never edit `install.sh` by hand: edit `installer/`, then
`make build-installer` (or `make watch-installer` while iterating), and commit both.

| Part | What |
| --- | --- |
| `header.sh`, `main.sh` | Entry point and option parsing. Options are `--name=value`, never `--name value`. |
| `lib/` | `colors.sh`, `log.sh`, `utils.sh` (the helpers below), `wizard.sh`. |
| `platforms/<id>.sh` | Defines `platform_<id>_name`, `_match`, `_info`, `_check_deps`, `_find_storage`, then `register_platform "<id>"`. Detection order is in `_detect.sh`. |
| `services/<type>.sh` | `service_<type>_install`, `_remove`, `_start`, `_stop`, then `register_service`: openrc, sysv, entware, systemd, procd, none. |
| `features/<id>.sh` | `feature_<id>_name`, `_description`, `_default_enabled`, `_run`, `_remove`, then `register_feature`. |
| `actions/` | install, remove, update, sysinfo. |

## Shell rules

The target is busybox `ash`, often old: ASUS Merlin ships BusyBox 1.25.1. GNU-only syntax
fails there silently. Fix portability in the script; never tell users to install GNU tools.

- No bashisms: `[[ ]]`, `function`, `local`, arrays, `${var:1:2}`, `${var/a/b}`, `$'...'`, `<<<`,
  `&>`, `source`, `==` inside `[ ]`, `echo -e`. The existing code avoids `local` by prefixing
  globals per function (`_ff_src`, `_ba_char`).
- No `grep -P`, no `\s` or `\d`: use `[[:space:]]` and `[[:digit:]]`.
- No `od`, `hexdump` or `xxd`; read a byte with `_byte_at <file> <offset>`.
- Probe tools with `command_exists`, which falls back to `which` where the `command` builtin
  is missing.
- Downloads go through `fetch_file <url> <out>`: it writes `<out>.part` and moves it into place
  only when complete, and tries the update mirrors when GitHub fails. curl and both wgets
  truncate the target in place, so a direct download over an existing file can leave half a
  geosite. Verify archives with `checksum_gate`, which refuses an unverified binary in
  `--quiet` mode.
- After placing a binary on flash, `flush_disk` before anything can reboot: routers on USB
  flash have come back with a corrupted binary.
- Never trust `/tmp`: it can be `noexec` or too small (64 MB routers run out during an update).
  Run scripts as `sh <file>`, never `./<file>`.
- A value passed in from outside (a version from the web UI, an option) must not start with
  `-`: a version string `--remove` once uninstalled b4.

## Testing

- `sh .claude/skills/b4-check/check.sh` rebuilds `install.sh` into a temporary copy, compares it,
  parses it with `sh -n` and `busybox sh -n`, and flags the patterns above in added lines.
- Read-only on any box: `sh install.sh --help`, `sh install.sh --sysinfo`.
- A full run belongs in a disposable environment: a busybox or Alpine container
  (`docker run --rm -it -v "$PWD/install.sh:/install.sh:ro" alpine:latest sh /install.sh`), a
  QEMU OpenWrt image, or a test router. Install, update and remove each need a run.
- To test single functions, extract them with a sed range
  (`sed -n '/^fetch_file() {/,/^}/p' install.sh`) and source only those. Never source a copy
  trimmed by line count: `install.sh` ends with `main "$@"`, `exit 0` and a blank line, and
  cutting two lines left `main "$@"` in place, which ran a full unattended install. If the whole
  file must be sourced, delete the call by pattern (`sed '/^main "\$@"$/d'`) and check with grep
  that it is gone.

## Changelog

None for an installer-only change. When it ships together with a binary release, it may be
mentioned inside that release's bullet (`/b4-changelog`).
