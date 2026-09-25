# Registration service: specific confirmations and a confirm click

**Date:** 2026-09-25
**Status:** design, awaiting review
**Scope:** a new Go service `registration-app/` (fly.io app `arc42-registration`),
`_includes/registration-form.html`, the registration and success pages, a
hidden test page, and later the removal of Formspark and Botpoison from this
repo.

Normative context: [ADR-0004](https://github.com/arc42/meta.arc42.org/blob/main/adr/0004-trainings-feed-is-a-contract.md)
(the feed is a contract). This service becomes a consumer of
`/api/trainings.json` and is bound by it like every other consumer.

---

## 1. Problem

Since arc42.de stopped linking its own form, every registration is submitted
on trainings.arc42.org, to Formspark. Two things are wrong with that.

**The confirmation mail says nothing.** Formspark allows one autoresponder
template per form and cannot select one per submission, which is the only
reason the site runs two Formspark forms (DE `AIKiYyJP`, EN `Tq1M7LqmX`). The
template cannot look up what was booked, so it cannot say "MSA, 1 to 4 December,
Munich, 2.890 €". Its "recap of the submitted data" is worse than nothing: it
mails text typed by a stranger to an address typed by the same stranger, from
arc42's name. The spam log already shows the pattern bots use to probe for
exactly that (a dotted Gmail address, `a.b.c.d.e1@gmail.com`).

**The spam that matters is plausible.** About one submission a week has a real
course selected and a believable person, so someone has to judge whether it is
real before acting on it. Botpoison is a proof-of-work challenge and was
beaten twice. Every alternative Formspark supports (Turnstile, hCaptcha,
reCAPTCHA v2) asks the same question, "is this a script?", and a plausible
fake is either typed by a person or made by a script that already passes. None
of them is hosted in the EU.

Checked and rejected: staying on Formspark and swapping the challenge. It
tidies the bot side and delivers neither goal.

## 2. Decision

A small stateless Go service receives the form instead of Formspark, and a
registration is only confirmed once the registrant clicks a button reached
from the confirmation mail.

- The **registrant** gets a mail in the form's language that states course,
  dates, location, trainers, the price that applies on the day of
  registration and the booking code, all looked up from the feed, and a link
  to confirm.
- The **back office** keeps receiving an email, as today, in two stages:
  `UNBESTÄTIGT` at once, `BESTÄTIGT` when the registrant confirms. Rule for the
  back office: book on BESTÄTIGT; after a few days, follow up on an
  UNBESTÄTIGT that looks like a real company. Nothing is lost when a corporate
  filter swallows the confirmation mail, and a fake stays visibly unconfirmed.
- Mail is sent **as `trainings@arc42.org` through Mailjet**, the provider
  arc42.org's DNS already authorizes (SPF `include:spf.mailjet.com`, DKIM at
  `mailjet._domainkey.arc42.org`). Mailjet is Sinch, processing in the EU. No
  subdomain is involved. Replies go to `info@arc42.de`, because arc42.org
  cannot receive mail (its MX `mail.arc42.org` does not resolve).
- **One form** for both languages. The hidden `language` field, which the form
  already submits, selects the mail language. The two Formspark ids and the
  two Botpoison keys disappear, and with them the "one form per language"
  contract in CLAUDE.md and README.

## 3. Components

```
trainings.arc42.org (GitHub Pages)          arc42-registration (fly.io, ams)
  /anmeldung/, /registration/   --POST-->   /submit
                                             |  checks, seal token
                                             |--> Mailjet --> back office: UNBESTÄTIGT
                                             |--> Mailjet --> registrant: details + link
  /anmeldung-erfolg/            <--303----   '
  (registrant clicks link)      --GET--->   /confirm?t=...   page with a button
                                --POST-->   /confirm         --> back office: BESTÄTIGT
  /anmeldung-bestaetigt/        <--303----
  /api/trainings.json           <--GET----  feed cache (bookable codes, course facts)
```

**`registration-app/`**, a separate Go module next to `admin-app/`, standard
library only. It is deliberately **not** part of the admin app: the admin app
sits behind GitHub sign-in and holds a token that can open pull requests on
this repository, and an endpoint anyone on the internet can POST to must not
share a process with that token. The service needs none of the admin app's
code; it reads the public JSON feed, not the YAML.

Units, each testable alone:

| Unit | Job | Depends on |
|---|---|---|
| `feed` | fetch and cache `/api/trainings.json`, answer "is this code bookable today?" and "what are its facts?" | HTTP, clock |
| `intake` | parse and check a submission, return a `Registration` or a reason to drop it | `feed`, rate limiter |
| `token` | seal and open a confirmation token (AES-256-GCM, expiry) | key from env |
| `mail` | render the three mails (DE/EN registrant, back office UNBESTÄTIGT and BESTÄTIGT) from templates | `feed` facts |
| `mailjet` | send one message via Mailjet Send API v3.1, sandbox switch, allow-list in test mode | HTTP |
| `web` | the four handlers and the confirm page | all of the above |

The price and date formatting rules of `_includes/money.html`,
`price-label.html` and `training-date-label.html` are re-implemented in Go
(`2.890 €` / `€2,890`, German month names). A golden test renders the same
dates through both and compares, so the site and the mail cannot drift apart.

## 4. Flow in detail

### 4.1 Submit (`POST /submit`)

Checks, in this order. The first failure decides.

| Check | On failure |
|---|---|
| body at most 32 KB, each field within its limit (comments 4000 chars) | 413 / fail page |
| `Origin`, when present, is `https://trainings.arc42.org` (plus the test origins in test mode) | silent drop |
| honeypots `_gotcha` and `company_website` empty | silent drop |
| rate limit: 5 submissions per IP per hour, in memory | silent drop |
| required fields present, email syntactically valid | redirect to fail page |
| `Kurs` is a currently bookable code, or `sonstige` | silent drop |

"Silent drop" means: log the reason, send no mail, answer with the normal
redirect to the success page. A bot learns nothing about which check it
tripped. A real person cannot hit these checks except through a stale page
whose date closed in the meantime; the `UNBESTÄTIGT` path would lose that
registration, so a code that exists in the feed but is no longer bookable is
**not** dropped: it goes through with a warning line in the back-office mail.
Only codes that never existed are dropped.
(A date deleted from `trainings.yml` while someone has the form open also
counts as never existed. That is rare enough to accept: the fail-safe for it
is the person noticing no confirmation mail arrived.)

The rate limiter lives in memory and resets when the machine stops. That is
acceptable: it exists to stop a burst, not to keep history.

**Suspicion hints.** Some signals are too weak to block but worth a glance:
three or more dots in a Gmail local part, a URL in the name fields, random
mixed-case strings. They are listed in the back-office mail under `Hinweise`,
never used to drop.

### 4.2 Mails on submit

1. Generate a registration id, `R-` plus 5 characters from an unambiguous
   alphabet (e.g. `R-7F3KQ`), for the back office to match the two mails.
2. **Back office first**, `UNBESTÄTIGT`. Subject
   `[trainings.arc42.org] ANMELDUNG R-7F3KQ 26-12 MSA (UNBESTÄTIGT)`. Body: every
   submitted field under fixed German labels regardless of form language, plus
   `Sprache`, `via`, `form_source`, the applicable price, and `Hinweise`.
   Reply-To is the registrant, so answering the mail reaches them.
3. **Registrant**, in their language. Course facts from the feed only. No
   submitted free text appears: not the name, not the comments, not the
   billing address. The salutation is neutral for that reason. The mail
   contains the confirm link and says the registration is not complete until
   it is confirmed.
4. If the back-office mail fails (after one retry on a 5xx), redirect to the
   fail page: the registration did not reach anyone and the person must know.
   If only the registrant mail fails, redirect to success anyway: the back
   office has it and will follow up.
5. Redirect (303) to `/anmeldung-erfolg/` or `/registration-success/`, whose
   text changes to "please check your inbox and confirm".

### 4.3 Token

The token carries only what the `BESTÄTIGT` mail needs: registration id,
booking code, registrant email, last name, language, issue time. Sealed with
AES-256-GCM under `TOKEN_KEY` (32 random bytes, fly secret), base64url, well
under 300 characters. Valid 14 days.

Deliberately not the whole registration: the full data is already in the
`UNBESTÄTIGT` mail, a short link survives mail clients that wrap or truncate
long URLs, and link scanners log URLs.

Rotating `TOKEN_KEY` invalidates every open confirmation link. Do it only
between registrations or accept that the back office follows up on those by
hand.

### 4.4 Confirm

`GET /confirm?t=...` opens the token and, if valid, renders a minimal page in
the registrant's language with course facts and one button. **It confirms
nothing.** Corporate link scanners (Microsoft Safe Links, Mimecast, Proofpoint)
fetch every link in incoming mail; if a GET confirmed, every fake sent to a
company address would confirm itself.

`POST /confirm` with the token sends the `BESTÄTIGT` mail (same subject with
`(BESTÄTIGT)`, same id, so mail clients thread the two) and redirects to
`/anmeldung-bestaetigt/` or `/registration-confirmed/`, two new pages on the
site.

Expired, tampered or unreadable token: a page saying so, with a mailto link to
info@arc42.de. A double confirm sends `BESTÄTIGT` twice; without state that
cannot be detected, and a duplicate is harmless.

### 4.5 Feed cache

Fetched at start and refreshed at most every 5 minutes, on demand. If a
refresh fails, the last good copy is used. If there has never been a good copy
(cold start while the site is down), the code check is skipped and every
back-office mail carries a `Hinweis: Kursliste nicht verfügbar`; failing closed
here would reject real bookings because of a GitHub Pages outage.

## 5. Mailjet

- Send API v3.1, `POST https://api.mailjet.com/v3.1/send`, basic auth with an
  API key pair created for this service only (sub-account key), so it can be
  revoked and its statistics read separately.
- **Click and open tracking off, per message** (`TrackClicks: "disabled"`,
  `TrackOpens: "disabled"`). Click tracking would rewrite the confirm link into
  a Mailjet redirect: longer, on a foreign domain, more likely to be flagged,
  and it hands every click to a tracker. Open tracking adds a pixel for no
  purpose here.
- Plain text and HTML part both, HTML kept simple.
- `SandboxMode: true` in the automated tests against the real API (optional,
  guarded by the presence of credentials), so a template error is caught
  without delivering anything.
- Deliverability: arc42.org has SPF and DKIM for Mailjet but **no DMARC
  record**. Add `v=DMARC1; p=none` before go-live. DMARC alignment comes from
  DKIM (`d=arc42.org`); SPF aligns only with a custom return path, which is
  not needed.

## 6. Configuration

| Env | Secret | Meaning |
|---|---|---|
| `MJ_APIKEY_PUBLIC`, `MJ_APIKEY_PRIVATE` | yes | Mailjet key pair |
| `TOKEN_KEY` | yes | 32 bytes, base64 |
| `MAIL_FROM` | no | `trainings@arc42.org` |
| `BACKOFFICE_TO` | no | where UNBESTÄTIGT/BESTÄTIGT go |
| `REPLY_TO` | no | `info@arc42.de` |
| `FEED_URL` | no | `https://trainings.arc42.org/api/trainings.json` |
| `SITE_URL` | no | base for redirects |
| `ALLOWED_ORIGINS` | no | comma list |
| `TEST_RECIPIENTS` | no | if set: test mode. Registrant mail only to these addresses, every subject prefixed `[TEST]` |

The service refuses to start with `TEST_RECIPIENTS` empty unless
`ENVIRONMENT=PRODUCTION`, so a test deployment cannot accidentally mail the
world.

## 7. Testing and rollout

Production is untouched until the last step. Until then the real forms keep
posting to Formspark.

1. **Mailjet by hand.** Sender and key set up, one curl send to a private and
   a corporate Outlook address, SPF/DKIM/DMARC checked in the headers.
2. **Local.** Unit tests per unit; handler tests with a fake feed and a fake
   mailer; golden tests for all mails in both languages; the price/date
   cross-check against the Jekyll output. `make reg-check` mirrors
   `make app-check`, and CI gates on it.
3. **Deployed in test mode** as `arc42-registration`, reachable only at
   `arc42-registration.fly.dev`, `BACKOFFICE_TO` = Gernot, `TEST_RECIPIENTS`
   set.
4. **Test page** on the live site at an unguessable path, `noindex`,
   `sitemap: false`, outside the nav, using the real `registration-form.html`
   with a new `endpoint` parameter. The repository is public, so the path is
   not secret; the allow-list is what makes that harmless.
5. **End-to-end:** DE and EN registration, confirm, double confirm, expired
   and tampered token, honeypot hit, unknown code, closed-but-real code,
   cold-start time, and a mail to a corporate Outlook inbox to prove the link
   scanner does not confirm.
6. **Switch-over, separate PR:** `register.arc42.org` (CNAME at GoDaddy, fly
   certificate), real forms point there, `BACKOFFICE_TO` real,
   `TEST_RECIPIENTS` removed, success pages reworded, new confirmed pages,
   privacy policy names Mailjet and fly.io, back office briefed. Botpoison
   script and page flag removed.
7. **Formspark stays** until arc42.de's own forms are stubs (phase 2 of the
   migration) and a few weeks of clean operation have passed, so going back is
   one revert. Only then do the Formspark ids, the "one form per language"
   sections in CLAUDE.md and README, and the Formspark subscription go.

## 8. Explicitly out of scope

- Storing registrations anywhere. The back-office inbox remains the record.
- Reminder mails for unconfirmed registrations (would need state).
- A challenge widget. Kept in reserve: if bots start mailing arbitrary
  addresses through `/submit`, Friendly Captcha (EU) can be verified
  server-side in `intake` without touching anything else.
- arc42.de. Its remaining forms post to Formspark until they are removed.

## 9. Open points for review

- Back-office mail in German with fixed labels, regardless of form language.
  Today an English registration arrives with English labels.
- The price stated in the mail is the one that applies on the registration
  date (early bird decided by that date). The back office must agree that this
  is binding.
