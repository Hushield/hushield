# Contributing to HuShield

HuShield is free, stays free, and is built in the open. Contributions are genuinely wanted — not as a
formality. This document tells you how to get a change merged with as little friction as possible.

If anything here is wrong, unclear, or out of date, that itself is a valid bug report.

## The one design constraint you must know

**HuShield holds no personally identifiable information, anywhere, by design.** The only identity in
the entire system is an Apple App Attest `key_id`. There are no user accounts, no phone numbers
attached to reporters, no contact uploads, no email addresses, no device fingerprints.

This is not a preference we might trade away later — it is the product. A PR that introduces a user
identifier, an analytics identifier, or any linkage between a report and a person will be declined
regardless of how well it is written. If you think a feature needs identity, open an issue first and
let's find another way; there usually is one.

## Repository layout

```
cmd/server/        HTTP API entry point
cmd/recompute/     Periodic score decay + trust recomputation
cmd/seed/          One-shot import of FTC/FCC public complaint data
internal/api/      Routing, response envelope, auth middleware
internal/attest/   App Attest verification (mock and real Apple modes)
internal/scoring/  Pure, deterministic spam-scoring engine
internal/trust/    Pure per-device reputation formula
internal/store/    Parameterized MySQL access
internal/token/    Stateless signed device tokens
internal/push/     APNs silent-push notifier
internal/db/       Migrations (applied automatically at startup)
ios/SpamFilterKit/ Shared framework: API client, attest, sync, stores
ios/SpamFilter/    SwiftUI app (Report / Lookup / Status / Setup)
ios/CallDirectoryExtension/   Blocks and labels incoming calls
ios/MessageFilterExtension/   Offline SMS classification
```

`internal/scoring` and `internal/trust` are deliberately pure and dependency-free. Keep them that
way — they are the easiest parts of the system to reason about and test, and that is on purpose.

## Prerequisites

| Tool | Notes |
|---|---|
| Go | Version per `go.mod` |
| MySQL 8+ | Migrations run automatically on startup; no separate migrate step |
| Xcode | Required for any iOS work |
| `xcodegen` | `brew install xcodegen` — see the warning below |

## Important: the Xcode project is generated

`ios/SpamFilter.xcodeproj` is **generated output**. Do not edit it, and do not commit changes to it.

The source of truth is **`ios/project.yml`**. Change that, then run:

```bash
cd ios && xcodegen generate
```

A PR that hand-edits the `.xcodeproj` will conflict with everyone else and be asked to convert to a
`project.yml` change.

## Running things

```bash
# Backend
DB_DSN='root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true' \
ADMIN_TOKEN=changeme \
go run ./cmd/server

# End-to-end smoke test against a running server
BASE=http://localhost:8080 ADMIN_TOKEN=changeme ./scripts/smoke.sh
```

`ATTEST_MODE` defaults to `mock`, which accepts **any** attestation. That is correct for local
development and must never be used in production. `ATTEST_MODE=apple` requires `APP_ID` to be set to
`997DW79YCR.com.brahy.hushield`.

## Tests: what "green" means

```bash
make test       # the full gate: Go + iOS. This is what must pass.
make test-go    # go test ./... -race -cover
make test-ios   # xcodegen + xcodebuild test on a resolved simulator
```

`make test` is the definition of green, and `make deploy` depends on it, so a red suite cannot reach
a deploy. Install the same gate on `git push` with:

```bash
make hooks
```

Notes that will save you time:

- The Go suite runs with `-race`. If you add concurrency, expect the race detector to find what you
  missed — that is the point.
- `SpamFilterKitTests/IntegrationTests` self-skip via `XCTSkip` unless
  `SPAMFILTER_INTEGRATION_BASE_URL` is set, so they will not fail the gate when you have no backend
  running. Set that variable to run them against a real local server.
- `make test-ios` resolves an available simulator at runtime rather than hardcoding one. If it fails
  with "iOS <version> is not installed", your Xcode's SDK and installed simulator runtimes are
  mismatched — run `xcodebuild -downloadPlatform iOS`.

## Writing tests

We use TDD, and reviewers will look for it: write the failing test, watch it fail for the right
reason, then make it pass.

Test behavior, not implementation. `internal/scoring` and `internal/trust` are pure functions — prefer
table-driven tests over mocks. For anything touching the database, use the helpers in
`internal/dbtest` rather than standing up your own fixtures.

A test that cannot fail is worse than no test, because it reports safety that isn't there.

## Submitting a change

1. Find or open an issue. For anything non-trivial, agreeing on the approach first saves you rework.
2. Branch from `main`.
3. Make the change, with tests.
4. Run `make test` and confirm it passes.
5. Open a PR describing **what problem it solves**, not just what it changes. Link the issue.

Keep PRs focused. One logical change per PR — a bugfix plus an unrelated refactor plus a formatting
sweep is three PRs, and reviewing it as one is slow for everybody.

Match the surrounding code's style even where you'd personally do it differently. Consistency is
worth more than any individual preference.

## Labels

| Label | Meaning |
|---|---|
| `good-first-issue` | Small, well-scoped, no deep system knowledge required. Start here. |
| `help-wanted` | We would especially like outside help on this. |
| `bug` | Something is broken. |
| `area:backend` | Go / MySQL side. |
| `area:ios` | Swift app or extensions. |
| `needs-device` | Requires a physical iPhone (App Attest cannot be validated in a simulator). |

If you want to work on an issue, comment on it. No formal assignment process — just say so, so two
people don't duplicate effort.

## Review expectations

Reviews are done by a small team, so response time varies. If a PR has had no response in a week,
comment on it — that is not nagging, it is a useful nudge.

Not every PR gets merged. If yours is declined, it is about scope or approach, not about you, and we
will explain the reasoning.

## Code of Conduct

Participation is governed by [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).

## Licensing your contribution

Contributions are accepted under [Apache-2.0](LICENSE), matching the project. Note that the HuShield
*name* is reserved separately — see [`TRADEMARK.md`](TRADEMARK.md). You retain copyright in your
contribution; you are simply licensing it under the project's terms.
