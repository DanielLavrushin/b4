---
sidebar_position: 50
title: Updating, backing up and removing
description: Updating b4 from the web interface or with the installer, backing up its settings, and removing it.
---

b4 updates from the web interface or with the installer script. An update keeps the settings, and a backup made before it is the usual precaution.

## Updating from the web interface {#web-interface}

The bottom of the side menu shows the running version. When a newer stable release is out, it reads **Update available**, with both version numbers.

1. Download a backup under **Settings > System > Backup**, see [Backing up the settings](#backup).
2. Click the version at the bottom of the side menu. The **Version Management** dialog opens.
3. Pick a release in **Select Version**. **Include pre-releases** adds test builds to the list.
4. Click **Upgrade**, or **Downgrade** for an older release.

b4 runs the installer, which downloads the release, replaces the binary and restarts the service. The page reloads when b4 is back. The file `update.log` in the log directory, `/var/log/b4` by default, records the last update started here. **Don't Show Again** hides the notice about that release in the current browser.

The dialog cannot update b4 in Docker, where it shows the `docker pull` command instead, see [Docker](docker.md#management). It also refuses to update a b4 that was started by hand rather than as a service.

When the router cannot reach GitHub, [Downloading b4 when GitHub is blocked](../advanced/update-mirrors.md) describes mirrors and **Install from a file instead**.

## Updating with the installer {#installer}

Over SSH, as root:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --update
```

To a specific release, older ones included:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --update v1.46.5
```

The installer sets the current binary aside, stops the service, puts the new version in place and starts it again. A new binary that does not run is replaced by the previous one. The settings stay as they are.

On MikroTik and in Docker, an update replaces the container, see [MikroTik](mikrotik.md#update) and [Docker](docker.md#management).

## Backing up the settings {#backup}

The **Backup** sub-tab under **Settings > System** holds the backup tools, described under [System](../settings/system.md#backup):

| Button | What it does |
| --- | --- |
| **Download Backup** | Saves the configuration directory as a `.tar.gz` archive |
| **Upload & Restore** | Writes such an archive back. A restart applies it |
| **Download safe copy** | Saves `b4.json` alone, with passwords and tokens masked, for sharing |

The backup archive holds the passwords, tokens and secrets unmasked, so it is kept private.

An update started from the web interface also saves a copy of `b4.json` next to it, named `b4.json.bak.v<version>` after the version it replaced.

## Removing b4 {#remove}

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --remove
```

The installer stops b4, clears the firewall and routing rules b4 installed, and removes the service and the binary. When b4 cannot be stopped, nothing is removed. At the end the installer asks whether to delete the configuration directory, `/etc/b4` or `/opt/etc/b4`, and keeps it unless the answer is yes.

:::warning
`--remove --quiet` deletes the configuration directory without asking. Without a terminal, such as under cron or over `ssh` without `-t`, `--remove` stops with an error unless `--quiet` is given.
:::

## Diagnostics {#diagnostics}

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh -s -- --sysinfo
```

The installer prints the system, the installed version and the state of the kernel modules. In the web interface, **System Info** under **Settings > System > Service** shows b4's own report, see [System Info](../settings/system.md#system-info).

## Unverified TLS {#unverified-tls}

When neither `curl` nor `wget` can verify GitHub's certificate, and installing CA certificates does not help, the installer asks before it downloads over unverified TLS. With `--quiet` it stops instead. `B4_ALLOW_INSECURE_TLS=1` in the environment allows the download for that run. A checksum fetched over the same connection only shows that the download is not corrupted.
