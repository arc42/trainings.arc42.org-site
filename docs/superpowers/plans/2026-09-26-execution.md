# Execution Plan: Registration Service, Waiting List, Go-Live

How the three implementation plans are carried out **natively** (the session
model implements, one fresh reviewer per plan at the end), with the context
window kept small. This file is the entry point: a session reads this file and
then only the task it is working on.

| Plan | File | Lines | Tasks |
|---|---|---|---|
| 1 | `2026-09-26-waiting-list.md` | 549 | 6 |
| 2 | `2026-09-26-registration-service.md` | 3817 | 12 |
| 3 | `2026-09-26-registration-go-live.md` | 200 | 5 |

Spec: `docs/superpowers/specs/2026-09-25-registration-service-design.md`.

## Context rules

1. **One session per block** (below). Start a block with `/clear` or a new
   session. Carry state between sessions through git commits and the checkboxes
   in the plan files, never through conversation memory.
2. **Read by line range, one task at a time.** `Read` with `offset`/`limit`
   from the task table below. Read the plan's header (lines 1 to the first
   task) once per session for the Global Constraints and Review Focus.
3. **Plan 2 code is copied, not read.** Every code block in Plan 2 is
   byte-identical to a file under `registration-service-reference/`. A task
   step "create file X with this content" becomes
   `cp docs/superpowers/plans/registration-service-reference/<path> registration-app/<path>`.
   Read a copied file only when its test fails.
4. **Keep tool output short.** `go test ./internal/<pkg>` without `-v`;
   `make site 2>&1 | tail -5`; grep with `-c` or `-o` as the plans already do.
   Never `cat` a built HTML page.
5. **Tick the checkbox** (`- [ ]` to `- [x]`) in the plan file after each
   step, and commit after each task as the plan says. A session that runs out
   of room resumes from the first unticked box.
6. **Review with a subagent.** At the end of each plan, one reviewer agent
   (Agent tool, most capable model) reviews `git diff main...HEAD` against the
   plan's Review Focus. Its findings come back as a summary; the diff never
   enters the main context.

## Blocks

Each block lists what must be true before it starts. Gates marked **Todoist**
are Gernot's manual tasks in "arc42 sites" > "Registration service".

### Block A: Plan 1, waiting list (branch `feat/waiting-list`)

Gate: none. Ships independently of everything else.

| Task | Lines | Note |
|---|---|---|
| 1 Admin status labels | 35-160 | |
| 2 Admin warnings | 161-241 | |
| 3 PR titles | 242-308 | |
| 4 Cards | 309-457 | edits 8 templates; use `sed`/Edit per file, do not read them whole |
| 5 Form | 458-545 | |
| 6 PR | 546-549 | then the reviewer agent |

### Block B: Plan 2 Tasks 1 to 9, the service (branch `feat/registration-service`)

Gate: none for Tasks 1 to 8 (all local). Task 7 step 4's sandbox test and
Task 9's deploy targets need nothing external either. This block is mostly
`cp` + `make reg-check`.

| Task | Lines | Copy from reference |
|---|---|---|
| 1 Skeleton, config, plumbing | 42-379 | `go.mod`, `internal/config/` (Makefile, `_config.yml`, workflow: from the plan text) |
| 2 Labels | 380-554 | `internal/labels/` |
| 3 Feed | 555-891 | `internal/feed/` |
| 4 Token | 892-1115 | `internal/token/` |
| 5 Intake | 1116-1694 | `internal/intake/` |
| 6 Mail | 1695-2440 | `internal/mail/` incl. `templates/` and `testdata/` |
| 7 Send | 2441-2774 | `internal/send/` |
| 8 Web | 2775-3363 | `internal/web/` incl. `pages/` |
| 9 Main, Docker, fly | 3364-3590 | `main.go`, `Dockerfile`, `fly.toml` (README from the plan text) |

TDD order still holds with copying: copy the `_test.go` file first, run it and
see it fail, then copy the implementation.

### Block C: Plan 2 Tasks 10 to 12, test mode (same branch)

Gates (**Todoist**): Mailjet domain validated and account activated; API key
pair created; curl test mail passed with `dkim=pass`; fly app created with all
five secrets. Optional: `FLY_REGISTRATION_API_TOKEN`.

| Task | Lines |
|---|---|
| 10 Site pages, test pages, endpoint | 3591-3747 |
| 11 Deploy, smoke test | 3748-3784 |
| 12 Docs, drop the reference copy, PR | 3785-3817 |

After merge: **Todoist** end-to-end test in test mode (Gernot, real inboxes,
corporate Outlook).

### Block D: Plan 3 Tasks 1 and 2, go-live (branch `feat/registration-go-live`)

Gates (**Todoist**): end-to-end test passed; back office briefed; privacy
wording approved; `register.arc42.org` CNAME and certificate issued.

| Task | Lines |
|---|---|
| 1 Site PR | 32-121 |
| 2 Production, merge | 122-141 |

### Block E and later: Plan 3 Tasks 3 to 5

Weeks apart; each is a short session of its own. Task 4 runs in the
arc42.de-site repository.

| Task | Lines | When |
|---|---|---|
| 3 Observation | 142-148 | 2 to 4 weeks after go-live |
| 4 arc42.de stubs | 149-192 | after Task 3 says continue |
| 5 Retire Formspark | 193-200 | 3 to 4 weeks after Task 4 |

## Line numbers

The ranges above are for the plan files as committed with this file. If a plan
is edited, regenerate them with
`grep -n '^### Task' docs/superpowers/plans/2026-09-26-*.md`.
