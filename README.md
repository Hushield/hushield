# SpamFilter

A community spam-reporting backend, in the spirit of Hiya/Truecaller: iOS devices attested with
Apple App Attest report phone numbers as spam or not-spam, reports are folded into a decaying
weighted score per number, and any attested device can pull the resulting blocklist. There is no
user identity beyond a device's App Attest key — no accounts, no PII. The iOS client itself is a
future Spec 2; this repo is the Go + MySQL backend only.

## Architecture

```
                         +-------------------+
   iOS app (Spec 2,      |  Apple App Attest  |
   not in this repo) --> |  (device identity) |
                         +---------+----------+
                                   |
                                   v
   +-------------------------------------------------------------+
   |                    cmd/server (net/http)                    |
   |                                                               |
   |  /healthz                                                    |
   |  /api/v1/attest/challenge, /api/v1/attest/verify              |
   |  /api/v1/reports        (RequireDevice)                      |
   |  /api/v1/blocklist       (RequireDevice)                      |
   |  /api/v1/admin/overrides (RequireAdmin)                       |
   |                                                               |
   |  internal/api    -- routing, envelope, auth middleware        |
   |  internal/attest -- App Attest verify (mock or real Apple)    |
   |  internal/token   -- stateless signed device tokens           |
   |  internal/store    -- parameterized MySQL access               |
   |  internal/scoring -- pure, deterministic spam-scoring engine   |
   |  internal/trust    -- pure device-reputation formula            |
   +----------------------------+----------------------------------+
                                |
                                v
                       +-----------------+
                       |  MySQL 8+/9      |
                       |  (5 tables)      |
                       +-----------------+
                                ^
              +-----------------+-----------------+
              |                                    |
      cmd/recompute                          cmd/seed
      (periodic decay/rescore,               (one-shot import of
       cron job)                             FTC/FCC public data)
```

## Data model

Five tables (see `internal/db/migrations/0001_init.up.sql` for the authoritative schema):

- **devices** — one row per App Attest key (`key_id`); carries `trust_weight`, no PII.
- **phone_numbers** — one row per E.164 number; caches `cached_score`, `status`, `top_category`.
- **reports** — one updatable row per `(device_id, phone_number_id)`: a device's current vote/category on a number.
- **caller_names** — community-supplied caller-ID name per `(device_id, phone_number_id)`.
- **admin_overrides** — one row per number an admin has forced to `allow` or `block`.

## Endpoints

All responses use the `/api/v1` envelope: `{success, data, errors, meta:{timestamp, request_id}}`.

| Method | Path                        | Auth          | Purpose                                                    |
|--------|-----------------------------|---------------|--------------------------------------------------------------|
| GET    | `/healthz`                  | none          | Liveness check                                                |
| POST   | `/api/v1/attest/challenge`  | none          | Issue a single-use App Attest challenge                       |
| POST   | `/api/v1/attest/verify`     | none          | Verify an attestation, mint a device token                    |
| POST   | `/api/v1/reports`           | device token  | File/update a spam or not-spam vote on a number                |
| GET    | `/api/v1/blocklist`         | device token  | Pull the block/label delta since a cursor                      |
| POST   | `/api/v1/admin/overrides`   | admin token   | Force a number to always-allow or always-block                 |

## Configuration (env vars)

| Var                    | Default                                                                 | Notes                                              |
|-------------------------|--------------------------------------------------------------------------|-----------------------------------------------------|
| `DB_DSN`                 | `root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true` | MySQL DSN                                           |
| `ADDR`                   | `:8080`                                                                    | HTTP listen address                                 |
| `ADMIN_TOKEN`             | *(empty)*                                                                  | Bearer token for admin routes; empty disables them (503) |
| `ATTEST_MODE`             | `mock`                                                                     | `mock` (dev/test) or `apple` (real App Attest)      |
| `APP_ID`                  | *(empty)*                                                                  | `<TeamID>.<BundleID>`; required when `ATTEST_MODE=apple` |
| `DEVICE_TOKEN_SECRET`      | insecure dev default (warns at startup)                                    | HMAC secret signing device tokens; set in production |
| `DEVICE_TOKEN_TTL`         | `720h` (30 days)                                                           | Device token lifetime                               |
| `CHALLENGE_TTL`            | `5m`                                                                       | App Attest challenge lifetime                        |

## How to run

Migrations run automatically on server (and `seed`/`recompute`) startup — no separate migrate step.

```sh
# Server
DB_DSN='root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true' \
ADMIN_TOKEN=changeme \
go run ./cmd/server

# Seed from a public dataset (FTC/FCC CSV)
go run ./cmd/seed -source ftc -file ftc_dnc_complaints.csv -number-column Company_Phone_Number

# Periodic recompute (decay + trust), run e.g. every 15 minutes by cron/launchd:
*/15 * * * * /path/to/recompute >> /var/log/spamfilter-recompute.log 2>&1
```

### Smoke test

`scripts/smoke.sh` drives a running server through the full report -> block -> counter-report ->
drop -> admin-override -> gone lifecycle with curl. See the script header for usage; in short:

```sh
BASE=http://localhost:8080 ADMIN_TOKEN=changeme ./scripts/smoke.sh
```

## Scoring model, in brief

Each report contributes `trust_weight x category_multiplier x exponential_decay(age, half-life=30d)`
to a number's score; `not_spam` votes subtract instead of add. Scores never go negative. Two
thresholds classify a number: `>= 2.0` is `suspected` (surfaced as a "label"), `>= 5.0` is `blocked`.
A device's `trust_weight` is itself a pure function of its reporting tenure, volume, and how often
its votes have disagreed with the eventual community/admin outcome (see `internal/trust`). Identity
is Apple App Attest only — no accounts. An admin override (`allow`/`block`) always takes precedence
over the computed score. See `internal/scoring` for the exact, pure, deterministic implementation.

## Security note

`ATTEST_MODE=apple` wires up `internal/attest.AppleVerifier`, but **it must be validated against a
real Apple App Attest attestation from a physical device before this is used in production**. It has
only been exercised with synthetic/injected test material so far (see Task 3 concerns). Do not ship
`ATTEST_MODE=apple` to production without that validation. `ATTEST_MODE=mock` (the default) accepts
any attestation and must never be used outside dev/test.
