# Registration Go-Live and Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move real registrations on trainings.arc42.org from Formspark to the registration service, then retire arc42.de's old form pages, then Formspark itself.

**Architecture:** Four stages with waiting time between them (spec section 7.2, stages 5 to 8). Each stage is one PR or one operational step and can be reverted on its own. The code is small; the risk is in ordering, so every task names its preconditions.

**Tech Stack:** Jekyll/Liquid, the service from `docs/superpowers/plans/2026-09-26-registration-service.md`, fly.io, GoDaddy DNS, the arc42.de-site repository (GitHub Pages).

**Spec:** `docs/superpowers/specs/2026-09-25-registration-service-design.md`, section 7.

## Global Constraints

- Preconditions for Task 1, all checked off in Todoist section "Registration service": end-to-end test in test mode passed (including the corporate Outlook check), back office briefed, privacy wording approved by Gernot, `register.arc42.org` certificate issued.
- The `<option value>` stays the booking code byte-for-byte.
- The form's visible field names (`Nachname`, `last name`, `Rechnungsadresse`, `Billing address`, ...) do not change: `registration-app/internal/intake/intake.go` (`fieldNames`) maps them. Renaming one requires changing that map in the same PR.
- `_pages/home.html` and `_pages/home-de.html` stay structural twins; nothing here touches them.
- arc42.de's `/anmeldung/` and `/anmeldungEN/` are never deleted, only stubbed (GitHub Pages cannot redirect; a deleted page is a 404 for bookmarks and course PDFs).
- No em-dashes in page copy.

## Review Focus

- **A registrant with JavaScript off**: after Task 1 the form works without JavaScript for the first time; the `<noscript>` block and the `display:none` wrapper must be gone, and the preselect/summary script must still only enhance. Pinned in Task 1, step 6 (curl the built page, submit with a plain POST).
- **A registrant whose confirmation mail never arrives**: the service's page after submitting tells them to check spam and lets them correct the address (spec 4.6); check it once on the live service after go-live.
- **An old tab or cached page still posting to Formspark after the switch**: Formspark keeps receiving for weeks; the back office must know such a notification is still real. Pinned in Task 2, step 3 (checklist item) and Task 5 (only retire after the Formspark log is quiet).
- **arc42.de's `?kurs=` links**: every id arc42.de links to must exist as a `data-id` on the form, including ids off the naming convention such as `msa-06-2027` (a real date: 27-06 MSA-EN, online), or preselection silently fails. Pinned in Task 4, step 1.
- **Rollback after the service went production**: reverting the site PR sends the forms back to Formspark, but the service still accepts posts from any cached new page; that is fine and must not be "fixed" by stopping the service. Pinned in Task 2, step 4.

---

### Task 1: Site PR, forms post to register.arc42.org

**Files:**
- Modify: `_includes/registration-form.html`, `_includes/head.html`, `_pages/anmeldung.md`, `_pages/registration.md`, `_pages/anmeldung-erfolg.md`, `_pages/registration-success.md`, `_pages/anmeldung-fail.md`, `_pages/registration-fail.md`, `_pages/imprint-privacy.md`, `CLAUDE.md`, `README.md`
- Delete: `_pages/anmeldung-test-8r4tqz.md`, `_pages/registration-test-8r4tqz.md`

- [x] **Step 1: The form**

In `_includes/registration-form.html`:

1. Replace the whole "ONE FORMSPARK FORM PER LANGUAGE" comment and the `formspark_id`/`botpoison_key` assigns with:

```liquid
{%- comment -%}
  The form posts to the registration service (registration-app/, fly app
  arc42-registration, https://register.arc42.org). One endpoint for both
  languages: the hidden `language` field decides the language of every mail.
  The visible field names differ per language and are mapped in
  registration-app/internal/intake/intake.go (fieldNames); rename a field
  here only together with that map. include.endpoint overrides the target
  for local or test runs.
{%- endcomment -%}
{%- assign rf_action = include.endpoint | default: "https://register.arc42.org/submit" -%}
```

and make the form tag `<form action="{{ rf_action }}" method="post" class="registration-form" id="arc42anmeldung">`.

2. Delete both `<noscript>...</noscript>` blocks, the `<div id="main_body" style="display: none;">` opening tag with its closing `</div>`, and the script line `document.getElementById("main_body").style.display="block";`. The form now works without JavaScript; the script only preselects and fills the summary.

3. Delete the Formspark-only hidden fields: `_source`, `_redirect`, `_error`, `_append`, `_email.subject`, `_email.from`, `_email.template.title`, and the HTML comment "custom REDIRECT / SOURCE / EMAIL configuration for Formspark". Keep `_gotcha`, `company_website`, `form_source`, `via`, `language`. In the honeypot comment, replace the Formspark explanation with: the service drops a submission whose `_gotcha` or `company_website` is non-empty (`intake.Check`), without mail and with the normal success page.

- [x] **Step 2: Botpoison out**

Delete the Botpoison `<script>` block and its comment from `_includes/head.html`, and the `botpoison: nospam` line from `_pages/anmeldung.md` and `_pages/registration.md`.

- [x] **Step 3: Success and fail pages**

Since 2026-09-27 the service shows its own page after submitting (the typed
address plus a correction form, spec 4.2 step 5 and 4.6), so nothing redirects
to `/anmeldung-erfolg/` or `/registration-success/` any more. Keep both pages
(bookmarks, and the rollback to Formspark needs them) and leave their text as
it is. In both fail pages, replace the first paragraph ("Unsere Anmeldeseite
benötigt JavaScript..." / "Our registration page requires JavaScript...") with
"Beim Absenden ist etwas schiefgegangen." / "Something went wrong while
sending your registration." and keep the mailto button.

- [x] **Step 4: Privacy statement**

Add a section to `_pages/imprint-privacy.md` after "Kontakt- und Anfragenverwaltung", using the wording Gernot approved in Todoist ("Privacy policy: name Mailjet and fly.io"). The facts it must state: registration data (names, e-mail, billing address, comments) is received by a service hosted by Fly.io, Inc. on a server in Amsterdam, which stores nothing after the request; two e-mails are sent through Mailjet SAS (Sinch group, processing in the EU); no tracking pixels or click tracking; the confirmation link carries the registration id, booking code, e-mail and last name in encrypted form and expires after 5 days. Formspark stays listed until Task 5.

- [x] **Step 5: Test pages out, docs in**

Delete the two `*-test-8r4tqz.md` pages. In `CLAUDE.md`, replace the bullet "One Formspark form per language" with a bullet "The form posts to the registration service" carrying the field-name mapping rule from the Global Constraints above and the note that the language of every mail comes from the hidden `language` field. Update the matching README section the same way.

- [x] **Step 6: Verify**

```bash
make site
grep -o '<form action="[^"]*" method="post"' _site/anmeldung/index.html _site/registration/index.html
grep -c -i -E 'botpoison|submit-form.com|noscript|main_body' _site/anmeldung/index.html _site/registration/index.html _site/index.html
test ! -e _site/anmeldung-test-8r4tqz && echo test-page-gone
make check-links
```

Expected: both forms post to `https://register.arc42.org/submit`; the count is `0` everywhere; `test-page-gone`; the link check passes.

- [x] **Step 7: Open the PR, do not merge yet**

Title `Registrations go to register.arc42.org`. Description: the Review Focus items, the rollback (revert this PR), and "merge only after Task 2 step 2".

---

### Task 2: Service to production, then merge

- [x] **Step 1: Production settings** (fly.toml done in the go-live PR; the secrets are Gernot's)

In `registration-app/fly.toml` set `ENVIRONMENT = "PRODUCTION"`, `PUBLIC_URL = "https://register.arc42.org"` (already set since 28 Sep 2026, in test mode), `ALLOWED_ORIGINS = "https://trainings.arc42.org"`. Gernot sets the secrets (never Claude): `fly secrets set -a arc42-registration BACKOFFICE_TO=<real back-office address>` and `fly secrets unset -a arc42-registration TEST_RECIPIENTS`. Commit the fly.toml change in the same PR as Task 1.

- [ ] **Step 2: Deploy and check**

Run: `make reg-deploy`, then `curl -sI https://register.arc42.org/healthz` (expect 200) and `make reg-logs` (expect no `test mode:` line at start-up).

- [ ] **Step 3: Merge and one real registration**

Merge the PR. After GitHub Pages has built (check `https://trainings.arc42.org/anmeldung/` shows the new form action), Gernot registers once for a real date with his own address and a note "TEST, bitte ignorieren", confirms, and checks that the back office received UNBESTÄTIGT then BESTÄTIGT and the registrant mail came from `trainings@arc42.org`. Tell the back office: for a few weeks, a Formspark notification can still arrive (an old open tab, a cached page); it is a real registration.

- [ ] **Step 4: Rollback, if needed**

Revert the site PR. The forms post to Formspark again within one Pages build. Leave the service running: any page still open with the new form keeps working through it.

---

### Task 3: Observation (2 to 4 weeks)

- [ ] **Step 1:** Weekly, Gernot looks at: UNBESTÄTIGT without BESTÄTIGT (how many, how many were real after follow-up), mails reported in spam folders, anything in `make reg-logs` with `mail failed`. More than one real registrant a week without confirmation means the confirmation mail has a deliverability problem: check Mailjet's message log for bounces and blocks before anything else.
- [ ] **Step 2:** At the end, write three lines into the Todoist task "Switch-over trainings.arc42.org" as a comment: counts and whether to continue with Task 4.

---

### Task 4: arc42.de, stub the old form pages (arc42.de-site repository)

Precondition: Task 3 closed with "continue".

**Files (arc42.de-site):** `_pages/anmeldung.md`, `_pages/anmeldungEN.md`, `_includes/head/custom.html` (Botpoison block), the `test-theme` target in its `Makefile`, `README.md`, and the data file behind its `/termine` page.

- [ ] **Step 1: Every linked id exists**

Before stubbing anything, confirm that every `?kurs=` value on arc42.de exists as a `data-id` on `https://trainings.arc42.org/anmeldung/`. (An earlier note suspected `msa-06-2027` was stale; it is not, it is a separate date with an off-convention id. If a real mismatch shows up, check arc42.de's workflow that refreshes its copy of `trainings.json`.) Verify with:

```bash
curl -s https://trainings.arc42.org/anmeldung/ | grep -o 'data-id="[^"]*"' | sort -u > /tmp/ids
# after building arc42.de locally:
grep -rho 'kurs=[a-z0-9-]*' _site | sed 's/kurs=/data-id="/;s/$/"/' | sort -u | comm -23 - /tmp/ids
```

Expected: no output (every linked id exists).

- [ ] **Step 2: Stubs**

Replace the body of `_pages/anmeldung.md` (keep its front matter, remove `botpoison: nospam`) with:

```markdown
Die Anmeldung zu unseren Schulungen finden Sie jetzt auf **trainings.arc42.org**.

<a class="btn btn--primary" href="https://trainings.arc42.org/anmeldung/?via=arc42.de">Zur Anmeldung</a>
```

and `_pages/anmeldungEN.md` with:

```markdown
Registration for our trainings has moved to **trainings.arc42.org**.

<a class="btn btn--primary" href="https://trainings.arc42.org/registration/?via=arc42.de">Go to registration</a>
```

Remove the Botpoison block from `_includes/head/custom.html`. Adjust the three `make test-theme` guards named in the Todoist task ("Migrate arc42.de registrations", phase 2): the pages still exist, but the German-label check on `anmeldungEN` and anything asserting form fields must change to assert the stub's link instead.

- [ ] **Step 3: Verify and PR**

Run the repository's build and `make test-theme`; `grep -c 'submit-form.com' _site/anmeldung/index.html _site/anmeldungEN/index.html` prints `0`. Open a PR in arc42.de-site titled `Old registration pages become pointers to trainings.arc42.org`.

---

### Task 5: Retire Formspark

Precondition: 3 to 4 weeks after Task 4, and the Formspark submission log shows nothing new for both forms since Task 4 merged.

- [ ] **Step 1:** Gernot works through the Todoist task "Retire Formspark" (export, delete forms and Botpoison projects, cancel the subscription).
- [ ] **Step 2:** In this repo, remove Formspark from `_pages/imprint-privacy.md` if Task 1 left it listed, and grep for leftovers: `git grep -n -i -E 'formspark|botpoison|AIKiYyJP|Tq1M7LqmX'` must only hit `docs/` (history) afterwards.
- [ ] **Step 3:** In arc42.de-site, work through the Todoist task "Delete the dead registration form code on arc42.de" (its README's Formspark sections, `assets/js/arc42-anmeldung.js`, the `_redirects` file).
- [ ] **Step 4:** Set the spec's status line to `implemented` with the date, and commit.
