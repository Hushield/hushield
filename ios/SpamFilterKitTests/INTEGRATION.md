# Live integration tests

`IntegrationTests.swift` drives the real `SpamFilterKit` stack (`URLSessionTransport` ->
`APIClient` -> `EnrollmentService` -> report/lookup/blocklist) over actual HTTP against a
locally running Go backend in `ATTEST_MODE=mock`. It proves the wire contract, not scoring
thresholds.

It is gated on the `SPAMFILTER_INTEGRATION_BASE_URL` environment variable and is **skipped**
(not failed) whenever that variable is unset or the server it points at doesn't answer
`GET /healthz`. The normal hermetic `SpamFilterKitTests` run (no env var set) is unaffected.

## Running it

From the repo root (`SpamFilter/`):

```sh
# 1. Create the dev database (idempotent).
mysql -h 127.0.0.1 -u root -e 'CREATE DATABASE IF NOT EXISTS spamfilter_dev'

# 2. Start the server in mock attest mode (migrations auto-run on startup).
ATTEST_MODE=mock \
DB_DSN='root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true' \
ADMIN_TOKEN=changeme \
ADDR=':8080' \
go run ./cmd/server &

# 3. Run just the integration test, with the base URL set.
#
# IMPORTANT: the test binary runs inside the Simulator as a separate process,
# so a plain `SPAMFILTER_INTEGRATION_BASE_URL=... xcodebuild ...` prefix does
# NOT reach it -- xcodebuild only forwards environment variables that are
# `export`ed (not just prefixed on the command line) under the `TEST_RUNNER_`
# prefix, stripping that prefix before handing them to the test host.
cd ios
xcodegen generate
export TEST_RUNNER_SPAMFILTER_INTEGRATION_BASE_URL=http://localhost:8080
xcodebuild \
  -project SpamFilter.xcodeproj -scheme SpamFilter -sdk iphonesimulator \
  -destination 'id=<simulator UDID, from `xcrun simctl list devices available`>' \
  test -only-testing:SpamFilterKitTests/IntegrationTests \
  CODE_SIGNING_ALLOWED=NO
unset TEST_RUNNER_SPAMFILTER_INTEGRATION_BASE_URL

# 4. Stop the server.
kill %1
```

If `:8080` is already in use by something else, pick another port for both the server's
`ADDR` and the `TEST_RUNNER_SPAMFILTER_INTEGRATION_BASE_URL` value.

Without step 3's env var (i.e. a normal `xcodebuild test` run with no
`TEST_RUNNER_SPAMFILTER_INTEGRATION_BASE_URL` exported), `IntegrationTests` reports as
skipped and the rest of `SpamFilterKitTests` runs exactly as before.
