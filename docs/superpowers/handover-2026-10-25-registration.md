# Handover 2026-10-25: registration service, confirm-gated redesign

Read this first in a new Claude session. It replaces the handover of 10 Oct
2026 (in git history), whose operational facts are carried over in section 3.

## 1. State in one paragraph

The registration service has been live since **29 Sep 2026** (fly app
`arc42-registration`, `https://register.arc42.org`, Brevo, Turso). It works,
but spam that books a real course passes every check and reaches the back
office as an `UNBESTÄTIGT`/`UNCONFIRMED` mail. On **25 Oct 2026** Gernot
approved a redesign: store registrations in Turso, mail the back office
**once, after confirmation**, send a **weekly summary** of all attempts,
append confirmed registrations to a **Google Sheet**, and reject links in
the comments. The spec is written and committed; **nothing is implemented**.
Branch `registration/confirm-gated` holds the spec (and this file); `main`
is unchanged.

## 2. Where things are

| What | Where |
|---|---|
| **The new spec**, with the decision record of 25 Oct | `docs/superpowers/specs/2026-10-25-confirm-gated-registration-design.md` |
| The original service spec (still valid where the new one is silent) | `docs/superpowers/specs/2026-09-25-registration-service-design.md` |
| Service code, config table, log lines | `registration-app/`, its `README.md` |
| Form (field names, honeypots, hidden fields) | `_includes/registration-form.html`; field map in `registration-app/internal/intake/intake.go` (`fieldNames`) |
| Privacy text | `_pages/imprint-privacy.md`, section `#m-anmeldung` "Anmeldung zu Schulungen" |
| Open tasks | Todoist, project "arc42 sites", section "Registration service (replaces Formspark)" |
| Cross-site note (still describes Mailjet; its own Todoist task) | meta.arc42.org `raw/registration-service.md` |

## 3. How it runs (unchanged since 10 Oct)

- **fly**: one `shared-cpu-1x` 256 MB machine in `ams`, `auto_stop_machines =
  "suspend"`, scale to zero. Rate limits live in memory: **never scale to
  two**. `make reg-status`, `make reg-logs`.
- **Deploy = merge.** `.github/workflows/registration-app.yml` runs
  `make reg-check`-equivalent on every PR and **deploys to production on
  every push to `main` that touches `registration-app/**`**. There is no
  test deployment and no hidden test page any more. Consequences for the
  work below are in section 5.
- **fly secrets** (names only; values in Gernot's password manager):
  `TOKEN_KEY`, `BREVO_API_KEY`, `BACKOFFICE_TO` (info@arc42.de and
  trainings@arc42.org), `TURSO_DATABASE_URL`, `TURSO_AUTH_TOKEN`.
  `TEST_RECIPIENTS` is unset in production.
- **Brevo**: sender `trainings@arc42.org`, open and click tracking off.
  Bounces and blocks: Brevo > Transactional > Logs, search by the R- id tag.
- **Turso**: database `arc42-registration`, tables `confirmed` and
  `code_failures`, no personal data so far. The redesign adds personal data
  (section 4 of the spec); **the database region must be verified as EU**
  (`turso db show arc42-registration`) before PR 2 goes live, and the
  privacy text must name the storage.
- **DNS arc42.org** (HostEurope KIS): SPF `include:spf.brevo.com`, DKIM
  `brevo1/brevo2._domainkey`, DMARC `p=none` with `rua` to dmarcguard and
  Brevo (no `ruf`, decided 21 Oct), CNAME `register` to fly.

## 4. The work: four PRs, in this order

Spec section 10 has the detail. Each PR gets its own branch from `main`,
`make reg-check` green, and is reversible alone.

| PR | Branch (suggested) | Touches | Done when |
|---|---|---|---|
| 1 Spam rules | `registration/spam-rules` | `intake` (rejections, score, `js_ok`), the form include (hidden `js_ok` filled by script), fail-page sentence DE/EN, `fieldNames` | the sample from spec section 1 is dropped in a table test; a plain real registration scores 0; `make site` shows the new field in `_site/anmeldung/index.html` |
| 2 Store + one mail | `registration/confirm-gated` (reuse this branch) | `store` (new tables in both Turso and Memory), `web/submit.go`, `web/confirm.go`, `web/correct.go`, `mail` (UNCONFIRMED and CORRECTED templates removed, CONFIRMED grown), privacy page, README | handler tests for store-write-fails, registrant-mail-fails, store-down-at-confirm; golden files regenerated and read; back office briefed before merge |
| 3 Summary | `registration/weekly-summary` | new `summary` unit, due-check in a middleware around every route, retention, `SUMMARY_NOW` | fake-clock tests from spec 10.3; after deploy: the Todoist task "cron-job.org: GET …/healthz every Monday 07:00" |
| 4 Sheet | `registration/sheet` | new `sheet` unit (service-account JWT, append), confirm hook, reconciliation in the summary run | fake-server tests; one manual row into a test sheet; column layout agreed with Peter first (Todoist) |

## 5. Things the spec does not say, learned from the code

- **Turso is spoken to over HTTP, no driver.** `store/turso.go` keeps the
  `CREATE TABLE IF NOT EXISTS` statements in the `schema` slice and runs
  them once per process in front of the first pipeline. Add the new tables
  there; nothing else migrates. Arguments are typed (`text`, `integer`);
  rows come back as typed values, see `firstInt`.
- **The Memory store must mirror Turso** method for method, or `make
  reg-demo` and the handler tests diverge from production.
- **Mail templates have golden files** in `internal/mail/testdata/`.
  After a template change: `go test ./internal/mail -update`, then read the
  diff of `testdata/` before committing it.
- **The form include and `fieldNames` change together** (CLAUDE.md). The
  validator `scripts/validate_trainings.rb` runs in CI on changes to the
  registration includes; it does not know field names, so `js_ok` needs
  no change there.
- **Jekyll includes share scope**: a new variable in the form include needs
  the `rf_` prefix like its neighbours.
- **`token.Claims` stays small.** The confirm step reads the registration
  by `Claims.ID` from the store; the token is not the place for the data.
- **Confirm must never fail because the store is down** (existing rule,
  `web/confirm.go`): render the short mail from the token then, as today,
  plus the "see the summary" line.
- **`WriteTimeout` is 45 s** in `main.go`, sized for two sends. The summary
  send runs in the background, so it must not be tied to the request
  context; give it its own `context.WithTimeout`.
- **Standard library only.** No `go.sum` exists and that is deliberate.
  The sheet unit's JWT is `crypto/rsa` + `crypto/sha256` +
  `encoding/base64`, about 60 lines.
- **Testing real mails without a test app**: run the service locally with
  `MAILER=brevo`, the real `BREVO_API_KEY`, `TEST_RECIPIENTS=<your
  address>` and `ENVIRONMENT` unset. Outside production the allow-list is
  enforced, so nothing can reach a stranger. `make reg-demo` runs the
  whole flow with mails printed instead.
- **No em-dashes** in anything user-visible (mails, pages, fail-page text).

## 6. Decisions on record (25 Oct 2026, Gernot)

- Weekly summary, German only (proposed; confirm with the back office).
- Links in comments or billing: **visible rejection** with a reason, not a
  silent drop. Name fields checked the same way.
- A failed registrant mail at submit shows the **fail page** (today:
  success).
- Summary trigger: due-check in every request, **cron-job.org pings
  `/healthz` on Monday 07:00**. Rejected: GitHub Actions cron, cron-job.org
  calling a token-protected endpoint, an always-on machine (spec 7.3; the
  always-on machine is the fallback, about 2 USD a month).
- Dead-man's switch (healthchecks.io) optional, not in the first round.
- The "back-office overview" decision from the 10 Oct handover is settled
  by this spec: the weekly mail and the sheet are the overview. The Todoist
  task "Decide: price display …, and a back-office overview" can drop its
  second half.

Open: Turso region; sheet columns; German-only summary; the privacy wording.

## 7. Resuming

```bash
git switch registration/confirm-gated && git pull   # the spec and this file
make reg-check     # Go 1.23+: tests, vet, gofmt
make reg-demo      # whole flow locally, mails printed
make reg-logs      # production logs (flyctl, logged in)
```

**For a Claude session:** read this file, then the new spec in full, then
`registration-app/README.md`. Work one PR at a time. Opus is sufficient:
the design is settled and the spec is precise; the care needed is in
keeping the store-down fallbacks and the exactly-once confirm as they are.
Ask Gernot before merging PR 2, because the merge is the production deploy.
