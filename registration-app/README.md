# Registration service

Receives the registration form of trainings.arc42.org, stores it in Turso,
mails the registrant through Brevo, and confirms a registration when the
registrant types the 6-digit code from their mail on the page that stayed
open, or presses the button reached from the link in it. Only then does the
back office get its one mail, BESTÄTIGT/CONFIRMED, with the complete
registration. Turso also records which registrations are confirmed and how
many wrong codes each got, so a registration confirms exactly once. Every
Monday the back office gets a summary of the week's attempts: confirmed,
open and dropped; after it, what is final is deleted.

Design: [spec](../docs/superpowers/specs/2026-09-25-registration-service-design.md),
the confirm-gated flow since Oct 2026:
[spec](../docs/superpowers/specs/2026-10-25-confirm-gated-registration-design.md),
flow diagram: [registration-flow.svg](../docs/registration-flow/registration-flow.svg).

## Endpoints

| Route | Does |
|---|---|
| `POST /submit` | the form posts here; checks, store, registrant mail, the "please confirm" page or 303 to the fail page |
| `GET /confirm?t=…` | shows the course and one button. **Sends nothing**: corporate link scanners open every link in a mail |
| `POST /confirm` | sends the BESTÄTIGT / CONFIRMED mail, 303 to the confirmed page |
| `POST /code` | the 6-digit code typed on the page that stayed open; same effect as `POST /confirm`, locked after 5 wrong codes |
| `POST /correct` | corrects a mistyped address (at most 2 per registration, within 30 minutes) and sends the registrant mail, with a new code, to the corrected address |
| `GET /healthz` | fly's health check, and the Monday ping that sends the weekly summary (below) |

## Configuration

| Env | Secret | Meaning |
|---|---|---|
| `BREVO_API_KEY` | yes | Brevo API key (`xkeysib-…`). Open and click tracking must be off in the Brevo account: the API has no per-message switch |
| `TOKEN_KEY` | yes | 32 random bytes, base64 (`openssl rand -base64 32`). Rotating it invalidates every open confirm link |
| `BACKOFFICE_TO` | yes | where BESTÄTIGT/CONFIRMED goes; several addresses **comma**-separated (a semicolon is refused at start). Personal addresses, and this repo is public |
| `TEST_RECIPIENTS` | yes | if set: **test mode**. Registrant mails only to these addresses, `[TEST]` in every subject. Personal addresses too |
| `PUBLIC_URL` | no | this service's base URL, the start of every confirm link |
| `SITE_URL` | no | where the success, fail and confirmed pages live (default trainings.arc42.org) |
| `FEED_URL` | no | the course feed (default `/api/trainings.json` on the site) |
| `ALLOWED_ORIGINS` | no | comma list of form origins; others are dropped silently |
| `MAIL_FROM`, `MAIL_FROM_NAME`, `REPLY_TO` | no | default `trainings@arc42.org`, `arc42 Trainings`, `info@arc42.de` |
| `ENVIRONMENT` | no | `PRODUCTION` or anything else |
| `TURSO_DATABASE_URL`, `TURSO_AUTH_TOKEN` | yes | the Turso database (EU, `aws-eu-west-1`) for registrations, confirmed ids and wrong-code counts (`turso db show --url`, `turso db tokens create`). Required in production; without them a test deployment keeps this in memory and forgets it on every restart |
| `MAILER` | no | `brevo` (default) or `log`: print mails instead of sending |
| `SUMMARY_NOW` | no | `1`: send a weekly summary at start, whatever the day, to see one on a test deployment. Refused in production |

Outside `ENVIRONMENT=PRODUCTION` the service **refuses to start** unless
`TEST_RECIPIENTS` is set (or `MAILER=log`), so a test deployment cannot mail
anyone off the list.

## The weekly summary

There is no scheduler. Every request, `/healthz` included, asks whether the
summary is due (`internal/summary`): it is, once the most recent Monday
07:00 Berlin time lies after the last summary the store recorded. The run
happens in the background, so the request is not slowed down. Its steps:
claim the week in the `summaries` table (two machines during a deploy send
one mail), mark registrations confirmed whose mail went out but whose row
was not updated, send, then delete what is final: drops, confirmed ones,
and open ones whose link has expired. An open registration whose link
still works stays and is listed again next week with its final status.

The machine sleeps when idle, so a **cron-job.org job** makes sure there is
a request: `GET https://register.arc42.org/healthz`, every Monday at 07:00
Europe/Berlin, with failure notification. It carries no secret and can only
make the summary late: without it, the next submit or confirm sends the
overdue summary, marked "Verspätet". The summary goes out even when the
week was empty, so **a Monday without it means the service is not running**.

The first request after the first deploy sends a summary at once; after
that, Mondays. A test deployment with the in-memory store sends one per
process start, since the store forgets the last one.

## Working on it

```bash
make reg-check    # tests, vet, gofmt: what CI gates on
make reg-run      # local on :8099, mails printed to the terminal, live course feed
make reg-deploy   # fly deploy from the working tree (asks first)
make reg-logs     # production logs
```

Mail templates are in `internal/mail/templates/`; their expected output is in
`internal/mail/testdata/`. After changing a template, run
`go test ./internal/mail -update` and read the diff of `testdata/`.

## Log lines

| Line | Meaning |
|---|---|
| `submit: dropped (reason)` | bot signal (origin, honeypot, rate limit, unknown code, `score: <hints>` for two or more spam signals). No mail, the person saw the success page |
| `submit: rejected (reason)` | a person's mistake (missing field, bad email, a link in comments, billing or a name). Fail page, no mail |
| `submit R-…: storing the drop failed` | a drop is missing from the summary; the person saw the success page anyway |
| `submit R-…: accepted <code> (<lang>) hints=[…]` | stored, registrant mail sent |
| `submit R-…: store write failed` | fail page shown; nothing stored, nothing sent |
| `submit R-…: registrant mail failed` | fail page shown; the row is marked `mail_failed` for the summary |
| `confirm R-…: confirmed <code>` | BESTÄTIGT/CONFIRMED sent, the one back-office mail |
| `confirm R-…: registration unreadable, short mail` | store down at confirm: the mail has id, code, name and address only |
| `confirm R-…: could not mark the row confirmed` | the mail went out; the summary run repairs the row |
| `correct R-…: store update failed` | the confirmed mail still shows the corrected address, from the token |
| `summary <week>: sent, n rows reported, …` | the weekly summary went out; how many rows were kept for next week, deleted, and reconciled |
| `summary <week>: claimed by another machine` | a second machine during a deploy; it sends nothing |
| `summary: summary <week>: …` | the run failed before the mail was out; nothing changed, the next request a minute later tries again |
| `summary <week>: sent, but not recorded` | the mail went out but the store did not take it; expect a second summary for the week after 15 minutes |
| `test mode: not sending …` | a registrant address outside `TEST_RECIPIENTS`, as intended |
