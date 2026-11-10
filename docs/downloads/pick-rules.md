---
title: Pick rules and traits
description: How Pick rules judge a Release, how to make your own traits from words, release groups or conditions, and how rules moved from Sonarr and Radarr judge.
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
2. Pick what it looks at:

   | Look at | Matches when |
   |---|---|
   | Words in the name | The Release name has one of the words or `/patterns/` |
   | Release group | The group (after the last dash) is one of the words or fits a pattern |
   | Conditions | Its conditions pass, as in a Sonarr or Radarr custom format |

3. Click **Make trait**. You should see it in the list, and in the trait lists of every set of Pick rules.
4. Open a set of Pick rules, click **Add a trait**, pick yours and give it points.

### Conditions

| Condition | Passes when |
|---|---|
| Release name fits a pattern | The pattern matches the name. For a movie, the movie's own title is left out, so a tag pattern never matches a word of the title |
| Release group fits a pattern | The pattern matches the release group |
| Edition fits a pattern | The pattern matches the edition (Director's Cut, IMAX) |
| Resolution | The resolution is this one |
| Source | The source is this one (Blu-ray, web, TV, DVD) |
| Kind of copy | The copy is a remux, a full disc, raw HD, a screener or a regional release |
| Audio language | The Release has this language. **The original language** is the title's own. **Any other language** passes when a language other than this one is there |
| Size | The Release is over the smallest and up to the largest size |

How they add up:

- Conditions of the same kind pass when any one of them passes.
- Every kind must pass.
- A **Required** condition must pass for its kind to pass.
- **Not** turns one condition's answer around first.

Patterns are read the way Sonarr and Radarr read them: case is ignored, and look-aheads such as `\bHDR\b(?!ip)` work. A pattern HolyArrs cannot read is refused when you save, with what is wrong. A pattern that takes more than a tenth of a second on one name stops and does not match.

## Languages in names

HolyArrs reads the spoken languages a Release name gives after its title, such as `NORWEGiAN`, `Norsk`, `NOR`, `SWEDiSH`, `ENG-ITA`, `EN/FR/ES` or `LATINO`. `NORDiC` means the Nordic dubs (Danish, Finnish, Norwegian, Swedish) and the original language. A name with no language counts as the original language.

| Language rule | A Release passes when |
|---|---|
| A wanted language, or the original | It is in a wanted language, or in the original when **The original language is fine** is on |
| Any language | Always |
| Only the original language | It has the title's original language |
| Only the wanted languages | It has one of the wanted languages |

## Judge like Sonarr and Radarr

Pick rules moved from Sonarr or Radarr have **Judge like Sonarr and Radarr** on. Then:

- The steps are those apps' qualities, such as 4K TV, Full Blu-ray disc and SD web, in the order the profile listed them.
- A name is read the way they read it. A name with only a resolution is a TV copy, and `BDRip` without a resolution is SD Blu-ray.
- A better copy replaces the file as in those apps. A higher step replaces it while the file is below Good enough. On the same step the points must rise by **Points a better copy must add**, while the file is below **Points needed for good enough**.
- **Look for better copies** off keeps the file once it is there. Only a fixed copy replaces it.

The ready-made Pick rules keep HolyArrs's own steps and reading.

## If it does not work

| Symptom | Cause | Fix |
|---|---|---|
| Saving a trait says the pattern cannot be read | The pattern uses something neither HolyArrs nor the old apps read | Fix the pattern the message points at |
| A Release you want is turned away for its language | The language rule wants the original, and the name gives another language | Pick **Any language**, or add the language as wanted |
| A trait never matches | A Required condition fails, or one kind has no passing condition | Open **Choose a release**. The trait's line says which conditions passed |

## Related

- [Sonarr and Radarr](../migrate/sonarr-radarr.md)
- [Data packs](../library/data-packs.md)
