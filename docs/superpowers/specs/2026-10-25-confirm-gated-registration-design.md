# Registration service: mail the back office only after confirmation

**Date:** 2026-10-25
**Status:** proposed
**Scope:** `registration-app/` (store, intake, web, mail, a new `summary`
unit and a new `sheet` unit), one job on cron-job.org, the privacy page, `_includes/registration-form.html` (one hidden field), the
registration service README and CLAUDE.md.

Supersedes parts of
[2026-09-25-registration-service-design.md](2026-09-25-registration-service-design.md):
section 4.2 (two back-office mails), section 8a (what the store holds) and
section 9 ("storing registrations" is no longer out of scope). Everything not
mentioned here stays as that spec describes it: the registrant mail with
code and link, confirming exactly once, the correction flow, the feed
contract (ADR-0004), Brevo, test mode.

---

## 1. Problem

Since the go-live (29 Sep 2026) every accepted submission sends the back
office an `UNBESTÄTIGT`/`UNCONFIRMED` mail at once. The spec wanted that: the
back office could chase a real-looking registration whose company filter had
swallowed the confirmation mail. Four weeks of operation show the other side
of the trade: spam reaches the inbox unconfirmed, and it passes every check
because it books a real course under a syntactically fine address. A sample
from 25 Oct 2026: identical first and last name (`AllenHeavy`), a
`.com.de` address, "Cusco" as the whole billing address, and a forum link
twice in the comments. The only link detector looks at the name fields.

Two further costs of mailing on submit. Each spam submit also sends a
confirmation mail to an invented address, which bounces at Brevo and lowers
the reputation of `trainings@arc42.org`. And the `CONFIRMED` mail is short
by design, because the data already sat in the first mail, so the back
office has to find and pair two mails per booking.

## 2. Decision

1. **The back office gets exactly one mail per registration, after the
   registrant confirmed.** It carries the complete registration in
   attribute/value lines, ready to be copied into the back-office
   spreadsheet.
2. **Registrations are stored in Turso** from submit until they have been
   reported, so the confirm step can render the full mail from the id and
   so attempts can be summarised. The confirm token stays as small as it is.
3. **A weekly summary mail** lists every attempt of the week: confirmed,
   pending and dropped. It replaces the "follow up on a plausible
   UNBESTÄTIGT" rule and is sent even when there is nothing to report, so
   its absence is a signal.
4. **One row per confirmed registration in a Google Sheet**, appended by
   the service, shared with Gernot Starke and Peter Hruschka. Column layout
   to be agreed; the code needs a fixed order and nothing else.
5. **Earlier spam detection.** A link in the comments or the billing
   address is rejected visibly. Weak signals are scored; two points drop a
   submission silently, one point stays a hint.

Decisions taken on 25 Oct 2026 (Gernot): weekly rather than bi-weekly;
visible rejection for links; name-field checks included; a failed registrant
mail at submit now shows the fail page. The summary is sent by the service
itself, on the first request after Monday 07:00; a cron-job.org job makes
sure there is one (decided 25 Oct 2026 after weighing a GitHub Actions
cron, cron-job.org as the trigger, and an always-on machine, section 7.3):
fewest moving parts, nothing secret outside the service, no extra bill.

## 3. Flow

```
form  --POST /submit-->  checks (4.1)  --accept-->  Turso: status=pending
                                        |             |--> Brevo --> registrant: code + link
                                        '--drop---->  Turso: status=dropped (reason), no mail
                                        '--reject-->  fail page, nothing stored

registrant  --POST /confirm | /code-->  Turso: pending -> confirmed (once)
                                        |--> Brevo  --> back office: BESTÄTIGT / CONFIRMED, full data
                                        '--> Sheets --> one row (best effort, retried by the summary run)

any request after Monday 07:00 -->      Turso: everything not yet reported
(cron-job.org GETs /healthz then)
                                        |--> Brevo  --> back office: weekly table
                                        '--> Turso: mark reported, delete what is done
```

### 3.1 Submit

Checks as before, in the same order, with the additions of section 6. Then:

- The accepted registration is **written to the store first**. If that write
  fails, the fail page is shown: the registration would otherwise exist only
  in a mail nobody receives. (Before: the back-office mail was the thing that
  had to succeed. The store takes that role.)
- The registrant mail is sent. **If it fails, the fail page is shown** and
  the stored row is marked `mail_failed` so the summary lists it. Before,
  the person saw success because the back office had the data; now nobody
  has it in a mailbox. The row stays for the summary, which is how the back
  office learns that Brevo was down.
- A **dropped** submission is stored too, with its drop reason and the
  fields that were typed, status `dropped`. It gets the same "we sent a
  mail" page as today, so a bot cannot tell a drop from an accept. Dropped
  rows exist for one summary and are deleted by it.
- A **rejected** submission (missing field, bad address, link in comments)
  is not stored; the person sees the fail page and tries again.
- The correction flow (`POST /correct`) updates the stored address and
  stores the old one. The `E-MAIL KORRIGIERT` mail to the back office goes
  away: the final mail shows "address corrected from X" instead.

### 3.2 Confirm

`POST /confirm` and `POST /code` work as today (token or code, once per id,
store-down fallback). The one change: the mail is rendered from the stored
row, not from the token.

The **CONFIRMED mail** is the UNCONFIRMED mail of today plus three lines:
when it was confirmed, how (code or link), and the corrected-from address
when there was one. Subject unchanged:
`[trainings.arc42.org] ANMELDUNG R-7F3KQ 26-12 MSA (BESTÄTIGT)`, so existing
filters keep matching. Reply-To stays the registrant. Format rules so the
mail can be copied into a spreadsheet without thought:

- one `Label: value` line per field, labels padded to one column, in the
  language of the registration as today;
- multi-line values only for the billing address and the comments, indented
  as today;
- the hints block and the heard-via line stay;
- no price (spec 2026-09-25, section 2).

If the store is down at confirm time, the mail is rendered from the token
as today (id, code, name, address) with a line "full data unavailable, see
the summary", and the row is marked when the store is back by the summary
run's reconciliation (section 4.3). Confirming must never fail because the
store is down.

Then the Sheets row (section 5), best effort.

### 3.3 Without the store at submit

Production refuses to start without Turso (unchanged). The in-memory store
used by `make reg-run` and test deployments gains the same tables, so every
path can be tested locally; it forgets on restart as today.

## 4. Store

### 4.1 Tables

```sql
CREATE TABLE IF NOT EXISTS registrations (
  id            TEXT PRIMARY KEY,         -- R-XXXXX
  created_at    INTEGER NOT NULL,         -- unix seconds
  status        TEXT NOT NULL,            -- pending | confirmed | dropped
  confirmed_at  INTEGER, confirmed_via TEXT,   -- code | link
  drop_reason   TEXT,                     -- for status dropped
  mail_failed   INTEGER NOT NULL DEFAULT 0,    -- registrant mail at submit failed
  lang TEXT NOT NULL, code TEXT NOT NULL,
  last_name TEXT, first_name TEXT, email TEXT, emails TEXT,  -- emails: parsed list, comma
  corrected_from TEXT,
  p_last TEXT, p_first TEXT, p_email TEXT,
  billing TEXT, comments TEXT, heard TEXT, heard_detail TEXT,
  via TEXT, form_source TEXT,
  hints TEXT,                             -- hint keys, comma
  sheet_at      INTEGER,                  -- row appended; NULL = still owed
  reported_at   INTEGER                   -- listed in a summary; NULL = not yet
);
CREATE TABLE IF NOT EXISTS summaries (
  week       TEXT PRIMARY KEY,             -- ISO week, e.g. 2026-W44: one summary per week, whoever sends it
  claimed_at INTEGER NOT NULL,             -- the claim (7.2); sent_at NULL means claimed, not yet sent
  sent_at    INTEGER, from_at INTEGER, to_at INTEGER, rows INTEGER
);
```

`confirmed` and `code_failures` stay as they are: `confirmed` is the
exactly-once guard and must keep working when `registrations` is missing a
row (store down at submit, confirmed later from the token). Not stored: the
client IP, the user agent, the honeypot contents. The IP is only ever used
for the in-memory rate limit.

### 4.2 Retention

Deleted by the summary run, after that run's mail was sent:

| Row | Deleted when |
|---|---|
| `dropped` | reported |
| `pending`, `mail_failed` | reported **and** older than the link lifetime (5 days). A pending row younger than that is reported again next week with its final status |
| `confirmed` | reported **and** `sheet_at` set, or reported twice (the sheet failed for two weeks, the back office has the mail) |

Nothing lives longer than about two weeks after it was typed, and a
registration that went through is in the back office's mailbox and the
sheet, not in Turso. The existing `Prune` of `confirmed` and `code_failures`
moves into the same run.

### 4.3 Reconciliation

The summary run also repairs what the confirm step could not: a `confirmed`
id without a `registrations` row of status `confirmed` is marked (the store
was down at confirm time), and rows with `sheet_at IS NULL` get their Sheets
row appended again.

## 5. Google Sheet

- One Google Cloud **service account**, Sheets API enabled, its JSON key as
  the fly secret `GOOGLE_SA_KEY`, the sheet id as `SHEET_ID`, the tab name
  as `SHEET_TAB` (default `Anmeldungen`). The sheet is shared with the
  service account's address, Gernot and Peter. Claude's access goes through
  Gernot's account via the Google Drive connector, not through a key of its
  own.
- Standard library only, as the rest of the service: build the RS256 JWT
  with `crypto/rsa` and `crypto/sha256`, exchange it at
  `oauth2.googleapis.com/token`, cache the access token for its lifetime,
  then `POST .../values/{tab}!A:A:append?valueInputOption=RAW`. About 150
  lines plus tests against a fake HTTP server.
- **Never on the critical path.** The confirm handler sends the mail, then
  appends the row with a 5-second timeout. On failure it logs and leaves
  `sheet_at` NULL; the summary run retries. The confirmed page never waits
  for Google and never fails because of it.
- Column order is a single Go slice (`columns` in `internal/sheet`).
  **Decided 30 Oct 2026** (Gernot, from the sheet the back office set up):
  one row **per participant**, columns A to L: Anrede, Name, Vorname,
  Firma, Infos nach Einladung (empty), email Teilnehmer, email Sekretariat,
  Datum der Anmeldung, Preis, in echtes Termin-Sheet übernommen von
  (empty), Kurs (booking code), Anmeldung-ID. E and J are the back office's;
  K and L sit outside the part that is copied into the date's sheet. The
  registrant is the "Sekretariat" exactly when a participant is someone
  else. Preis is the early-bird amount if the submit day is on or before
  `early_bird.until`, else the list price, stored at submit; alumni and
  special prices stay manual. The mapping rules are in
  `registration-app/README.md`, "The back-office sheet".
- For one row per participant the form collects participants as
  structured blocks (Anrede, Nachname, Vorname, E-Mail), one shown, up to
  10 added by "+ weitere Person"; every block posts the same four field
  names, which intake pairs by position. Firma is one field per
  registration. Anrede values are "Frau"/"Herr" in both languages. Stored
  as JSON in a new `participants` column (plus `company`, `price`), added
  to the live table by `ALTER TABLE` on the first request after deploy;
  `p_last`, `p_first`, `p_email` keep the first participant so a revert
  still reads it.
- Retention (4.2) for a confirmed row whose sheet append failed: kept one
  more week and retried; once reported as confirmed it is deleted, and the
  summary tells the back office to type it in from the mail.
- **A refusal is mailed at once** (Gernot, 30 Oct 2026: "if the key is no
  longer valid, we need to be informed immediately"). Any 4xx from the
  token endpoint or the Sheets API except 429, after one retry with a
  fresh token on a 401, sends `ACHTUNG: Google Sheet nicht beschreibbar`
  to `BACKOFFICE_TO`, with the remedy for that kind of refusal. At the
  first refusal, then at most once a day (in memory). Transient failures
  stay with the summary's retry.
- Unset `SHEET_ID` disables the unit entirely (test deployments, local
  runs).

## 6. Spam detection

Order inside `intake.Check`: the existing checks, then **rejections**, then
the **score**, then hints.

**Rejected visibly** (fail page with a reason the person can act on; the
fail pages get one new sentence per language, "Links are not allowed in the
comments or the billing address"):

- `http://`, `https://` or `www.` in `comments` or `billing`;
- the same in any of the four name fields (today a hint).

A real person who pasted a link learns why and resubmits. A bot that learns
to omit the link is harmless under the confirm gate.

**Scored**, two points drop silently, one point becomes a hint in the
summary and the confirmed mail:

| Signal | Points | Why |
|---|---|---|
| first name equals last name | 1 | the sample; real double names are rare and still only a hint |
| all four name fields identical | 1 | generators fill every name field from one string |
| billing address shorter than 10 characters or a single word | 1 | an invoice address has a street or a company |
| e-mail domain ends in two country/generic labels like `.com.de`, `.net.pl` | 1 | throwaway domains in the spam log; legitimate `.co.uk`, `.com.br`, `.com.au` are allow-listed |
| Gmail local part with three or more dots (existing hint) | 1 | unchanged |
| random case in a name (existing hint) | 1 | unchanged |
| the hidden `js_ok` field is empty | 1 | see below |

**`js_ok`**: a new hidden input in `registration-form.html` that the page's
script fills on load. Form bots that post without running the page's script
leave it empty. The course list itself is static Liquid output and the form
submits fine without script, so a person with scripts off can register
today and must still be able to: the empty field is **one** point, never a
drop on its own, and only tips a submission that already carries another
signal. The validator's field map in `intake.go` gets the name; nothing
else reads it.

Drops are recorded with the reason, so the weekly table shows which rule
fired and how often. If a rule drops a real person, that shows up there too,
with a typed address to write to. The 3-per-address and 5-per-IP limits
stay. Friendly Captcha remains in reserve as before.

## 7. Summary run

### 7.1 When

The service sends the summary itself, **from inside an ordinary request**:
every handler, `/healthz` included, first asks the store for the last
`summaries` row and sends when the most recent **Monday 07:00
Europe/Berlin** lies after it. The send runs in the background so the
request that triggered it is not slowed down.

The machine suspends when idle (`fly.toml`, unchanged), so there has to be a
request on Monday morning. A **cron-job.org job** (arc42 already uses the
service) does `GET https://register.arc42.org/healthz` every Monday at
07:00 Europe/Berlin, failure notification on. The GET resumes the machine,
`/healthz` runs the due-check, the summary goes out, and fly's own health
checks keep the machine awake for the minute the send needs. Nothing about
this job is secret: the URL is public and carries no token, and the
response says `ok` either way.

If the ping does not come, nothing is lost: the next real submit or confirm
sends the overdue summary, with a note that it is late. The ping decides
*when*, never *whether*.

### 7.2 What it does

1. Load the last `summaries` row. Not due: do nothing. (A redeploy or a
   restart during the hour therefore cannot send twice: the decision is
   made from the store, not from process memory.)
2. **Claim the week**: insert the `summaries` row for this ISO week with
   `sent_at` NULL. The primary key makes a second claimant fail, so two
   machines during a deploy cannot both send. A claim older than 15 minutes
   with `sent_at` still NULL is stale (the process died mid-send) and may be
   taken over.
3. Select every row with `reported_at IS NULL`, plus pending rows reported
   once but not yet final. Reconcile (4.3).
4. Render and send the mail. **A failed send deletes the claim and changes
   nothing else**; the next request's due-check tries again, so a Brevo
   hiccup costs minutes, not a week.
5. Set `sent_at`, set `reported_at`, apply the retention rules, prune
   `confirmed`/`code_failures`. If this step fails after the mail went out,
   next week's table repeats some rows; a duplicate line is the cheaper
   failure.
6. One log line: rows reported, rows deleted.

The mail: subject
`[trainings.arc42.org] Wochenübersicht Anmeldungen KW 44 (3 bestätigt, 1 offen, 12 verworfen)`.
German only: it goes to the back office, not to registrants. Body: three
counts, then one plain-text table per status, columns id, time, course,
last name, first name, e-mail, billing (first line), comments (first 80
characters, `http` replaced by `hxxp` so no client turns it into a link),
hints or drop reason. **Sent also when every count is zero**, body "keine
Anmeldeversuche": the mail is the heartbeat (7.4). Recipients
`BACKOFFICE_TO`.

### 7.3 Why no external scheduler

Three ways were weighed on 25 Oct 2026:

| Trigger | Adds to the failure surface | Would have given |
|---|---|---|
| GitHub Actions cron calling `POST /summary` | the schedule (silently disabled after 60 days without a commit), a token in two places, a workflow nobody watches, an issue-opening step so that someone does | a versioned schedule |
| cron-job.org calling `POST /summary` | the token in a third-party dashboard, an endpoint that does something, a 30-second call timeout, auto-disable after 25 failures | failure and recovery mails, an execution history |
| an always-on machine with an in-process timer | nothing in the failure chain, but about 2 USD a month for `min_machines_running = 1` | no external party at all |
| **due-check in every request, cron-job.org pings `/healthz`** | cron-job.org, but only for timing: a missed ping delays the summary to the next real request, nothing else | zero cost, no token, no new endpoint, failure mail from cron-job.org when the service is unreachable |

The last row wins: the only external party holds nothing secret and can
only make the summary late, never wrong or missing. The always-on machine
was the runner-up and is the fallback if cron-job.org ever goes away: one
line in `fly.toml` and a `time.Ticker`.

### 7.4 Detecting a missing summary

| Failure | Visible as | Covered by |
|---|---|---|
| cron-job.org job disabled, deleted, or its account gone | summary arrives with the next real registration, marked late; in a quiet week no Monday mail | the **heartbeat**: a Monday without a summary is the alarm. Re-create the job (one minute, nothing secret) |
| service unreachable at 07:00 | failure mail from cron-job.org; summary with the next request | fly restarts a crashed process; the next request sends |
| Brevo or Turso down at 07:00 | a late mail | fly's health checks hit `/healthz` every 30 s while the machine is awake, so the due-check retries for as long as the machine stays up, and the next request after that |
| mail sent, marking failed | duplicate rows next week | accepted |
| two machines running (a deploy in progress) | one mail | the week claim (7.2 step 2): the second insert fails and that machine sends nothing |

**Heartbeat.** The summary is sent even when there is nothing to report, so
"no mail on Monday" means "the service did not run", whatever the cause.
The back office reads this mail anyway; nobody has to watch a dashboard.

**Dead-man's switch, optional, not in the first round.** If a missing
Monday mail proves easy to overlook: healthchecks.io (SIA Monkey See Monkey
Do, Latvia; free for 20 checks) gives a URL that the service `GET`s right
after a successful send, as the last step. The check is configured with a
period of 7 days and a grace of 1 day; it mails when no ping arrives in
time and again when pings resume. One env var (`HEARTBEAT_URL`), one HTTP
call, no endpoint on our side, independent of fly and of this process.
This is simpler than the same thing built on cron-job.org, which would need
a status endpoint here for cron-job.org to poll. The pattern: the job
reports success, an independent watcher alerts on silence; it detects
absence, which a failure notification cannot.

## 8. Configuration

| Env | Secret | Meaning |
|---|---|---|
| `GOOGLE_SA_KEY` | yes | service account JSON; unset disables the sheet |
| `SHEET_ID`, `SHEET_TAB` | no | the sheet and tab; `SHEET_ID` unset disables the sheet |
| `HEARTBEAT_URL` | yes | optional, section 7.4 |
| `SUMMARY_NOW` | no | `1` sends a summary at start regardless of the week, for test deployments; refused in production |

Everything else unchanged; `fly.toml` is not touched. Test mode: the summary and the confirmed mail go to
`BACKOFFICE_TO` as every back-office mail does; the sheet is off unless a
test sheet is configured. A test deployment with the in-memory store sends
a summary at most once per process lifetime, since the store forgets.

## 9. Privacy

New facts for `_pages/imprint-privacy.md`, both languages: registration data
is stored by the service on Turso (hosted libSQL; **the database location
must be an EU region, to be verified with `turso db show`** before
production use) for at most about two weeks, until it has been reported to
the back office; confirmed registrations are additionally written to a
Google Sheet (Google Ireland Ltd.) that the back office uses as its record.
Legal basis as for the registration itself: performance of the contract,
and legitimate interest for the attempt log, whose purpose is abuse
detection. The weekly table contains personal data of people who never
confirmed; it is retained in the back-office mailbox like the registration
mails are today, which is an existing practice, not a new one.

## 10. Testing and rollout

Four PRs, in this order, each on its own branch and each reversible alone.
`make reg-check` gates each.

1. **Spam rules** (`intake`, the form's `js_ok` field, fail-page sentence,
   validator field list). Immediate relief, no storage involved. Tests: a
   table of submissions with expected outcome and points, the sample from
   section 1 among them. Observation: a week of logs, looking for a
   dropped real person.
2. **Store and confirm-gated mail** (`store`, `web`, `mail`, memory store,
   spec and README, privacy page). The UNCONFIRMED mail and the CORRECTED
   mail go; their templates and golden files are deleted, the CONFIRMED
   templates grow. Tests: handler tests for store-write-fails,
   registrant-mail-fails, store-down-at-confirm; golden files for the new
   mail. Test deployment first with `TEST_RECIPIENTS`, then production.
   Back office briefed: from this deploy on, no more UNBESTÄTIGT mails.
3. **Summary** (`summary` unit, due-check on every request, retention).
   Tests with a fake clock: due exactly once per week, not due after a
   restart in the same week, send-fails-changes-nothing, retention table,
   week claim. First run forced on the test deployment by setting the
   clock-independent `SUMMARY_NOW=1` env var once. Then, by hand and
   tracked in Todoist: the cron-job.org job, `GET
   https://register.arc42.org/healthz`, every Monday 07:00 Europe/Berlin,
   failure notification to the back office address.
4. **Sheet** (`sheet` unit, confirm hook, reconciliation), once the column
   layout is agreed. Tests against a fake token and Sheets server; one
   manual end-to-end into a test sheet.

Rollback per PR: revert it. Reverting PR 2 brings back the two-mail flow;
the `registrations` table can stay, unused.

## 11. Explicitly out of scope

- Reminder mails to unconfirmed registrants. The state would allow it, but
  most pending addresses are invented and the mails would bounce.
- A challenge widget. Friendly Captcha stays in reserve, as before.
- An admin UI over the stored registrations. The weekly mail and the sheet
  are the views.
- Automatic status changes on dates (seats, waiting list). Unchanged.

## 12. Open points

- Turso region check (section 9).
- ~~Column layout of the sheet (section 5)~~ decided 30 Oct 2026.
- Whether the back office wants the weekly mail in German only or in both
  languages. Proposed: German.
