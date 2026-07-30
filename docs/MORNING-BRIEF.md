# Morning Brief — instructions for the scheduled agent

This file is the full instruction set for the **HuShield morning check** routine
(`trig_01VVz7aYajqyv5yUVrRDgNHf`), which runs daily at 15:00 UTC (8am
America/Los_Angeles). The routine's prompt is deliberately one line — "read
`docs/MORNING-BRIEF.md` and follow it exactly" — so this checklist can be edited
in a normal reviewed commit instead of by reconfiguring the routine.

**Owner:** John Brahy — john@brahy.com
**Repos:** `Hushield/hushield` (Go backend + native Swift iOS app),
`Hushield/website` (the site at hushield.com)

## Your environment and its limits

You run in Anthropic's cloud with a fresh checkout of `Hushield/hushield`. You
**cannot** reach John's laptop, the EC2 host (`10.30.1.244`), or anything on a
private network. **Never attempt SSH.** Everything below is reachable via `gh`
and public HTTPS.

Read `README.md` for what the product is if you need context.

## 1. Contributor funnel

```sh
gh issue list --repo Hushield/hushield --state open --json number,title,labels,createdAt,comments
```

Report the open issue count and how many carry `good-first-issue`.

**If good-first-issues drop below 3, say so prominently.** This project's primary
goal is recruiting outside contributors, and a site advertising nothing to claim
defeats its own purpose. It has happened before.

Flag any issue open more than 30 days with zero comments — those are going stale.

## 2. Pull requests — the highest-priority item

```sh
gh pr list --repo Hushield/hushield --state open --json number,title,author,createdAt,statusCheckRollup
gh pr list --repo Hushield/website  --state open --json number,title,author,createdAt,statusCheckRollup
```

For each open PR: age, author, and CI status.

**Flag any PR older than 3 days with no maintainer response, at the top of the
email.** An ignored pull request is the most damaging thing that can appear in
this brief. `CONTRIBUTING.md` promises contributors a response, and a project
whose whole pitch is "contributions are genuinely wanted" cannot leave one
sitting.

## 3. CI on `main`

```sh
gh run list --repo Hushield/hushield --branch main --limit 1
gh run list --repo Hushield/website  --branch main --limit 1
```

If red, name the failing job and quote **only the first genuine error line** —
not the whole log. Use `gh run view <id> --log-failed` and extract.

## 4. Production health (public HTTPS only)

```sh
curl -sS -o /dev/null -w '%{http_code}\n' https://api.hushield.com/healthz   # want 200
curl -sS https://api.hushield.com/healthz                                     # want "success":true
curl -sS -o /dev/null -w '%{http_code}\n' https://hushield.com/               # want 200

for h in api.hushield.com hushield.com; do
  echo | openssl s_client -connect "$h:443" -servername "$h" 2>/dev/null \
    | openssl x509 -noout -enddate
done
```

Certificates renew automatically via a certbot systemd timer. **If either has
fewer than 21 days remaining, treat that as broken** — it means renewal is not
working, and `certbot-renew.timer` has silently been disabled once already.

## 5. Production smoke suite

```sh
BASE=https://api.hushield.com ./scripts/smoke-prod.sh
```

Expect **17 passed, 0 failed**. Quote any failure verbatim.

The most important single check in there posts a *forged* App Attest attestation
and requires a 4xx. If that one ever passes-as-accepted, the server has fallen
back to `ATTEST_MODE=mock` and is accepting **any** device. Treat it as an
emergency and put it first in the email.

## 6. Email the digest

Send to **john@brahy.com**, subject `HuShield morning check YYYY-MM-DD`.

Rules:

- **Lead with anything broken or needing John's decision.** If nothing is,
  say everything is healthy in two lines and stop. Do not pad.
- No section that just says "nothing to report". Omit it.
- Never paste raw logs. One quoted error line, maximum.
- Plain prose and short lists. This is read on a phone before coffee.

Known-and-accepted items — **do not report these as problems** unless something
changes:

- REQ-08 and REQ-09 are unverified; they need a second phone line to call from
- APNs silent push is a documented no-op; there is no `.p8` configured
- The website runs without `GITHUB_TOKEN`, so it is rate-limited to 60 req/hr
  (issue #22)
- The App Store listing is named "Hushield"; the brand is "HuShield"

## 7. File an issue only when genuinely actionable

If you find something clearly broken — red CI, a certificate under 21 days, a
failing smoke check, or a PR ignored more than 7 days — then:

1. **Search open issues first.** `gh issue list --search "..."`. Never open a
   duplicate; comment on the existing issue instead.
2. Open **at most one** issue per run, with the evidence embedded: the failing
   command and its actual output.
3. Label it `bug`, plus the right `area:` label
   (`area:backend`, `area:ios`, `area:ops`).

Do not open issues for anything in the known-and-accepted list, for a
low-good-first-issue count (mention it in the email instead), or for anything
you have not verified by running the command yourself.

When in doubt, email it and do not file. A noisy tracker is worse than a quiet
one, and John reads the email either way.
