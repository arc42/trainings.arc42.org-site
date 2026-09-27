# Handover 2026-09-27: registration service, waiting list, mail setup

Read this first when you continue on another machine, or in a new Claude
session. It says what exists, what is verified, what waits for whom, and how
to pick up. Everything referenced is in this repository on branch
`feat/registration-service` unless stated otherwise.

## 1. Where things are

| What | Where |
|---|---|
| Design (authoritative) | `docs/superpowers/specs/2026-09-25-registration-service-design.md` |
| Flow diagram and slides | `docs/registration-flow/registration-flow.svg`, `.pptx` |
| Plan 1, waiting list | `docs/superpowers/plans/2026-09-26-waiting-list.md` |
| Plan 2, the service | `docs/superpowers/plans/2026-09-26-registration-service.md` |
| Plan 3, go-live and migration | `docs/superpowers/plans/2026-09-26-registration-go-live.md` |
| How to execute the plans (context rules, blocks, line ranges) | `docs/superpowers/plans/2026-09-26-execution.md` |
| Service code and README | `registration-app/` |
| Manual tasks | Todoist, project "arc42 sites", section "Registration service (replaces Formspark)" |
| Cross-site note | meta.arc42.org `raw/registration-service.md` (commit `f69fde4` on its `main`, **not pushed**, see 6) |

## 2. Branches (all rebased on `origin/main` as of 2026-09-27, pushed)

| Branch | Contains | State |
|---|---|---|
| `design/registration-service` | spec, diagram, slides, the three plans, execution plan | docs only |
| `feat/waiting-list` | design + Plan 1 (6 tasks) + review fix | **done**, reviewed, ready for a PR |
| `feat/registration-service` | design + Plan 2 tasks 1 to 10 and 12, review fixes, later additions (below) | done except Task 11 (deploy) |

The two feature branches are independent and both carry the design commits.
Merge order does not matter; the second PR will simply show fewer commits.

History was rewritten on 2026-09-27 before the first push to remove a real
person's mail address from old commits. Nothing earlier was ever pushed.

## 3. What is done and verified

**Waiting list** (`feat/waiting-list`): cards show `waitlist` as "Ausgebucht,
Warteliste verfügbar" / "Fully booked, waiting list available" with a "Auf die
Warteliste" / "Join waiting list" button; `full` as "Ausgebucht" / "Fully
booked" without a button; waitlist dates are in the form with "(Warteliste)";
the form's price box shows the regular price only (no early bird, no alumni);
admin app explains each status, warns correctly, PR titles in words. Verified
with `make app-check`, `make site` plus grep with temporary statuses,
`make check-links`, and a fresh reviewer.

**Registration service** (`feat/registration-service`), Go, standard library only:
- `POST /submit`: checks (size, origin incl. `null`, honeypots, per-IP limit
  with IPv6 per /64, required fields, email, booking code bookable per feed),
  back-office mail UNBESTÄTIGT/UNCONFIRMED, registrant mail with course facts
  and confirm link (at most 3 per address and day), then **its own page**
  showing the address with a field to correct it.
- `POST /correct`: typo correction, max 2 per registration, 30 minutes after
  submitting; back office gets "(E-MAIL KORRIGIERT)".
- `GET /confirm` shows a page only; `POST /confirm` sends BESTÄTIGT/CONFIRMED.
  Correction and confirm tokens cannot be swapped.
- Waiting-list dates: same flow, a note in the registrant mail, WARTELISTE in
  the back-office mail.
- Bot drops get the identical page (same token length, same address spelling).
- 8 test packages green (`make reg-check`), Docker image 8 MB and runs,
  CI workflow `.github/workflows/registration-app.yml` (deploy job skips
  until `FLY_REGISTRATION_API_TOKEN` exists).
- Site side: `endpoint` option in `_includes/registration-form.html`,
  `registration_endpoint` / `registration_test_endpoint` in `_config.yml`,
  confirmed pages, hidden test pages `/anmeldung-test-8r4tqz/` and
  `/registration-test-8r4tqz/` (noindex, not in sitemap). Production forms
  still post to Formspark with unchanged attributes.
- Two fresh reviews (opus); all Critical and Important findings fixed.

**arc42.org mail DNS** (HostEurope KIS, nameservers `ns29/ns30.domaincontrol.com`),
checked from outside on 2026-09-27:

| Record | Value |
|---|---|
| SPF (`arc42.org` TXT) | `v=spf1 include:mailbox.org include:spf.mailjet.com ~all` (one record only) |
| Mailjet DKIM | `mailjet._domainkey` TXT `k=rsa; p=MIIB...` |
| Mailjet validation | `mailjet._a2e86ebf` TXT |
| DMARC | `_dmarc` `v=DMARC1; p=none; rua=mailto:dmarc@arc42.org;` |
| MX | `mxext1.mailbox.org` 10, `mxext2` 10, `mxext3` 20 (set in the KIS "MX-Records" section, Präfix empty) |
| mailbox.org verification | `1ec3c7...` TXT |

Mail to `trainings@arc42.org` arrives in Gernot's mailbox.org (tested from
me.com). Aliases `trainings@` and `dmarc@` exist. mailbox.org DKIM CNAMEs are
optional (only for sending from @arc42.org via mailbox.org) and not set.

## 4. Waiting for whom

| Waiting for | Then | Todoist task |
|---|---|---|
| Mailjet account review (banner "we are reviewing your request", takes days) | domain no longer "Pending" | "Mailjet: check that arc42.org is validated at DOMAIN level" |
| Gernot | sub-account API key `arc42-registration`, tracking off | "Mailjet: create a separate API key pair" |
| Gernot | curl test mail, send Claude the `Authentication-Results` line (want `dkim=pass`, `dmarc=pass`) | "Mailjet: send one test mail with curl" |
| Gernot | `fly apps create arc42-registration`, `fly secrets import` of five values | "fly.io: create the arc42-registration app and set its secrets" |
| Gernot (optional before go-live) | `FLY_REGISTRATION_API_TOKEN` in GitHub | "GitHub: add FLY_REGISTRATION_API_TOKEN" |

## 5. Next steps, in order

1. **Now possible:** open PRs for `feat/waiting-list` (ships independently)
   and, if wanted, `feat/registration-service` (safe to merge: production
   forms stay on Formspark; the test pages point at a service that is not
   deployed yet, so they would error until step 3).
2. Gernot's tasks in section 4.
3. **Plan 2, Task 11**: deploy in test mode (`make reg-deploy`), smoke tests
   against `arc42-registration.fly.dev`, and check whether fly overwrites a
   forged `Fly-Client-IP` header (open question from review).
4. Gernot: end-to-end test in test mode incl. a corporate Outlook inbox
   (Todoist "Registration service: end-to-end test").
5. Plan 3: go-live on `register.arc42.org` (CNAME + fly cert, Todoist task),
   observation, arc42.de stubs, Formspark retired.

## 6. Resuming on another machine

```bash
git clone git@github.com:arc42/trainings.arc42.org-site.git   # or git fetch
git switch feat/registration-service
make reg-check            # Go 1.23+; tests, vet, gofmt
make reg-demo             # Docker + Go: whole flow locally, mails in the terminal
                          # form: http://localhost:4260/anmeldung-test-8r4tqz/
                          # (every form of the demo site posts to the local service)
```

Needs Docker, Go 1.23 or newer, `flyctl` for deploys. Stop the demo with
Ctrl-C and `docker rm -f trainings-regdemo`.

Not on GitHub, and therefore not on the other machine:
- the executor ledgers under `.superpowers/sdd/` (gitignored). Their content
  that matters is in section 7 below.
- meta.arc42.org commit `f69fde4` (the raw note). That repo had someone's
  uncommitted ingest work in `wiki/index.md` and `_system/log.md`, so only the
  note was committed and nothing was pushed. Push it from the original
  machine, or copy `raw/registration-service.md`.
- local backup branches were deleted after the push.

**For a Claude session:** start by reading this file, then
`docs/superpowers/plans/2026-09-26-execution.md` (context rules: one task at a
time by line range). Plan 2 Task 11 is the next task. Unchecked boxes in the
plan files mark what is left.

## 7. Rulings and deferred items (from the executor ledgers)

Rulings (decisions taken without asking, each with its cost if wrong):
- Tests in the plans were corrected where they could not fail for the right
  reason (admin dropdown test used an unreachable GitHub; "back to open" test
  started open). Cost: none.
- Plan 2 code was copied from a byte-identical reference copy instead of
  retyped; the copy was deleted afterwards. Cost: none.
- HTML mail goldens were checked by grep, not in a browser. Cost: a layout
  flaw would show up only in the end-to-end test.
- Re-graded to Important and fixed: contradictory card (waitlist plus "few
  seats"), "Sonstige" wording for bookings without feed facts, real-looking
  addresses in the public repo, a committed PowerPoint lock file, an empty
  `registration_endpoint` breaking every form.
- Out of scope as the spec decided: other feed consumers may still list only
  open dates.

Deferred minors (not fixed, known):
- 32 KB body cap hit before the character limits for non-Latin text; plain 413.
- An extreme name plus address can make a confirm token too long to open.
- Control characters in a booking code can reach the back-office subject, only while the feed is down.
- A cancelled request during a feed fetch can skip the code check for 30 s on a cold machine.
- After a correction, the old confirm link (to the mistyped address) still works; the correction notice mitigates.
- `timeline_course.html` usage docs lack the three new parameters; a comment in the admin `server.go` overstates where `statusLabel` is used; one badly wrapped comment in `registration-form.html`.

Open decision: a back-office UI for registrations. Recommendation on record:
not now; if wanted after the observation weeks, a PII-free event log shown in
the existing admin app.
