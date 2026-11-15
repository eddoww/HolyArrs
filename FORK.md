# eddoww/HolyArrs fork

`homelab` = upstream `main` + our feature branches (`feat/*`, `fix/*`). CI on `homelab`
(".github/workflows/homelab-image.yml") tests and publishes `ghcr.io/eddoww/holyarrs`,
which the homelab cluster deploys (k8s/holyarrs in eddoww/homelab).

## What the fork adds on top of upstream (as of upstream 1.1.4)

Where upstream has a feature, we use upstream's and only keep the fork's
differences. These are the differences.

- **Downloads**: a new torrent Downloader starts with "Show only HolyArrs items" on
  (`own_only`). The snapshot fallback list survives a client panic (`recover()` in
  `poll.go`). A clearer refusal when a cookie-signed Release cannot be fetched.
- **Built-in sites**: IPTorrents is read by the fork's engine (`sources/sites_fork.go`,
  Prowlarr's way: list pages, IMDb search, `S01*` seasons, freeleech marks, sign-in page
  detection). Each site gets the user agent and freeleech choice from the Source's
  `extra` (`user_agent`, `freeleech_only`), a 2 s floor between calls, Prowlarr's search
  word cleaning (`siteWords`, `episodeWords`) and category mapping (`siteCategories`).
  A raw cookie without `pass` is accepted.
- **Traits and Pick rules from Sonarr/Radarr**: imported Pick rules judge as the old
  apps (`mediafiles/judge_fork.go`): the step from `ReadOldAppQuality`, name tests as
  Radarr reads names, old-app language settling, refused cross-group repacks, upgrades
  off. Language reading in names (`releasename/languages.go`): NORDiC, NOR, swe/cze/heb,
  dub words, group suffix skipped. Parity test: `migrate/arr_parity_test.go`.
- **Migrate**: recorded files keep the old app's recorded quality
  (`SetRecordedQualities`), Known files, EpisodesOff, per-title Pick rules, 4K folders
  marked as 4K copies (`fourKVersions`), Seerr's 4K servers written to
  `ask_four_k_*`, folder Pick rules settled after the rules group, kept rules taken back
  after an undo, download handling (free space, side files, feed interval), Prowlarr
  site Sources with user agent and freeleech.
- **Requests**: "Request 4K" for a show already in the Library (`requests/ask_fourk.go`).
- **Subtitles**: BSPlayer hash check, `nb`/`nn` sent as `no`, YIFY under upstream's
  `yifysubtitles`, guessed-language label, extra Bazarr settings on the subtitle page.
- **Importer**: a vague file name is matched by the Release HolyArrs sent
  (`importer/release_fork.go`).

## Database: carrying a fork database onto upstream

Our migrations live at 9001+. Upstream's numbers never collide, but a fork database is
already at 9001-9004 when upstream's newer migrations arrive, and the runner applies
every unrecorded migration in number order. So:

- `9001_download_handling.sql` and `9002_site_sources.sql` are now no-ops (`SELECT 1;`);
  their old SQL is in `core/database/testdata/homelab_1019/` for the upgrade tests.
- `core/database/fork_hooks.go` wraps upstream migrations on a fork database only
  (9001 or 9002 recorded under the fork's names). The SQL is in
  `core/database/migrations/fork/`:
  - before 0109: set the fork's Downloader columns aside (`fork_downloaders_kept`);
  - after 0118: restore them into `category_after_import` and `own_only`; copy
    `downloads.imported_at` into `category_moved_at`; a qBittorrent seed goal becomes
    `after_import='remove'` with the goal clamped; `import_extra_files` and its types
    become upstream's side files settings;
  - before 0121: the fork's site Sources get `format='site'` (rewritten in 0121's copy)
    with site, user agent and freeleech in `extra`, and the site's polite limits.
- `9005_fork_settings_to_upstream.sql`: `ask_4k_*` settings to `ask_four_k_*` (and their
  undo records), `trait` undo marks to `own_trait`, `'4K'`-labelled main versions get
  `four_k`, subtitle site `yify` to `yifysubtitles`. A no-op on a fresh database.
- `mediafiles/store_fork_upgrade.go` (`UpgradeForkRules`, run once at start from
  `library.StartOwnTraits`, marked by the `this_app` setting `fork_rules_upgraded`):
  conditions traits become imported traits with specs, and `like_old_apps` Pick rules
  become upstream's imported Pick rules (steps mapped onto the ladder). Go, not SQL,
  because the bodies are JSON with checks.
- Tests: `TestForkDatabaseUpgradesFromHomelab` (incl. the "1.0.19 live" case with every
  live value, `fork_live_test.go`) and `TestUpgradeForkRulesFromLive`.

## Syncing with upstream (keep it short)

Target: under an hour of wall time, no multi-hour local test runs.

1. `git fetch upstream && git merge upstream/main` on a `sync/<version>` branch.
2. Resolve conflicts. Upstream wins where both did the same thing; keep ours on top.
   - Check whether any new upstream migration rebuilds a table the fork hooks touch
     (downloaders, downloads, sources, own_traits, pick_rules, settings), or changes the
     `SELECT` 0121 copies with. If so, extend `fork_hooks.go` and
     `TestForkDatabaseUpgradesFromHomelab`.
   - If upstream changes `PickRulesBody` or `OwnSpec`, rerun `TestUpgradeForkRulesFromLive`
     and the parity test.
3. Local checks only (niced, GOMAXPROCS=2, `-p 2`):
   - `go build ./... && go vet ./...`
   - `go test -tags structure ./app/backend/tools/structurecheck` (`-update` after a
     deliberate change to keys, headers or migrations)
   - `go test ./app/backend/internal/core/database/... ./app/backend/internal/migrate/...`
     plus the packages that had conflicts. `cmd/holyarrs` needs the built web app
     (`npm --prefix app/frontend run build`).
   - `npm --prefix app/frontend run check`, and vitest on the touched files
4. Push and merge into `homelab`; CI runs the full Go and frontend suites remotely.
5. No full Playwright run locally. If pages changed in conflicts, run only those
   projects (`npm --prefix app/frontend run e2e -- --project=<name>`), or skip.
6. Back up the live config (Settings > Backups > Back up now, download), deploy, and
   check the start-up log and a few API reads (titles, Sources test, Downloader test).
