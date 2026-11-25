---
title: Built-in sites
description: "Add Knaben, TorrentsCSV, SubsPlease or IPTorrents as a Source, with no Jackett or Prowlarr in between."
level: Setup
sidebar: { order: 4 }
---

HolyArrs can read 4 torrent sites itself. You do not need Jackett or Prowlarr for them.

Each one becomes an ordinary Source. Priority, use and limits work as on any other Source. See [Sources](sources.md).

## The sites

| Site | For | Needs | Newest list |
|---|---|---|---|
| Knaben | Movies, shows, music | Nothing | Yes |
| TorrentsCSV | Movies, shows, music | Nothing | No, search only |
| SubsPlease | Shows (anime) | Nothing | Yes |
| IPTorrents | Movies, shows, music | Your own user ID and passkey, or a browser cookie and its user agent | Yes |

All 4 hand out torrents, so you need a torrent Downloader. See [Downloaders](downloaders.md).

## Add a built-in site

1. Open **Settings > Sources**.
2. Press **Add a Source**.
3. Set **Kind of feed** to **Built-in site**.
4. Pick the **Site**.

   You should see the name, **Address** and limits filled in for you.

5. For IPTorrents only, pick **Sign in with**, then fill in **User ID** and **Passkey**, or **Cookie** and **User agent**, and tick **Freeleech only** if you want it. See below.
6. Press **Save**. HolyArrs tests it right away.

   You should see **Working** under it.

Leave the **Address** as it is. Change it only if the site moved.

After saving, you cannot change the site of a Source. To switch, add a new one.

## How each site behaves

**Knaben** searches by title and sorts by seeders. Its newest list shows the latest uploads.

**TorrentsCSV** only searches. It has no newest list, so turning on **Newest list** for it finds nothing.

**SubsPlease** searches by the show's title alone, because it numbers episodes its own way. Its newest list shows the latest episodes.

**IPTorrents** is a private site. It searches with your own account. With a user ID and passkey, HolyArrs uses the site's own RSS: a search gives the 100 newest matches, with their seeders. With a browser cookie, it reads the site's search pages, the way Prowlarr reads them, sorted by seeders: a movie with an IMDb id is searched by that id, a whole season is searched as `S01*` so every episode matches, and Releases the site marks freeleech carry the `freeleech` flag.

## IPTorrents: user ID and passkey

HolyArrs never signs in for you. Give it these 2 things from your own account:

| Field | What it is | Where to find it |
|---|---|---|
| **User ID** | Your account number, digits only | Open your profile on the site. It is the number in the page's address |
| **Passkey** | The key the site puts in your download links | On your profile page on the site |
| **Cookie** | `uid=...; pass=...`, or the whole cookie line | Your browser's cookies for the site, after you sign in. Open the developer tools, find a request to the site and copy its cookie |
| **User agent** | `Mozilla/5.0 ...` | The user agent of the browser the cookie came from, copied from the same request |

HolyArrs keeps the passkey encrypted. The passkey does not run out, so you set this up once. If you reset it on the site, type the new one here.

Treat the passkey like a password. Anyone who has it can download as you.

With the cookie, fill in **User agent** too: the site turns the cookie down from any other browser. **Freeleech only** lists only Releases that do not count against your ratio; it works with the cookie, as the site's RSS does not mark them. HolyArrs fetches a Release file with the cookie and hands the file to the Downloader, which has no cookie.

A Source you added earlier with a cookie keeps working. To switch it, open it, fill in **User ID** and type your passkey into **Passkey**.

## If it does not work

| You see | Try |
|---|---|
| IPTorrents says it needs your user ID and your passkey | Fill in both **User ID** and **Passkey** |
| IPTorrents refused the user ID or the passkey | Check both against your profile page. The user ID is digits only |
| IPTorrents turned down the cookie | Sign in to IPTorrents in your browser again, then paste the new cookie and that browser's user agent |
| IPTorrents asks to set the category column | In your IPTorrents settings, set **Torrents - Category column** to **Icons**, wait a minute, then test again |
| TorrentsCSV never grabs new episodes on its own | Expected. It has no newest list. Searches still work |
| The site answers with something odd | The site may be down or have moved. Try **Test** later, or fix the **Address** |
| Nothing is found for music on SubsPlease | Expected. SubsPlease only has shows |

<details class="ha-more">
<summary>For experts</summary>

Each site starts with polite limits: one call at a time with a gap, and a cap per hour. Knaben, TorrentsCSV and SubsPlease wait 2 seconds between calls and allow 100 searches an hour. IPTorrents waits 3 seconds and allows 60 an hour. Change them under the Source's limits like any other Source, but HolyArrs always waits at least 2 seconds between two calls to a site, as Prowlarr does.

Searches use the title plus `S01E02`, `S01` or the air date (`2024.01.31`) for shows, the year for movies, and artist and album for music, cleaned of odd characters as Prowlarr cleans them. Categories asked for are turned into each site's own, as Prowlarr maps them.

</details>

## Related

- [Sources](sources.md)
- [Downloaders](downloaders.md)
