<!--
Thanks for contributing to HuShield. Keep this PR to one logical change — a bugfix plus an
unrelated refactor plus a formatting sweep is three PRs, and reviewing it as one is slow for
everybody.
-->

## What problem does this solve?

<!-- Describe the problem, not just the diff. Link the issue: "Fixes #123" / "Part of #123". -->

## What changed

<!-- A short summary of the approach. Call out anything a reviewer might not expect. -->

## How was it tested?

<!--
Name the tests you added or changed, and paste the relevant `make test` output. If you could not run
part of the suite, say so explicitly and why — an honest gap is fine, a silent one is not.
-->

## Checklist

- [ ] `make test` passes (Go `-race -cover` + iOS), or I've explained below which part I couldn't run
- [ ] I added or updated tests covering this change
- [ ] I watched the new test fail before making it pass
- [ ] I did not edit `ios/SpamFilter.xcodeproj` by hand (it's generated — `ios/project.yml` is the source of truth)
- [ ] This introduces no PII: no user accounts, no phone numbers tied to reporters, no contact data, no device fingerprinting
- [ ] I matched the surrounding code's style, and kept unrelated changes out of this PR
- [ ] Docs updated if this changes behavior, config, or the API surface

## Anything reviewers should know?

<!-- Open questions, tradeoffs you weren't sure about, follow-up work you're deliberately leaving. -->
