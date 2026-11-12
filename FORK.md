# eddoww/HolyArrs fork

`homelab` = upstream `main` + our feature branches (`feat/*`, `fix/*`). CI on `homelab`
(".github/workflows/homelab-image.yml") tests and publishes `ghcr.io/eddoww/holyarrs`,
which the homelab cluster deploys (k8s/holyarrs in eddoww/homelab).

## Syncing with upstream (keep it short)

Target: under an hour of wall time, no multi-hour local test runs.

1. `git fetch upstream && git merge upstream/main` on a `sync/<version>` branch.
2. Resolve conflicts. Upstream wins where both did the same thing; keep ours on top.
   - Our migrations live at 9001+; upstream's numbers never collide. Check whether any
     new upstream migration rebuilds a table we added columns to (downloaders, downloads,
     sources, own_traits, subtitles). If so, extend the set-aside in the fork renumber
     code and `TestForkDatabaseUpgradesFromHomelab`.
3. Local checks only (niced, GOMAXPROCS=2):
   - `go build ./... && go vet ./...`
   - `go test -tags structure ./app/backend/tools/structurecheck`
   - `go test ./app/backend/internal/core/database/... ./app/backend/internal/migrate/...`
     plus the packages that had conflicts
   - `npm --prefix app/frontend run check`
4. Push and merge into `homelab`; CI runs the full Go and frontend suites remotely.
5. No full Playwright run locally. If pages changed in conflicts, run only those
   projects (`npm --prefix app/frontend run e2e -- --project=<name>`), or skip.
6. Back up the live config (Settings > Backups > Back up now, download), deploy, and
   check the start-up log and a few API reads (titles, Sources test, Downloader test).
