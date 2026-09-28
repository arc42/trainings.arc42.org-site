# Registration service

Receives the registration form of trainings.arc42.org, mails the back office
and the registrant through Mailjet or Brevo, and confirms a registration when
the registrant types the 6-digit code from their mail on the page that stayed
open, or presses the button reached from the link in it. The pending
registration lives inside the sealed links; the only stored state is which
registrations are confirmed and how many wrong codes each got (Turso, see
"State" in the spec), so a registration confirms exactly once.

Design: [spec](../docs/superpowers/specs/2026-09-25-registration-service-design.md),
flow diagram: [registration-flow.svg](../docs/registration-flow/registration-flow.svg).

## Endpoints

| Route | Does |
|---|---|
| `POST /submit` | the form posts here; checks, two mails, 303 to the success or fail page |
| `GET /confirm?t=…` | shows the course and one button. **Sends nothing**: corporate link scanners open every link in a mail |
| `POST /confirm` | sends the BESTÄTIGT / CONFIRMED mail, 303 to the confirmed page |
| `GET /healthz` | fly's health check |

## Configuration

| Env | Secret | Meaning |
|---|---|---|
| `MJ_APIKEY_PUBLIC`, `MJ_APIKEY_PRIVATE` | yes | Mailjet key pair (sub-account key), for `MAILER=mailjet` |
| `BREVO_API_KEY` | yes | Brevo API key (`xkeysib-…`), for `MAILER=brevo`. Open and click tracking must be off in the Brevo account: the API has no per-message switch |
| `TOKEN_KEY` | yes | 32 random bytes, base64 (`openssl rand -base64 32`). Rotating it invalidates every open confirm link |
| `BACKOFFICE_TO` | yes | where UNBESTÄTIGT/BESTÄTIGT go; several addresses **comma**-separated (a semicolon is refused at start). Personal addresses, and this repo is public |
| `TEST_RECIPIENTS` | yes | if set: **test mode**. Registrant mails only to these addresses, `[TEST]` in every subject. Personal addresses too |
| `PUBLIC_URL` | no | this service's base URL, the start of every confirm link |
| `SITE_URL` | no | where the success, fail and confirmed pages live (default trainings.arc42.org) |
| `FEED_URL` | no | the course feed (default `/api/trainings.json` on the site) |
| `ALLOWED_ORIGINS` | no | comma list of form origins; others are dropped silently |
| `MAIL_FROM`, `MAIL_FROM_NAME`, `REPLY_TO` | no | default `trainings@arc42.org`, `arc42 Trainings`, `info@arc42.de` |
| `ENVIRONMENT` | no | `PRODUCTION` or anything else |
| `TURSO_DATABASE_URL`, `TURSO_AUTH_TOKEN` | yes | the Turso database for confirmed ids and wrong-code counts (`turso db show --url`, `turso db tokens create`). Required in production; without them a test deployment keeps this in memory and forgets it on every restart |
| `MAILER` | no | `mailjet` (default), `brevo`, or `log`: print mails instead of sending |

Outside `ENVIRONMENT=PRODUCTION` the service **refuses to start** unless
`TEST_RECIPIENTS` is set (or `MAILER=log`), so a test deployment cannot mail
anyone off the list.

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
| `submit: dropped (reason)` | bot signal (origin, honeypot, rate limit, unknown code). No mail, the person saw the success page |
| `submit: rejected (reason)` | a person's mistake (missing field, bad email). Fail page, no mail |
| `submit R-…: accepted <code> (<lang>) hints=[…]` | both mails sent |
| `submit R-…: back-office mail failed` | fail page shown; nothing reached anyone |
| `submit R-…: registrant mail failed` | back office has it and follows up |
| `confirm R-…: confirmed <code>` | BESTÄTIGT/CONFIRMED sent |
| `test mode: not sending …` | a registrant address outside `TEST_RECIPIENTS`, as intended |
