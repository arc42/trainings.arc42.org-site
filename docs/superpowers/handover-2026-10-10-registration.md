# Handover 2026-10-10: registration service, live

Read this first in a new Claude session or on another machine. It replaces the
handover of 27 Sep 2026 (in git history under its old name), which described
the state before the go-live and the Mailjet setup that no longer exists.

## 1. State in one paragraph

Since **29 Sep 2026** every registration form on **trainings.arc42.org** posts
to the registration service at `https://register.arc42.org/submit` (fly app
`arc42-registration`). Mail goes out through **Brevo** only; Mailjet was
dropped (its account never passed review) and its DNS records are gone. The
service's only stored state, confirmed ids and wrong-code counts, is in
**Turso**. Peter Hruschka tested in production, and real registrations have
confirmed since. **arc42.de** still runs its own `/anmeldung/` forms on
Formspark; moving those over is the next real step (section 4). Nothing is
in progress: `main` is clean, no open branches for this work.

## 2. Where things are

| What | Where |
|---|---|
| Design, also the decision record (Brevo, Turso, single use, code) | `docs/superpowers/specs/2026-09-25-registration-service-design.md` |
| Plan 3, go-live and migration (unchecked boxes = what is left) | `docs/superpowers/plans/2026-09-26-registration-go-live.md` |
| Plans 1 and 2 (waiting list, service), done | `docs/superpowers/plans/2026-09-26-*.md` |
| Service code, config table, log lines | `registration-app/`, its `README.md` |
| Form and steps bar on the site | `_includes/registration-form.html`, `_includes/registration-steps.html` |
| Privacy text | `_pages/imprint-privacy.md`, section "Anmeldung zu Schulungen" |
| Open tasks | Todoist, project "arc42 sites", section "Registration service (replaces Formspark)"; one task in project "arc42-spam-protection" |
| Cross-site note | meta.arc42.org `raw/registration-service.md` (pushed, but still describes Mailjet; Todoist task) |

## 3. How it runs

- **fly**: one machine in `ams` (rate limits live in memory, so never scale to
  two), `auto_stop_machines = "suspend"` (resume 0.7 to 1.3 s, measured
  29 Sep). `make reg-status`, `make reg-logs`. Deploys: CI on push to `main`
  touching `registration-app/**` (secret `FLY_REGISTRATION_API_TOKEN`), or
  `make reg-deploy` from the working tree.
- **fly secrets** (names only; values are in Gernot's password manager):
  `TOKEN_KEY`, `BREVO_API_KEY`, `BACKOFFICE_TO` (info@arc42.de and
  trainings@arc42.org, comma-separated), `TURSO_DATABASE_URL`,
  `TURSO_AUTH_TOKEN`. `TEST_RECIPIENTS` is unset (production).
- **Brevo**: sender `trainings@arc42.org`, open and click tracking off in the
  account, no branded tracking subdomain (not needed while tracking is off).
  Bounces and blocks: Brevo > Transactional > Logs, search by the R- id tag.
- **Turso**: database `arc42-registration`, tables `confirmed` and
  `code_failures`, no personal data (hence not named in the privacy text,
  Gernot's decision). Token created without expiry. If Turso is down, the link
  still confirms (fail open), the 6-digit code is refused.
- **DNS arc42.org** (HostEurope KIS), checked after the Mailjet cleanup:
  SPF `v=spf1 include:mailbox.org include:spf.brevo.com ~all`; DKIM CNAMEs
  `brevo1._domainkey`, `brevo2._domainkey`; `brevo-code` TXT; DMARC
  `v=DMARC1; p=none; rua=mailto:dmarc@arc42.org,mailto:rua@dmarc.brevo.com`;
  MX mailbox.org; CNAME `register` to fly with certificate.

## 4. What is left, in order (all in Todoist)

| Due | Task | Who |
|---|---|---|
| 13 Oct (checks 6, 13, 20 Oct; wrap-up by 27 Oct) | Observe the registration service (Plan 3, Task 3) | Gernot |
| 27 Oct | Prompt Claude: turn arc42.de's `/anmeldung/` and `/anmeldungEN/` into pointers to trainings.arc42.org (Plan 3, Task 4); the prompt is in the task | Gernot prompts, Claude works in arc42.de-site |
| 30 Oct | Prompt Claude: bring the meta.arc42.org note up to date | Gernot prompts |
| 30 Oct | Decide: price display and a back-office overview (section 5) | Gernot |
| 17 Nov | Delete the dead registration form code on arc42.de | Gernot prompts |
| 24 Nov | Retire Formspark (Plan 3, Task 5); step 3 is a Claude prompt | Gernot, then Claude |

When all of that is done: set the spec's status line to `implemented` with the
date (Plan 3, Task 5, Step 4).

The examples.arc42.org task in the same project is unrelated and recurring.

## 5. Decisions on record, and open ones

Taken (details and reasons in the spec):
- Registrants confirm their **e-mail address**, not the registration.
- The registrant mail shows **no price** (special agreements; prices are
  settled with the invoice).
- A registration confirms **once**; the code locks after 5 wrong tries, the
  link keeps working.
- After a correction, the old confirm link still works (accepted 27 Sep; the
  back office gets the correction notice).
- Waitlist dates take registrations; the mail says "wir melden uns, um das
  weitere Vorgehen zu klären".
- arc42.de/termine stays as its own German schedule; only its registration
  pages move.

Open:
- **Price display** in the registration flow: "we consider price display
  later" (Gernot, 28 Sep). The form's booking summary shows prices; the
  mails show none.
- **Back-office overview** of registrations. Recommendation on record: not
  now; if wanted after the observation weeks, a PII-free event log shown in
  the existing admin app.

## 6. Resuming

```bash
git switch main && git pull
make reg-check    # Go 1.23+: tests, vet, gofmt
make reg-demo     # whole flow locally (Docker + Go), mails printed in the terminal
make reg-logs     # production logs (flyctl, logged in)
```

**For a Claude session:** read this file and the spec. For a Todoist task that
says "Prompt Claude", the prompt is in the task description; the plan it
names has the steps.
