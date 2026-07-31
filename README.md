# Village Report

A dashboard for a Clash of Clans village export. It answers four questions:

- **What lands next?** Every upgrade in flight, on a live 48-hour timeline, with
  builder and laboratory capacity.
- **Where is the work?** Completion per category, measured against what your
  Town Hall can actually reach — not the game-wide maximum you cannot build yet.
- **What will it cost to finish?** Gold, elixir, dark elixir, ore and builder/lab
  time left to hit every current ceiling, plus a ranked list of what's startable
  right now and what's dragging your account down.
- **What changed?** A change log between two exports of the same village —
  what landed, what started, what got cleared — once you turn on history.

Don't want to re-export just to see something you started reflected? Click
**Build now** on anything in "What to upgrade next" and it counts toward
every number above immediately - the next real export you drop confirms it
(or, if it turns out you never actually started it, quietly retires it).
Nothing here is ever treated as ground truth the way an export is.

Two ways to run it:

- **Local** — a single Go binary, no accounts, nothing written to disk unless
  you ask. Drop an export on the page; that's the whole setup. Holds
  several villages at once and lets you switch between them.
- **Hosted** — sign in with GitHub or Google, exports persist to Postgres, and
  you get a rolling two weeks of history from any device. Meant for a small
  Vercel deployment, not a laptop.

## Run it locally

```bash
go build .
./coc-progress -snapshot village.json
```

Open <http://localhost:8080>. You can also start it with no arguments and drop
the export onto the page.

The frontend is already built into `web/dist` and embedded in the binary. To
change it:

```bash
cd web
npm install
npm run build       # writes web/dist, which the Go binary embeds
go build ..         # rebuild to pick it up
```

For frontend work with hot reload, run the Go server on `:8080` and
`npm run dev` in `web/` — Vite proxies `/api` across.

### Flags

| Flag        | Default   | Purpose                                                          |
| ----------- | --------- | ----------------------------------------------------------------- |
| `-addr`     | `:8080`, or `:$PORT` if set | Listen address                                     |
| `-snapshot` | –         | Export(s) to load at startup, comma-separated for more than one village |
| `-catalog`  | built-in  | Use a `catalog.json` from disk instead                            |
| `-history`  | off       | Directory to keep past exports in, so the History tab has something to diff. Nothing is written to disk without this - villages are still held in memory (and switchable) without it, just not across a restart. |

### API

| Endpoint         | Method | Purpose                                          |
| ---------------- | ------ | ------------------------------------------------- |
| `/api/report`    | GET    | The current analysis for a village (`?tag=`, defaults to the most recently captured) |
| `/api/report`    | POST   | Send an export body, get the analysis back        |
| `/api/villages`  | GET    | Every village currently held, newest capture first, for a switcher |
| `/api/villages`  | DELETE | Forget a village entirely (`?tag=`) - local stores only, not Postgres |
| `/api/pending`   | POST   | Declare an upgrade started by hand ("build now"), without a fresh export (`?tag=`) |
| `/api/pending`   | DELETE | Cancel a declared upgrade (`?tag=&id=`) |
| `/api/history`   | GET    | Change log between the two most recent snapshots (`?tag=`) |
| `/api/catalog`   | GET    | The ID lookup table                               |
| `/api/features`  | GET    | Which gated capabilities (`themes`, `build_now`) the caller currently has - always both, locally |
| `/healthz`       | GET    | Liveness                                          |

```bash
curl -X POST --data-binary @village.json localhost:8080/api/report
```

## Themes

Six of them, in a picker next to the header: Paper (the original light theme),
Night, Elixir, Dark Elixir, Gold, and Builder Base. The choice is saved in the
browser and otherwise follows your OS's light/dark setting. Every theme keeps
the one rule that actually matters here: colour means *still to do*, plain ink
means *finished*.

In local mode the picker is always there. In hosted mode, showing it (and
the Build Now button) depends on the signed-in account's role - see below.

## Running it hosted (Vercel + Postgres + accounts)

Setting `DATABASE_URL` switches the same binary into hosted mode: sign-in via
GitHub and/or Google, every accepted export persists to Postgres under your
account, and a daily job prunes snapshots older than 14 days (always keeping
each village's newest, so a village you haven't exported in a month still
shows its last known state instead of nothing).

**This trades away the local promise.** In local mode nothing leaves your
machine. In hosted mode your exports are uploaded to a database you don't
control the hosting of, and icons load from a third-party CDN either way.
Decide which mode fits before you point it at a real village.

### Deploying

1. Provision a Postgres database and copy its connection string. Use a
   **transaction-pooling** endpoint (Neon's `-pooler` host, or PgBouncer in
   transaction mode) — a serverless function opens a fresh connection pool
   per instance, and a session-pooling or direct connection runs out of slots
   fast. The schema (`internal/store/postgres/schema.sql`) applies itself on
   first connect.
2. Register an OAuth app with GitHub and/or Google. The callback URL is
   `{BASE_URL}/api/auth/github/callback` or `.../google/callback`.
3. Copy `.env.example` to `.env.local` (or set the same names in the Vercel
   dashboard) and fill in `DATABASE_URL`, `BASE_URL`, the OAuth credentials,
   and a random `CRON_SECRET` (`openssl rand -base64 32`).
4. `vercel deploy`. `vercel.json` builds the frontend and the Go binary in one
   step (the [Go framework preset](https://vercel.com/docs/functions/runtimes/go)
   runs a plain `net/http` server on `$PORT`, so local and hosted mode are the
   same binary) and registers the daily prune cron.

### Running hosted mode locally, for development

`docker-compose.yml` starts a throwaway Postgres on `localhost:15432` -
`docker compose up -d`. Its port and credentials match `TEST_DATABASE_URL`'s
own convention below, so the same container backs both the Postgres-backed
test suite and running the real binary in hosted mode. Copy `.env.example`
to `.env.local`, point `DATABASE_URL` at that container, register an OAuth
app with a `localhost` callback URL, and set `ADMIN_EMAIL` to whichever
account you'll sign in with - see the env var table below for all of it.

### Env vars

| Variable                | Required | Purpose                                              |
| ------------------------ | -------- | ----------------------------------------------------- |
| `DATABASE_URL`            | to enable hosted mode | Postgres connection string (pooled endpoint) |
| `BASE_URL`                | in hosted mode | This deployment's own origin, no trailing slash — used for OAuth redirects and CORS |
| `GITHUB_CLIENT_ID` / `_SECRET` | one of GitHub or Google | GitHub OAuth app credentials |
| `GOOGLE_CLIENT_ID` / `_SECRET` | one of GitHub or Google | Google OAuth app credentials |
| `CRON_SECRET`             | in hosted mode | Bearer token the daily prune cron presents to `/api/cron/prune` |
| `ADMIN_EMAIL`             | to ever have an admin | Email promoted to the `admin` role on sign-in - see "Roles and feature flags" |
| `PORT`                    | set by Vercel | Overrides `-addr`'s default listen port |

### Roles and feature flags

Hosted mode has two roles: `user` (everyone, by default) and `admin`. The
only way to become admin is for `ADMIN_EMAIL` to match the email your OAuth
provider hands back at sign-in - there is no other bootstrap. From the admin
board (a new tab in the frontend once you're signed in as one) an admin can
promote or demote anyone else.

Two capabilities are gated by role rather than being open to every account:
themes and Build Now. Both default to admin-only (`internal/store/postgres/schema.sql`'s
`feature_flags` table), so a freshly signed-up account gets the core
tracker - the timeline, completion, cost-to-finish, history - and nothing
else until an admin promotes it. `GET /api/features` reports which of the
two the caller currently has; the frontend hides the theme picker and the
Build Now button rather than showing either as merely disabled.

**None of this touches local mode.** Local mode has no accounts to hold a
role, so every gate in `internal/feature` treats "no accounts" the same as
"fully unlocked" - themes and Build Now work exactly as they always have.

### Hosted-mode API additions

| Endpoint                    | Method | Purpose                                        |
| ---------------------------- | ------ | ------------------------------------------------ |
| `/api/config`                 | GET    | Which sign-in providers are configured           |
| `/api/me`                     | GET    | The signed-in user (now including `role`), or `{"user":null}` |
| `/api/admin/users`            | GET    | List every user, their role, and their village count (admin only) |
| `/api/admin/users`            | POST   | Change one user's role - `{"userId":…,"role":"admin"\|"user"}` (admin only) |
| `/api/auth/github`, `/google` | GET    | Start that provider's OAuth flow                 |
| `/api/auth/{provider}/callback` | GET  | OAuth redirect target                            |
| `/api/auth/logout`            | POST   | Revoke the current session                       |
| `/api/cron/prune`             | GET    | Runs the retention prune; needs `Authorization: Bearer $CRON_SECRET` |

`/api/report` and `/api/history` require a session cookie in hosted mode.
Open signup means everyone gets the same guards: 5 villages per account, 100
snapshots per village, 40 uploads per day, and a 512 KB body cap (a real
export is a few kilobytes).

The frontend's village switcher (backed by `/api/villages` and `?tag=`) is
mode-agnostic and works against a signed-in session the same way it does
locally. What hosted mode is still missing is a sign-in screen in the
frontend itself - today a session cookie has to come from hitting
`/api/auth/github` or `/api/auth/google` directly, with no button in the UI
for it yet.

## How the numbers are worked out

An export contains no names — only numeric IDs, levels and counts:

```json
{ "data": 1000008, "lvl": 11, "cnt": 4 }
```

`data` is an ID, `lvl` is the current level, `cnt` is how many copies sit at that
level. A row with a `timer` is mid-upgrade and its `lvl` is the level it is
*leaving*. A row with neither `cnt` nor `timer` is a single copy — which matters,
because a Cannon being geared up and a Cannon being upgraded each get their own
row, and missing them undercounts the base.

`data/catalog.json` maps those IDs to names, categories, per-level cost and
build time, and icon paths. It is generated from
[ClashKing's](https://github.com/ClashKingInc/ClashKingAssets) extracted game
data (`static_data.json`), which carries an explicit numeric ID per entity —
no row-order guessing required — plus `manifest.json` for the matching icon
assets served from `assets.clashk.ing`. Both data and icons come from that
project; if you use this dashboard's data pipeline elsewhere, credit them too.

**Ceilings are per Town Hall (or whichever gate actually applies), not
global.** Most entries are gated on the Town Hall or Builder Hall, but a hero
is also gated by the Hero Hall, a troop by the Laboratory, hero equipment by
both the Town Hall and the Blacksmith, and a pet by the Pet House — each
independently, all of them required at once. At Town Hall 10 a Cannon caps at
13, not the game-wide 21; a Hero Hall 1 caps a home hero at level 1 no matter
how high the Town Hall runs ahead of it. "At ceiling" means *finished until
you upgrade whatever's actually gating it*, which is the number worth acting
on — not a raw distance from the game-wide max.

Levels are 1-indexed. A level of `0` alongside a timer means a first-time build.

### Refreshing the catalog

```bash
./scripts/fetch-gamedata.sh
go build .
```

Do this after a game update. Until you do, anything new is still counted and
timed, but shown as `Item #1000093` and left out of the percentages rather
than measured against a ceiling that would be a guess. The dashboard says so
in its footnotes.

## Layout

```
main.go                     Entry point: picks local or hosted mode, embeds catalog + frontend
internal/snapshot/          Export parsing
internal/catalog/           ID lookup, per-hall ceilings, cost/time, icon URLs
internal/analyze/           Completion, the bill, next-up suggestions, missing buildings,
                             next-hall preview, strength, balance, and the change-log diff (+ tests)
internal/server/            The API, mode-aware (local vs hosted), quotas, CORS, admin board (+ tests)
internal/auth/               GitHub/Google OAuth and session cookies, hosted mode only (+ tests)
internal/feature/            Role-gated feature flags (themes, build_now), hosted mode only
internal/store/              Snapshot persistence: memory, file (-history), Postgres, all + tests
cmd/catalogen/               Builds catalog.json from ClashKing's static_data.json + manifest.json (+ tests)
scripts/fetch-gamedata.sh    Downloads that data, regenerates the catalog
web/                         React frontend (Vite) — Now / Plan / Progress / History tabs (+ Admin for
                             admins), six themes; web/src/features/{core,themes,build-now,admin}/
data/catalog.json            Generated lookup table
vercel.json, .env.example    Hosted deployment config
```

```bash
go test ./...
```

The Postgres-backed tests (`internal/store/postgres`, `internal/server`'s
hosted-mode tests) are skipped unless `TEST_DATABASE_URL` is set. They share
one throwaway database by convention, so running them together via
`go test ./...` needs `-p 1` — Go parallelizes across packages by default,
and two packages truncating the same database at once will strand each other
mid-test.

## Notes

- **Local mode**: villages are held in memory only (up to 20 snapshots per
  village; oldest trimmed first) and nothing is written to disk unless
  `-history` is set — then only export JSON, to the directory you named,
  and durably rather than capped.
- **Hosted mode**: your exports are stored in the Postgres database you
  configured, scoped to your account, pruned after 14 days.
- Building icons load from `assets.clashk.ing` by default in both modes;
  `index.html`'s Google Fonts link is the other external request either mode
  makes.
- Countdowns are computed from absolute finish times, so an old export shows
  what has already landed instead of a frozen timer.
- Unofficial and unaffiliated with Supercell.
