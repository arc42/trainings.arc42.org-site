# Registration service

Receives the registration form of trainings.arc42.org, stores it in Turso,
mails the registrant through Brevo, and confirms a registration when the
registrant types the 6-digit code from their mail on the page that stayed
open, or presses the button reached from the link in it. Only then does the
back office get its one mail, BESTÄTIGT/CONFIRMED, with the complete
registration, and its rows in the back-office Google Sheet, one per
participant (below). Turso also records which registrations are confirmed and how
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
| `POST /brevo/events?t=…` | Brevo's delivery events: a mail that did not arrive (below). Exists only when `BREVO_WEBHOOK_TOKEN` is set |

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
| `GOOGLE_SA_KEY` | yes | the JSON key of the Google service account that writes the sheet, as Google hands it out. Goes with `SHEET_ID`; one without the other, or a key that cannot be read, starts the service **without** the sheet and mails the back office (nothing about the sheet ever stops registrations) |
| `SHEET_ID` | yes | the spreadsheet id from its URL (`/spreadsheets/d/<id>/edit`). Unset: no sheet, as in test deployments and `make reg-run` |
| `SHEET_TAB` | no | the tab rows go to (default `Tabelle1`) |
| `BREVO_WEBHOOK_TOKEN` | yes | the secret in the URL of Brevo's webhook, 32 characters or more (`openssl rand -hex 24`). Unset: no webhook route |

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

## The back-office sheet

Every confirmed registration is appended to the Google Sheet
"arc42-neue-Anmeldungen" (`internal/sheet`), **one row per participant**; the
back office copies each new row into the sheet of its course date. Columns,
as agreed on 30 Oct 2026 (the list is `columns` in `internal/sheet/sheet.go`,
the one place to change when the sheet changes):

| | Column | Filled with |
|---|---|---|
| A | Anrede | the participant's (Frau, Herr or empty) |
| B, C | Name, Vorname | the participant's; the registrant's when no participant was entered |
| D | Firma | the one Firma field of the registration, in every row |
| E | Infos nach Einladung | empty: the back office's |
| F | email Teilnehmer | the participant's; the registrant's when they attend |
| G | email Sekretariat | the registrant's, when they registered someone else; else empty |
| H | Datum der Anmeldung | the submit day, `DD.MM.YYYY`, Berlin |
| I | Preis | a number: the early-bird price if submitted by its last day, else the list price; empty for "Sonstige". Alumni and special prices are the back office's |
| J | in echtes Termin-Sheet übernommen von | empty: the back office's |
| K | Kurs | the booking code |
| L | Anmeldung-ID | `R-…`, the id in the BESTÄTIGT mail's subject |

The append runs in the background after the BESTÄTIGT mail; the page never
waits for Google. If it fails (or the store was down at confirm time), the
weekly summary run appends it, and the summary says so; after a second
failed week the summary asks the back office to type the rows in from the
mail, and the registration is deleted as usual. A row can end up in the
sheet twice (Google took it, the service did not hear back); the id in L
shows it.

**When Google refuses, the back office hears at once.** A refusal no retry
will fix (the key deleted, revoked or disabled; the sheet no longer shared
with the service account, deleted, or its tab renamed: any 4xx except 429,
after one retry with a fresh token on a 401) sends the mail `ACHTUNG:
Google Sheet nicht beschreibbar` to `BACKOFFICE_TO`, with Google's answer
and the steps that fix that kind of refusal, a new key among them
(`internal/mail/templates/sheet_alert_de.txt`). Immediately at the first
refusal, then at most once a day while it lasts; the pause is kept in
memory, so a restart may send one more. Network errors, timeouts and 5xx
send nothing: the summary run retries them, and lists what is missing.

**Nothing about the sheet stops the service.** A missing or unreadable key
starts it without the sheet, with the same ACHTUNG mail; every refusal is
an ordinary error; and a panic anywhere in the sheet code is contained
(`sheet.Contain`, and a `recover` in the confirm hook and the summary run),
because a panic in a goroutine would end the whole process.

Testing against the real Google (opt-in, skipped by `make reg-check`):
`SHEET_LIVE=1 go test ./internal/sheet -run Live -v` checks that a forged
key is refused as a dead key is (no secret needed); with `GOOGLE_SA_KEY`
and `SHEET_ID` set as well it appends one test registration (two rows, R-TEST1,
marked TEST) to the real sheet and checks the refusals for a missing tab and
a missing spreadsheet. Delete the test rows afterwards.

Setup: a Google Cloud project with the Sheets API enabled, a service account
without roles, a JSON key for it (`fly secrets set GOOGLE_SA_KEY="$(cat
key.json)" SHEET_ID=…`), and the spreadsheet shared with the account's
address as **editor**. The service logs that address at start (`sheet: tab
"Tabelle1" as …`).

## Mails that did not arrive

Brevo accepts a mail with 201 and only learns afterwards whether the
receiving server took it. On 30 Oct 2026 mailbox.org refused a confirmation
mail because Brevo's shared sending IP was on the SpamCop blocklist (`554
5.7.1 … blocked by RBL`); the service had logged `accepted`. A Brevo
webhook now reports such failures back (spec
`docs/superpowers/specs/2026-10-30-bounce-webhook-design.md`):

- **Registrant mail not delivered** (`hard_bounce`, `soft_bounce`,
  `blocked`, `invalid_email`): the registration row is marked, a row goes to
  the sheet's tab **Zustellung**, and the weekly summary shows the
  registration as NICHT ZUGESTELLT. A later `delivered` for the same
  address clears it; a corrected address makes it moot.
- **Back-office mail not delivered** (BESTÄTIGT, the summary, the sheet
  alert): a row in Zustellung, nothing else. The back office sits behind
  the same kind of filter, so an alert mail through Brevo would be lost
  the same way.

Nobody is told by a mail through Brevo. The back office learns of a new
Zustellung row from **Google's notification rule** on the spreadsheet,
which Google sends from its own servers. The service sends nothing again
automatically: Brevo picks the IP, and a resend may meet the same block.
Write to the person from info@arc42.de, which mailbox.org sends itself.

Setup, once:

1. In the spreadsheet, a tab named exactly `Zustellung`, header row A to I:
   `Zeit, Typ, Anmeldung-ID, Kurs, Name, Vorname, E-Mail, Ereignis, Grund`.
   A missing tab is refused by Google and mailed as `ACHTUNG: Google Sheet
   nicht beschreibbar`.
2. Each person who wants to hear of it: Tools, Notification settings, Edit
   notifications, "Any changes are made", "Email – right away". It covers
   the whole file, so a confirmed registration on the main tab notifies too.
3. `fly secrets set -a arc42-registration BREVO_WEBHOOK_TOKEN=$(openssl rand -hex 24)`,
   and keep the value for the next step.
4. Brevo, Transactional, Settings, Webhooks, add a webhook: URL
   `https://register.arc42.org/brevo/events?t=<token>`, events delivered,
   soft bounce, hard bounce, blocked, invalid email. Nothing else: opens and
   clicks would only add personal data to the log.

Answers: `401` wrong token; `400` unreadable body; `500` the store could
not take it (Brevo may retry); `200` for everything else, ignored events
included. The sheet row is appended after the answer; Google down costs a
log line, and the summary still lists the registration.

## Working on it

```bash
make reg-check    # tests, vet, gofmt: what CI gates on
make reg-run      # local on :8099, mails printed to the terminal, live course feed
make reg-deploy   # fly deploy from the working tree (asks first)
make reg-logs     # production logs
make show-registrations  # what Turso holds now, read-only (needs `turso auth login`)
```

`show-registrations` lists pending and confirmed registrations, newest
first, times in UTC: the week since the last summary, plus the open ones
that are kept. It is personal data; do not paste the output anywhere.

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
| `confirm R-…: n row(s) in the sheet` | appended to the back-office sheet |
| `confirm R-…: sheet append failed, the summary run retries` | Google unreachable or refusing (403: the sheet is not shared with the service account) |
| `confirm R-…: in the sheet, but not recorded` | the next summary run appends it again: expect a duplicate row |
| `sheet: sheet: token refused: 400 …; alert mailed to the back office` | Google refused the key (or `append refused: 403/404/400`: the sheet); the ACHTUNG mail went out |
| `sheet: … (alert already mailed at …)` | the same refusal again within a day; no second mail |
| `sheet: …; the alert mail failed too` | Brevo is down as well; the next refusal tries the mail again |
| `summary <week>: sheet append R-… failed, stopping here` | the rest is retried next week; the summary lists what is missing |
| `correct R-…: store update failed` | the confirmed mail still shows the corrected address, from the token |
| `brevo events: registrant mail R-… not delivered: <event>: <answer>` | marked; a row went to Zustellung (or a `Zustellung row … failed` line follows) |
| `brevo events: back-office mail … to … not delivered` | a back-office mail was refused; a row went to Zustellung |
| `brevo events: … at a former address, ignored` / `… no longer holds, ignored` | a bounce for a corrected address, or for a row the summary has deleted |
| `brevo events: wrong or missing token` | someone, or a Brevo webhook with an old token, called the route |
| `summary <week>: sent, n rows reported, …` | the weekly summary went out; how many rows were kept for next week, deleted, reconciled, and appended to the sheet |
| `summary <week>: claimed by another machine` | a second machine during a deploy; it sends nothing |
| `summary: summary <week>: …` | the run failed before the mail was out; nothing changed, the next request a minute later tries again |
| `summary <week>: sent, but not recorded` | the mail went out but the store did not take it; expect a second summary for the week after 15 minutes |
| `test mode: not sending …` | a registrant address outside `TEST_RECIPIENTS`, as intended |
