---
sidebar_position: 4
title: Geodat Settings
description: Sources, downloads and updates of the GeoSite and GeoIP databases that sets use as targets.
---

The **Geodat Settings** tab downloads and updates the GeoSite and GeoIP databases. With them, a set can target a whole category of domains or addresses instead of single entries, see [Adding sites to a set](../guides/add-sites.md).

![The Geodat Settings tab](/img/geodata/20261004000202.png)

## GeoSite and GeoIP

Both are databases in the [V2Ray](https://github.com/v2fly/v2ray-core) format that group entries into named categories.

- **GeoSite** (`geosite.dat`) groups domains. The `youtube` category, for example, holds youtube.com, googlevideo.com, ytimg.com and the other domains of YouTube.
- **GeoIP** (`geoip.dat`) groups IP ranges, by country or, in b4geoip, by service.

## Sources

The **Source** list on each card offers the presets that publish that database, and **Custom URL...** for any other address of a `.dat` file.

| Source | Contents |
| --- | --- |
| [Loyalsoldier](https://github.com/Loyalsoldier/v2ray-rules-dat) | Domains and IP ranges, China and worldwide |
| [RUNET Freedom](https://github.com/runetfreedom/russia-v2ray-rules-dat) | Domains and IP ranges for the blocking in Russia |
| [b4geoip](https://github.com/DanielLavrushin/b4geoip) | IP ranges by service, GeoIP only |

For networks in Russia, RUNET Freedom for GeoSite and b4geoip for GeoIP are the recommended sources. The installer offers both by default.

### b4geoip {#b4geoip}

The GeoIP database of the b4 project. It is built automatically from the prefixes each network announces, as [RIPE NCC](https://stat.ripe.net/) records them. It groups addresses by service, so a set can target one platform:

- cloud providers: AWS, Google Cloud, Azure, DigitalOcean, Hetzner, OVH, Scaleway, Oracle Cloud, Contabo, AEZA;
- CDNs: Cloudflare, Akamai, Fastly, CDN77;
- game companies: Roblox, Valve/Steam, Sony/PlayStation, Nintendo, EA, Riot Games, Ubisoft, Epic Games, Wargaming, Bungie, Take-Two, CCP;
- platforms: Telegram, GitHub, Apple, Adobe, Amazon, Blizzard.

## Destination Directory {#destination}

| Field | What it does | Default |
| --- | --- | --- |
| **Destination Directory** | The directory **Update** and **Upload** write the files to, always as `geosite.dat` and `geoip.dat`. b4 creates it when it does not exist. | The directory of the current files, `/etc/b4` when there are none |

The field is not saved with **Save Changes**. A new directory takes effect on the next **Update** or **Upload** of each card: the file goes to the new directory, and the copy b4 wrote at the old path is deleted. Until then, a hint under the field names both directories.

## Database cards {#database-cards}

The **Geosite Database** and **GeoIP Database** cards each manage one file. The chip in the card header shows its state:

| Chip | Meaning |
| --- | --- |
| **Active** | The file exists. Its size and modification time are shown under the path |
| **Not Found** | A path is set, but there is no file at it |
| **Disabled** | No path is set, and the database is off |

Under the header are the file path, or **Not configured**, and the source URL. An uploaded file shows **Local upload** instead of a URL.

| Control | What it does |
| --- | --- |
| **Source** | Picks a preset, or **Custom URL...** with the address in the **Custom URL** field. The choice is stored when **Update** succeeds |
| **Update** | Downloads the selected source into the destination directory. Sets get the new categories without a restart |
| **Upload** | Writes a `.dat` or `.db` file of up to 500 MB from the browser into the destination directory. An uploaded file has no source URL, so automatic updates leave it alone |
| **Remove** | Deletes the file and switches the database off, see [below](#remove) |

One download runs at a time. A download started while another one runs, from the other card or in the background, fails with `another geodata download is already running`.

### Removing a database {#remove}

**Remove** opens the **Remove geo database** dialog with the path of the file. Confirming deletes the file and clears the path and the source URL. The database stays off until the next **Update** or **Upload**, and automatic updates do not bring it back. b4 deletes only a file named `geosite.dat` or `geoip.dat`. A file under another name stays on disk, and the database is switched off all the same.

The dialog lists the sets that use categories from the database. While such a set exists, the database cannot be switched off. The file is deleted, but the save fails with `Failed to save configuration: Configuration is invalid`, and the next check for missing files downloads the file again. Removing the categories from those sets first lets the database go. On a router with little storage, removing a database that no set uses frees its full size.

## Updating {#updating}

**Update** replaces a database on demand. Background downloads fetch the stored source URLs, on the **Auto-update** schedule or to restore a missing file.

A download is checked before it replaces the current file. An error or block page, an empty or cut-short file, and a GeoIP file uploaded as GeoSite are rejected, and the current file stays in place. The new copy is written next to the current one, so an update needs free space for both. The files range from about 10 MB to more than 70 MB. When a direct download of a preset fails, b4 repeats it through the [update mirrors](../advanced/update-mirrors.md).

A file that is damaged anyway, for example by a full disk, costs only the categories b4 cannot read. Sets keep matching by their other targets, and the log names the categories and sets affected. A missing or damaged file is downloaded again from its source URL, whatever the **Auto-update** fields say.

### Auto-update {#auto-update}

| Field | What it does | Default |
| --- | --- | --- |
| **Refresh on startup** | Shortly after b4 starts, downloads every database that has a source URL | Off |
| **Schedule** | **Daily**, **Weekly** or **Monthly**: downloads every database that has a source URL once 24 hours, 7 days or 30 days have passed since the last run | **Off** |
| **Last run** | When a background download last refreshed every database that has a source URL. **Update** does not change it | - |

Both fields are saved with **Save Changes** and apply without a restart. Background downloads run only while the web server is on: with its port set to `0`, nothing is downloaded, missing files included.

## Using categories in sets {#using-in-sets}

Once a database has a path, the set editor shows its categories on the **Targets** tab: **Bypass GeoSite Categories** on the **Bypass Domains** sub-tab and **Bypass GeoIP Categories** on the **Bypass IPs** sub-tab. Each selected category shows how many domains or address ranges it holds, and clicking a GeoSite category opens a preview of its first 100 domains. [Targets](../sets/targets.md) describes how sets match by category.

## Tools

| Project | What it does |
| --- | --- |
| [GeodatExplorer](https://github.com/DanielLavrushin/GeodatExplorer) | A web application that shows the categories, domains and IP ranges in a `.dat` file, for checking a category before it goes into a set |
| [v2dat](https://github.com/DanielLavrushin/v2dat) | A command-line utility that unpacks `.dat` files into text lists, for scripts |
