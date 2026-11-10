---
title: Sonarr and Radarr
description: What comes over from Sonarr and Radarr, how folders are matched, and what to check after the move.
level: Setup
sidebar: { order: 2 }
---

Move your shows from Sonarr and movies from Radarr, with their folders, files and quality choices, without searching again.

## Before you start

- Have each app's address and API key (in that app's settings), or its backup zip.
- Map your media folders into HolyArrs. The old app's folder names can differ; you match them in **Choose**.

## Move Sonarr and Radarr

1. Open **Settings > Move from other apps** and click **Add an app**.
2. Pick **Radarr** or **Sonarr**, type its address and API key, and click **Check connection**. You should see the app's name and version.
3. Start the read. Repeat for the other app.
4. In **Choose**, match each old folder to a Library folder here. You should see **Suggested** next to folders HolyArrs filled in.
5. Click **Try it**, check the counts, then **Move**.

:::tip
Old apps often run in other containers and see folders under other names. A rule like `/data/media/movies` to `/movies` fixes every title in that folder at once.
:::

## What comes over

| From Sonarr or Radarr | In HolyArrs |
| --- | --- |
| Library folders | Library folders for movies and shows. |
| Movies and shows | Titles, matched by TMDB or TVDB id. Folders and files stay where they are. A watched title gets **Keep up to date**; seasons that were not watched stay off. Nothing is searched for during the move. |
| Quality profiles | Each one matched to the closest **Pick rules** preset (Standard, 4K). **Try it** shows why. Titles use the preset of their profile. |
| Exclusions | The **Never-add** list. |
| Radarr's release stage per movie, and its availability delay | Each movie's **Release gate** and gate delay, up to 365 days. Your default for new movies stays. |
| The untagged default wait rule | The preferred protocol and Usenet and torrent waits on the matched Pick rules, up to 7 days. See [Waits](#waits). |
| Radarr TMDB lists, Trakt lists and Trakt watchlists | **Outside lists**, added paused, with folder, Pick rules, Keep up to date, search on add, Release gate and read interval (6 to 168 hours). A Trakt list must be public and needs your Trakt key. |
| Torznab, Newznab and RSS indexers | **Sources**, added paused and blocked from your home network. |
| Download clients (qBittorrent, SABnzbd, Transmission, Deluge, NZBGet) | **Downloaders**, added paused and blocked from your home network. Each keeps the old app's category and, on qBittorrent, its post-import category as **Category after import**, and manages only those two (**Only manage HolyArrs' own categories**). A client without a category uses `holyarrs`. When Sonarr and Radarr share one client, it comes over once and uses HolyArrs' own `holyarrs` category (and `holyarrs-imported` after import, when they moved imported torrents), so it never touches their torrents; their categories are on the **not moved** list. |
| Download handling | Minimum free space, import extra files and their types, and the RSS sync interval become **Settings > Naming and files** and the feed check. When Sonarr and Radarr differ, the stricter one wins: more free space, extra files on with both lists of types, the shorter interval (10 to 120 minutes). Propers and repacks other than prefer and upgrade, hardlinks off, completed download handling off and redownload failed off are listed as not moved, since HolyArrs always works the same way there. |
| Remote path mappings | **Different folder names** on the Downloader at that host, up to 100 pairs. A Downloader that was already here is left alone, and its pairs are listed for you to add by hand. A pair whose folder HolyArrs cannot see yet is still added and marked "not visible here yet (check your mounts)". |
| Naming | HolyArrs naming, where a pattern word has a match. Nothing on disk is renamed. |
| Discord, webhook and email notifications | **Alerts**, off until you type the link, address or mail password. |

An indexer is a site that lists releases; in HolyArrs it is a **Source**.

## Choices

### Folders

In **Choose**, each old folder that is not a Library folder here gets a row. Pick the matching Library folder, or **Other path…** and type one. When the old folder is missing here but a Library folder with the same last name exists, HolyArrs fills it in as **Suggested**.

Under **Advanced**, add path rules. The longest matching rule wins. Titles whose folder is still not found are listed with the reason; fix the rule and move again. Nothing is added twice.

### Waits

The old default wait rule comes over only when it differs from that app's own default, and only while those Pick rules keep the waits they came with. "Skip the wait at the top quality" turns on **Skip the wait once good enough**. If two apps set different waits for the same Pick rules, neither is used. **Try it** shows the change and how many versions use those Pick rules.

## What stays behind

Custom formats, tag-scoped wait rules (listed with their waits, so you can set them under Pick rules), release word rules, language profiles, tags, import lists from Sonarr (Outside lists add movies only), list kinds HolyArrs does not read (IMDb, Plex, another Radarr, Trakt watched or collection lists), metadata writers, qBittorrent's and SABnzbd's own category lists and notification kinds other than Discord, webhook and email. Each is on the **not moved** list with its reason. Auto-tag rules and history are not read.

:::caution
A Downloader only one old app used works in that app's category. Stop that app, or turn off its completed download handling, before you resume it, or both apps import the same downloads. A client Sonarr and Radarr shared uses HolyArrs' own categories instead, so it can run beside them. A clean-up script that removes torrents by category needs `holyarrs-imported` added.
:::

## If it does not work

| Symptom | Cause | Fix |
| --- | --- | --- |
| **Check connection** fails | The address works from your computer but not from the HolyArrs server. | Use the address the server can reach. For a home-network app, turn on the local network switch. |
| Titles show as not found | No path rule matches their folder. | Add a rule under **Advanced** in **Choose**, then move again. |
| A path pair says "not visible here yet (check your mounts)" | HolyArrs cannot see that folder. | Add the mount to the container. The pair is already saved. |

## For experts

<details class="ha-more"><summary>Remote path mappings in detail</summary>

A Downloader that was already here is left alone; its pairs are listed for you to add by hand. A pair whose folder HolyArrs cannot see yet is still added and marked "not visible here yet (check your mounts)".

</details>

## Related

- [Move from other apps](index.md)
- [Read your old apps](what-is-read.md)
- [Lidarr, Bazarr and Prowlarr](lidarr-bazarr-prowlarr.md)
