---
title: Pick rules and traits
description: How Pick rules judge a Release, how to make your own traits, and how traits and Pick rules moved from Sonarr and Radarr judge.
level: Setup
sidebar: { order: 5 }
---

Decide which Releases HolyArrs takes for a title, and which one wins.

## Before you start

- Open **Settings > Quality**. Each set there is a set of **Pick rules**. Standard, Lite, Best and 4K come ready-made.
- Changing Pick rules needs the Settings permission.

## What Pick rules check

1. **Steps.** The quality steps a Release may be on, and **Good enough**: the step where HolyArrs stops looking for a better copy.
2. **Traits and points.** Each trait a Release has adds or takes points. On the same step, the most points wins.
3. **Must have and Never.** A Release without a must-have trait, or with a Never trait, is turned away.
4. **Size, waits and language.**

**Choose a release** shows, for each Release, why it would be picked or not, its points and the traits that gave or took them.

## Make your own trait

1. Under **Your traits**, click **Make a trait**.
2. Pick what it looks at: words in the name, the release group, or the size.
3. Click **Make trait**. You should see it in the list, and in the trait lists of every set of Pick rules.
4. Open a set of Pick rules, click **Add a trait**, pick yours and give it points.

## Traits from Sonarr and Radarr

Custom formats moved from Sonarr or Radarr are listed under **Your traits**, in **From old apps**, with the app and their conditions. They test what the custom format tested: the name, the release group, the edition, the source, the kind of copy, the resolution, the audio language and the size. You can remove one, but not change it. Move the app again to update them.

- Conditions of the same kind pass when any one of them passes. Every kind must pass. A **Required** condition must pass for its kind to pass. **Not** turns one condition's answer around first.
- Patterns are read the way Sonarr and Radarr read them: case is ignored, and look-aheads such as `\bHDR\b(?!ip)` work.
- A name pattern tests the name as Radarr does. For a movie, the movie's own title is left out, so a tag pattern never matches a word of the title.
- For a file already in the Library, the audio languages its inspection read count instead of its name's, when it read any, as Radarr and Sonarr do.

## Languages in names

HolyArrs reads the spoken languages a Release name gives after its title, such as `NORWEGiAN`, `Norsk`, `NOR`, `SWEDiSH`, `ENG-ITA`, `EN/FR/ES` or `LATINO`. `NORDiC` means the Nordic dubs (Danish, Finnish, Norwegian, Swedish) and the original language. A name with no language counts as the original language. Bokmål and Nynorsk count as Norwegian.

:::note[Unlike Sonarr and Radarr]
Sonarr and Radarr read no language from `NORDiC` or `NOR`. A trait on Norwegian audio, such as one moved from their custom formats, matches a `NORDiC` release here and not there. With a Nordic Multi-Audio profile that gives each of Nordic multi release, Norwegian title tag and Norwegian audio 10000 points, a `NORDiC` 1080p release gets 20900 points here and 10900 in Radarr, so it ranks just above a `NORWEGiAN` one (20800 in both) instead of well below it. Whether a release is taken does not change.
:::

## Pick rules from Sonarr and Radarr

Pick rules made from a Sonarr or Radarr quality profile show the profile's name and app at the top of their page. They judge as that app does:

- A name is put on a step by the quality the app reads from it. A name with only a resolution is a TV copy, and `BDRip` without a resolution is SD Blu-ray. A full Blu-ray disc has no step here.
- The steps are HolyArrs's steps, each holding the app's qualities that fit it. `HDTV-2160p` and `WEBDL-2160p` share one step.
- A better copy replaces the file as in those apps. A higher step replaces it while the file is below Good enough. On the same step the points must rise by **Points a better copy must add**, while the file is below **Good enough points**.
- A profile with upgrades off keeps the file once it is there.
- A repack (`REPACK`, `RERIP`) of the file's own quality replaces it only when it comes from the group that made the file. A repack from another group is refused, and so is one when the file's group or the repack's group is not known. A `PROPER` from any group counts.

The ready-made Pick rules keep HolyArrs's own steps and reading.

## If it does not work

| Symptom | Cause | Fix |
|---|---|---|
| A Release you want is turned away for its language | The language rule wants the original, and the name gives another language | Allow any language, or add the language as wanted |
| A trait from an old app never matches | A Required condition fails, or one kind has no passing condition | Open **Choose a release**. The trait's line says what it saw |
| A repack is not taken | It comes from another group than the file, under Pick rules from Sonarr or Radarr | Expected, as in those apps |

## Related

- [Sonarr and Radarr](../migrate/sonarr-radarr.md)
- [Data packs](../library/data-packs.md)
