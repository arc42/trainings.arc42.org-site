# Registration service: learn when Brevo could not deliver a mail

**Date:** 2026-10-30
**Status:** implemented and deployed (30 Oct 2026); token header added 31 Oct 2026
**Scope:** `registration-app/` (a new `POST /brevo/events` handler in `web`,
two columns in `store`, a second tab in `sheet`, one line per row in the
summary), one webhook in the Brevo account, one notification rule on the
back-office sheet, the registration service README.

Builds on [2026-10-25-confirm-gated-registration-design.md](2026-10-25-confirm-gated-registration-design.md).
Nothing there changes; this adds a path for news that arrives after the
send.

---

## 1. Problem

On 30 Oct 2026 a confirmation mail to a mailbox.org address was rejected:

```
554 5.7.1 Service unavailable; Client host [77.32.148.25] blocked by RBL;
Blocked - see https://www.spamcop.net/bl.shtml?77.32.148.25
```

77.32.148.25 is in Brevo's shared sending pool and was on the SpamCop
blocklist. Brevo had accepted the mail with 201, so the service logged
`accepted` and showed the "check your e-mail" page. The rejection exists
only in Brevo's log. The registrant waits for a mail that never comes, and
the row sits as `pending` until the weekly summary lists it as an
unconfirmed attempt, indistinguishable from spam to an invented address.

Since PR #74 the page tells the person to write to info@arc42.de after 15
minutes. That relies on the person acting. This spec makes the service
know.

The same pool sends the **back office's** mails: BESTÄTIGT, the weekly
summary, the Sheet alert. `arc42.org` receives at mailbox.org, which checks
SpamCop, so a blocked IP can take those out too. A missing BESTÄTIGT mail
is covered by the sheet; a missing summary looks like the heartbeat failing
(spec 2026-10-25, 7.4) and would be investigated in the wrong place.

## 2. Decision

1. **Brevo reports delivery events to the service** through a transactional
   webhook, `POST https://register.arc42.org/brevo/events`.
2. **A failed delivery to a registrant** is stored on the registration row
   and **appended to a second tab of the back-office sheet**, "Zustellung".
   The back office sets a Google Sheets notification rule on that sheet, so
   Google mails them at once, through Google's servers, not Brevo's.
3. **A failed delivery to the back office** is appended to the same tab.
   That is the one channel for that case that does not depend on the pool
   that just failed.
4. **The weekly summary** gets a section "Nicht zugestellt" with the
   affected registrations.
5. **No alert mail through Brevo.** It would go to `BACKOFFICE_TO` through
   the same pool and be blocked exactly when it matters.
6. **No automatic resend.** Brevo picks the IP; a resend may hit the same
   one, and a person writing in after the page's hint must not then get two
   mails. The back office answers by hand from info@arc42.de, which goes
   through mailbox.org's own servers.

## 3. What Brevo sends

Brevo posts one JSON object per event (an array only if "batched" is
switched on, which it is not; the handler reads both). The fields this
design reads, as documented at
developers.brevo.com/docs/transactional-webhooks (read 30 Oct 2026; a real
event is still to be seen, section 9):

| Field | Use |
|---|---|
| `event` | which events count, below |
| `email` | the recipient |
| `tags` (array), in some events `tag` (a string, once holding a JSON array) | our `CustomID`: the registration id (`R-…`), `summary-<week>` or `sheet-alert`. Both read leniently |
| `ts_event` | when it happened, unix seconds |
| `reason` | the remote server's answer, for the sheet |
| `message-id` | for looking it up in Brevo |

Events (subscribed in Brevo as `delivered`, `softBounce`, `hardBounce`,
`blocked`, `invalid`; the payload names differ):

| Event | Meaning | Effect |
|---|---|---|
| `hard_bounce` | permanent refusal (unknown user, domain) | failed |
| `soft_bounce` | refusal Brevo files as temporary; the RBL case above was one | failed |
| `blocked` | Brevo did not send: the address is on its blocklist | failed |
| `invalid_email` | address syntactically unusable at Brevo | failed |
| `error` | Brevo-side failure; **not offered** for transactional webhooks, handled in case it comes | failed |
| `delivered` | the remote server accepted it | clears a failure recorded **earlier** for the same recipient and id |

`deferred` is not subscribed: Brevo is still retrying, and a later
`delivered` or bounce settles it. Opens and clicks are not subscribed: the
service has no use for them, and they would add personal data to the log.

`soft_bounce` counts as failed because the one case we saw was permanent
(`554`) and Brevo did not retry. If Brevo does retry a soft bounce and it
then succeeds, the `delivered` event clears it, so the worst case is a
sheet row that turns out to be a false alarm.

## 4. The handler

`POST /brevo/events`:

1. **Authentication.** A token in the URL, `?t=<token>`: Brevo's docs
   mention neither basic auth nor a bearer token for the receiving end
   (the create-webhook API has an undocumented `auth` object and custom
   headers), and a URL works in the API. **Amended 31 Oct 2026:** the UI
   form refuses a URL with a query string and offers "Token"
   authentication, sent as an `Authorization` header; the handler reads
   the token from that header (with or without `Bearer `) when present,
   otherwise from `?t=`. The token is the
   fly secret `BREVO_WEBHOOK_TOKEN`, at least 32 characters, compared in
   constant time. Wrong or missing: `401`, one log line. Unset secret: the
   route does not exist, so a deployment without the webhook has no open
   endpoint.
2. **Body** at most 64 KB, one event or an array of events (Brevo has sent
   both shapes over time; accept both, cheaply).
3. Per event:
   - **Tag is a registration id and the recipient is that registration's
     current address**: on a failure event, set `bounced_at` and
     `bounce_reason` on the row and append to the "Zustellung" tab; on
     `delivered` with `ts_event` after `bounced_at`, clear both. A bounce
     for an address the person has since corrected is ignored: the
     correction already fixed it.
   - **Recipient is in `BACKOFFICE_TO`**, any tag: failure events append to
     the tab with type "Back-Office". Not stored in Turso: there is no row
     to put it on, and the sheet is the record.
   - **Anything else** (unknown id, an id already deleted by retention,
     mails from other Brevo senders on the account): ignored, one log line.
4. Answer `200` once the store write is done. A store failure answers `500`
   so Brevo retries. The sheet append is best effort with the existing
   5-second timeout, as at confirm (spec 2026-10-25, 5): a failure is
   logged, and the weekly summary still lists the row.

The handler runs the summary due-check like every route does. A webhook
call resumes a suspended machine; nothing about fly changes.

## 5. Store

Two columns on `registrations`, added by the existing `ALTER TABLE`
mechanism on the first request after deploy:

```sql
bounced_at    INTEGER,   -- last delivery failure of the registrant mail; NULL = none known
bounce_reason TEXT       -- the event and the remote answer, e.g. "soft_bounce: 554 5.7.1 … blocked by RBL"
```

`Corrected` clears both: the next mail goes to a different address.
Retention is unchanged (spec 2026-10-25, 4.2): a bounced row is `pending`
and goes when pending rows go.

## 6. Sheet: the "Zustellung" tab

A second tab in the same spreadsheet, created by hand once, name fixed in
code (`Zustellung`), same service account, same append code. One row per
failure event:

| A | B | C | D | E | F | G | H | I |
|---|---|---|---|---|---|---|---|---|
| Zeit | Typ (Anmeldung / Back-Office) | Anmeldung-ID | Kurs | Name | Vorname | E-Mail | Ereignis | Grund |

Name and Vorname are two columns, as on the main tab (Gernot, 30 Oct 2026).
Back-office rows leave C to F empty when the tag is not a registration id.

No rows on `delivered`: the tab is a list of things to act on, and a row
that only says "never mind" is noise. A false alarm is resolved by the back
office finding the person registered.

A missing tab makes Google answer `400`, which the existing Sheet alert
(spec 2026-10-25, 5) mails as "Google Sheet nicht beschreibbar", in a
variant that names the Zustellung tab and its header row. That alert goes
through Brevo; it is the setup mistake it catches, not the outage.

**Notification rule** (by hand, once, by each back-office person who wants
it; it needs edit access): Google Sheets, Tools → Notification settings →
Edit notifications → "Any changes are made", "Email – right away". Decided
30 Oct 2026 (Gernot): every change, not the daily digest. Google sends it
from its own servers, a few minutes after the change, several changes
batched into one mail that links to the file rather than quoting the row.
The rule covers the whole file, so it fires on the main tab too; that is a
confirmed registration, which they want to know about as well.

**To be verified, not assumed:** that a change made by the service account
through the API triggers the rule. It is another account than the
recipient's, which is what the rule watches, but this has not been tried.
The end-to-end test (section 9) checks it; if Google stays silent, this
section needs another channel before the webhook goes live.

## 7. Weekly summary

A bounced registration stays in its status table and gets one more line,
`Zustellung: NICHT ZUGESTELLT, <reason, first 100 characters>`, with the
advice to write from info@arc42.de, the way a failed submit mail is shown
today. Below the counts, a line "Nicht zugestellt: n" when n > 0, and the
subject's counts gain ", n nicht zugestellt". (Built this way rather than
as a separate section: the row already carries everything the back office
needs, and it is where they look for the person.) The reason is defused
like a comment: SpamCop's answer quotes a URL.

## 8. Configuration

| Env | Secret | Meaning |
|---|---|---|
| `BREVO_WEBHOOK_TOKEN` | yes | the token in the webhook URL, 32 characters or more; unset disables the route |

In Brevo, by hand, documented in the README: Transactional → Settings →
Webhooks → new webhook, URL
`https://register.arc42.org/brevo/events`, authentication "Token" with
the token, the events of
section 3. Test deployments
leave the token unset.

What this adds to the failure surface: one route and one setting in Brevo.
If either breaks, the service is where it is today, no worse; the page's
"write to us" hint (PR #74) stays the fallback for the registrant. Cost:
none, webhooks are included in every Brevo plan.

## 9. Testing and rollout

One PR. `make reg-check` gates it.

- The parser follows Brevo's documented examples and reads the tag
  leniently (`tags` array, `tag` string, `tag` string holding an array).
  **After deploy**, the first real event is checked in the log against
  that: if a bounce of a known registration logs "without a registration
  id", the tag arrives in a shape the parser does not know.
- Handler tests with those bodies: failure marks the row and appends;
  `delivered` after a failure clears it, before does not; a bounce for a
  corrected address is ignored; a back-office recipient appends without a
  store write; unknown id ignored; wrong token `401`; unset token `404`;
  store failure `500`; sheet failure still `200`.
- Summary golden file with one bounced row.
- End to end after deploy: register with an address that hard-bounces
  (a non-existent mailbox at arc42.org), see the row in Turso, the line in
  "Zustellung" and Google's notification mail. The notification is the
  point of the test: it proves that API changes trigger the rule (section 6).

Rollback: revert the PR and delete the webhook in Brevo. The two columns
can stay, unused.

## 10. Explicitly out of scope

- A dedicated sending IP: a recurring cost, and with a few mails a week the
  IP would have no reputation, which filters punish more than a shared one.
- A second mail provider as failover: a second account, DNS records and API
  for an occasional outage.
- Telling the registrant automatically: the only channel is the one that
  just failed.

## 11. Open points

- Brevo's payload shape, from the first real event (section 9).
- Whether Google's notification rule fires for changes by the service
  account (section 6).
- ~~Notification on every change or a daily digest~~ decided 30 Oct 2026:
  every change.
