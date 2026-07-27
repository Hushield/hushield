# HuShield — Marketing Site & Contributor Funnel

**Date:** 2026-07-27
**Status:** Approved (design); implementation plan pending
**Domain:** `hushield.com` (registered 2026-07-27, Route 53, hosted zone `Z04195413MZU1NAXAI0FO`)
**Host:** `pm-prod-spamfilter` — `34.229.7.177` / `10.30.1.244`, `t4g.large`, `i-0978733a749326f25`

## 1. Purpose

A public marketing site for HuShield (the product previously called SpamFilter) whose primary
conversion goal is **recruiting outside developers**, not acquiring app users. The product is free
and will stay free; there is no billing, no paid tier, and no revenue mechanic anywhere in scope.

The site must answer three questions within one screen of scrolling:

1. What does HuShield do, and why is it different? (community-scored blocking, zero PII)
2. What is built, what is being built, and what is unclaimed?
3. How do I contribute, and will my contribution actually get merged?

Question 3 is the one most sites fail. The design treats "proof that outsiders get merged" as a
first-class feature rather than a footer link.

## 2. Locked decisions

| Decision | Value | Rationale |
|---|---|---|
| Repo home | New dedicated public GitHub org | Keeps the project's identity separate from `reach-x`, whose 363 private lead-gen repos read as "corporate side project" to a prospective contributor |
| License | Apache-2.0 + `TRADEMARK.md` | Patent grant and corporate-policy friendliness of Apache-2.0; the trademark policy reserves the HuShield name and App Store identity so forks must rebrand |
| Data fetching | Go service with a background poller and cached snapshots | Only approach that is genuinely live, keeps the token server-side, and makes visitor traffic irrelevant to rate limits |
| Leaderboard rank key | **Merged PRs** (primary), then issues closed, reviews, commits | Ranks the behavior actually wanted; lines-changed is displayed as a per-contributor stat but never as the sort key |
| iOS bundle ID | `com.brahy.hushield` | Matches the real product name; changing it now is nearly free and painful after TestFlight/App Store release |

## 3. Non-goals

- No user accounts, login, newsletter, or analytics that fingerprints visitors. A site whose product
  claim is "no PII" must not itself harvest visitors; this is a positioning constraint, not just taste.
- No payment, pricing, or upsell surface of any kind.
- No CMS. Content lives in the repo and ships via deploy.
- No client-side GitHub API calls (rate limits are per visitor IP; a token cannot be hidden in JS).

## 4. Repository layout

Three repos in the new org:

| Repo | Contents | Visibility |
|---|---|---|
| `hushield` | The product: Go backend + iOS app. Apache-2.0 + `TRADEMARK.md`. | Public |
| `website` | This marketing site. | Public |
| `.github` | Org-level profile README, shared issue templates. | Public |

The website is a separate repo so that website commits do not dilute the product repo's contributor
graph and leaderboard, and so the site deploys on its own cadence. Accepted cost: a contributor
fixing site copy must find a second repo; mitigated by a visible "edit this page" link on every page.

## 5. Site architecture

```
cmd/web/                    # entry point
internal/
  config/                   # env loading; hard-fail on missing GITHUB_TOKEN in prod
  github/                   # API client, typed models, ETag handling
  cache/                    # snapshot store + background refresh loop
  catalog/                  # the feature system (structured data, repo-local)
  handlers/                 # HTTP handlers, one per page
  views/                    # view models (no GitHub types leak into templates)
  templates/                # html/template, precompiled at startup
web/static/                 # CSS, minimal progressive-enhancement JS
```

Stack mirrors the existing backend: Go, `net/http` (standard library routing — five static routes do
not justify a router dependency), `html/template`, embedded static assets. No database — all state is either repo-local content or a
GitHub snapshot held in memory. Restart cost is one poll cycle.

### 5.1 The poller / snapshot boundary

**Handlers never call GitHub.** A background goroutine per dataset refreshes on its own interval and
publishes an immutable snapshot via `atomic.Pointer[Snapshot]`. Handlers do a single atomic load.

This yields three properties that the design depends on:

1. **Page loads never block on GitHub.** Latency is template rendering only.
2. **Visitor traffic cannot consume rate limit.** Polling cost is fixed regardless of traffic.
3. **Graceful degradation is the default.** If a refresh fails, the previous snapshot stays live and
   the page renders a `data as of HH:MM` stamp.

### 5.2 Failure rendering (explicit requirement)

A failed fetch and a genuinely empty result **must not render identically**. Every dataset carries a
tri-state: `Fresh`, `Stale(since)`, `Unavailable`. An empty leaderboard because nobody has
contributed renders as an invitation; an empty leaderboard because GitHub 500'd renders as a stale
notice with the last known data.

This requirement exists because of a real defect found in `brightbridgelabs.com` on 2026-07-27: its
WHOIS layer rendered failed lookups as a neutral "Unknown", making a broken integration
indistinguishable from a working one and putting affiliate buy links next to unverified domains. Do
not repeat that mistake here.

### 5.3 Data sources

| Dataset | Endpoint | Interval | Notes |
|---|---|---|---|
| Changelog | `GET /repos/{o}/{r}/releases` | 5 min | Falls back to annotated tags until the first release exists |
| Issues | `GET /repos/{o}/{r}/issues?state=open` | 2 min | Partitioned by label into bugs / good-first-issue / help-wanted |
| Pull requests | `GET /repos/{o}/{r}/pulls?state=all` | 2 min | Open + recently merged; drives the "outsiders get merged" proof |
| Contributors | `GET /repos/{o}/{r}/contributors` | 30 min | Identity, avatar, commit count |
| Line stats | `GET /repos/{o}/{r}/stats/contributors` | 30 min | Returns **202** while GitHub computes; poller must retry, not treat as empty |
| Repo meta | `GET /repos/{o}/{r}` | 5 min | Stars, forks, open issue count |

Auth: fine-grained PAT, **public-repo read-only**, supplied as `GITHUB_TOKEN`. Budget is 5,000
req/hr against a polling cost of roughly 40 req/hr. All requests send `If-None-Match`; a `304`
costs nothing against the limit.

## 6. Pages

### `/` — Home
Hero stating the product and the privacy claim. Live stats strip (stars, contributors, merged PRs,
numbers in the blocklist). Dual CTA: *Get the app* and *Contribute* given equal visual weight —
the contributor path is a primary conversion, not a secondary one.

### `/features` — The feature system
The full catalog, every entry tagged `shipped` | `in-progress` | `planned` | `help-wanted`.
`help-wanted` entries deep-link to their real GitHub issue. This page does double duty: it is the
product feature list *and* the recruiting surface, because it shows the roadmap and the gaps in a
single view.

Catalog entries are repo-local structured data (not GitHub-derived) so the roadmap can describe work
that has no issue yet:

```yaml
- id: sms-filter-extension
  title: Offline SMS filtering
  status: shipped
  area: ios
  summary: Classifies incoming messages with no network access.
  requirement: REQ-09          # traceability to .planning/PROJECT.md
  issue: null                  # or an issue number when help-wanted
```

Initial catalog derives from `REQ-01`–`REQ-12` in `.planning/PROJECT.md`, reconciled against what is
actually implemented (Phases 1–2 complete; 3–4 substantially complete, device- and deploy-gated).

### `/changelog` — Live changelog
GitHub Releases, newest first, release notes rendered from markdown. Until `v0.1.0` exists it falls
back to annotated tags, and if neither exists it renders an honest "no releases yet" state rather
than an empty page.

### `/contribute` — The funnel
Live `good-first-issue` list with claim status. Dev setup derived from the existing `Makefile`
(`make test` = Go + iOS). Architecture orientation map. Link to `CONTRIBUTING.md`. Explicit
statement of review turnaround expectations.

### `/contributors` — People and leaderboard
Grid of every contributor. Leaderboard ranked by **merged PRs**, then issues closed, then reviews,
then commits; lines added/removed shown per card as a stat. A prominent
"first-time contributors this month" row — the single strongest signal that an outsider's PR will
actually land. The rank key is config-driven so it can be changed without a code edit.

## 7. Prerequisites (must precede the site build)

1. **Create the GitHub org** — manual, in the browser. The available token has `read:org` only, not
   `admin:org`, so org creation cannot be scripted.
2. **Publish the product repo.** A secret audit was completed 2026-07-27: `.env` and
   `credentials.md` are gitignored and were never tracked, no AWS/OpenAI/GitHub key patterns exist in
   tracked files, and the only `.pem` in history is Apple's *public* App Attest root CA. The repo is
   clean to publish as-is.
3. **Add OSS scaffolding** — `LICENSE` (Apache-2.0), `TRADEMARK.md`, `CONTRIBUTING.md`,
   `CODE_OF_CONDUCT.md`, issue/PR templates, and the label taxonomy the site reads
   (`good-first-issue`, `help-wanted`, `bug`, `area:ios`, `area:backend`).
4. **Seed the backlog** — 10–15 genuine `good-first-issue`s. A funnel pointing at an empty issue list
   converts nobody. Strong candidates already documented in the planning docs: the never-built
   Dockerfile (`docker build` unverified, no daemon in the authoring environment), the
   memory-default challenge store, the stale `README.md` top section (still claims iOS is "future
   Spec 2"), and the unvalidated `ATTEST_MODE=apple` path.
5. **Cut `v0.1.0`** so the changelog has content on day one.

### 7.1 Bundle ID migration (separate, tracked work)

Rename `com.brahy.spamfilter` → `com.brahy.hushield`. Touches:

- Xcode project bundle identifiers (app + both extensions + test targets)
- App Group identifier used for cross-extension sync
- App Attest `APP_ID`: `997DW79YCR.com.brahy.spamfilter` → `997DW79YCR.com.brahy.hushield`
- `.env.example`, `.planning/PROJECT.md`, `.planning/STATE.md`, `README.md`, `docs/MORNING-CHECKLIST.md`

This must land **before** the physical-device App Attest session, or that session validates an
identity that is about to change.

## 8. Deployment

Both the site and the product API run on `pm-prod-spamfilter`:

| Host | Serves |
|---|---|
| `hushield.com`, `www.hushield.com` | Marketing site |
| `api.hushield.com` | SpamFilter backend API |

Route 53 A records into `34.229.7.177`. TLS via Let's Encrypt (`getssl` already available). Site runs
under systemd, matching the pattern in `brightbridgelabs.com/deploy/`.

Known risk carried from `.planning/STATE.md`: the SpamFilter `Dockerfile` has **never been built** —
authored in an environment with no Docker daemon. `pm-prod-spamfilter` is ARM64, which is where that
gets tested for the first time. Expect the first image build to fail and budget for it.

## 9. Testing

- `internal/github`: table-driven tests against recorded JSON fixtures. No live API calls in tests.
- `internal/cache`: poller tests proving the three degradation states, that a failed refresh
  preserves the prior snapshot, and that `202` from `/stats/contributors` triggers retry rather than
  publishing an empty result.
- `internal/catalog`: schema validation — every entry has a valid status, and every `help-wanted`
  entry references a real issue number.
- `internal/handlers`: golden-file tests for rendered output of each page, including the
  stale-data and unavailable states.
- Rate-limit accounting test: assert the poll cycle's request count stays within a documented budget.

## 10. Open risks

| Risk | Mitigation |
|---|---|
| Cold start: 1 contributor, 0 stars reads as abandoned | Seed issues + `v0.1.0` before launch; lead with roadmap and architecture rather than social proof |
| Leaderboard discourages newcomers who cannot outrank the maintainer | Separate "first-time contributors" section; maintainer excludable from ranking via config |
| GitHub API shape changes | All parsing isolated in `internal/github` behind typed models; fixtures make breakage a test failure |
| Docker image build unverified on ARM64 | Build once manually on the box before wiring any automated deploy |
