# Structured price, credits and seats — design

**Date:** 2026-08-31
**Status:** implemented
**Scope:** `_data/trainings.yml`, the label includes, `/api/trainings.json` and
its schema, `scripts/validate_trainings.rb`, and the admin app's model, YAML
round-trip, forms and tests.

Normative context: [ADR-0004](https://github.com/arc42/meta.arc42.org/blob/main/adr/0004-trainings-feed-is-a-contract.md)
(the feed is a contract), [ADR-0006](https://github.com/arc42/meta.arc42.org/blob/main/adr/0006-training-dates-single-source.md)
(single source of truth). Where this spec conflicts with an ADR, the ADR wins.

---

## 1. Problem

Three fields in `_data/trainings.yml` held hand-written German sentences:

```yaml
pricing: "Frühbucherpreis bei Anmeldung bis 8. August 2026: € 2690, Normalpreis: € 2890"
credits: "20 methodische und 10 technische Punkte"
few_seats: "only few seats available"
```

That is prose in a language-neutral data file, and it failed in both
directions. German pricing and German credit points appeared verbatim on every
English page; an English seats note appeared on every German page.

`pricing` failed a second, worse way. The early-bird deadline was written
*inside* the sentence, where no machine could see it. On 2026-08-31 the
December MSA was still advertising "bis 8. August 2026" — an offer that had
lapsed 23 days earlier — at €2690 against a real price of €2890, on both
language versions and on all four consumer sites.

The site already solves exactly this class of problem for dates. The timeline
filters on `d.end < today` at build time, and `weekly-refresh.yml` rebuilds on
a timer precisely so that the passing of time becomes a content change. A price
that expires is the same problem. It just was not expressible.

## 2. Decision

**Numbers and flags in the data; sentences in the templates.**

```yaml
price:                     credit_points:        seats_limited: true
  amount: 2890               methodical: 20
  currency: EUR              technical: 10       # also: communication
  early_bird:
    amount: 2690
    until: "2026-11-02"    # QUOTED, like start/end
```

Rendering moves into three includes: `money.html` (one amount, per language),
`price-label.html` (the sentence, comparing `early_bird.until` against the
build date) and `credits-label.html` (the categories, joined per language).
`timeline_auto.html` renders all three once per date and passes finished
strings down, so the six card templates did not change.

Amounts are integers in whole currency units. arc42 has never priced a training
in cents, and an integer stays comparable and formattable instead of only
printable.

### 2.1 Consequences that fall out for free

- An expired early-bird offer stops being advertised on its own, worst case one
  Monday late, under the rebuild guarantee that already exists.
- An expired offer does **not** need deleting from the data. It is inert, and
  keeping it preserves the record of what was offered.
- Prices and credits can be rendered in the reader's language, on this site and
  eventually on every consumer.

## 3. The feed stays a contract

Four consumer sites read `pricing`, `credits` and `few_seats` as strings and
render them verbatim. ADR-0004 permits adding optional fields and forbids
changing existing ones, so the published document now carries **both**:

| deprecated (kept, generated) | canonical (new) |
| --- | --- |
| `date.pricing` (string) | `date.price` (object) |
| `course.credits` (string) | `course.credit_points` (object) |
| `date.few_seats` (string) | `date.seats_limited` (bool) |

No published key changes type, so no consumer had to do anything. The three
legacy strings are now generated per build rather than stored, which is the
point: they stop being able to go stale. Consumers that migrate gain the
language freedom; consumers that never migrate simply stop being lied to.

The YAML keys were renamed (`credits` → `credit_points`, `few_seats` →
`seats_limited`) specifically so that a source key and a published key never
share a name while holding different types.

`api/trainings.json` is therefore written out by hand instead of
`courses | jsonify`. It also **omits an expired `early_bird` from `price`**, not
only from `pricing`: pushing the expiry comparison onto four consumers is the
arrangement this change exists to remove.

### 3.1 Rejected: a v2 endpoint

Publishing `/api/v2/trainings.json` was considered and rejected. Additive
fields on the existing document achieve the same migration with one endpoint,
one schema and no speculative contract that no consumer has asked for.

## 4. Guardrails

- `scripts/validate_trainings.rb` rejects `pricing`, `few_seats` and a
  string `credits` **by name**, with a message naming the replacement.
  Reintroducing prose is a CI failure, not a silent regression.
- It also checks that an early-bird amount is below the regular price and that
  its deadline is not after the course starts. An expired deadline is
  deliberately *not* an error.
- `safe_load` now permits `Date`, so an unquoted `YYYY-MM-DD` produces the
  script's own "must be QUOTED" message instead of a Psych stack trace.
- The published schema marks the three legacy fields deprecated in their
  `description`, so the next person reading it knows which half to use.
- `internal/yamldoc/price_test.go` round-trips the nested mappings through
  render and re-parse, including the quoting of `until` (unquoted
  `YYYY-MM-DD` is a timestamp to YAML — the same trap `start`/`end` carry).

## 5. Admin app

`model.Price`, `model.EarlyBird` and `model.CreditPoints` replace the three
string fields. The date form now has four number/date inputs instead of one
free-text "Pricing" box, and a checkbox instead of the seats note. The course
form has three category counts.

Two deliberate choices:

- **The new-date prefill copies the amounts but never the early-bird
  deadline.** A deadline belongs to the run it was set for; copying it forward
  would seed each new date with an offer that expires before anyone can take
  it, which is a tidier version of the original bug.
- **The app's schema shim does not reproduce the legacy prose fields.** They
  are optional in the schema, and a second Go implementation of German wording
  that already exists once in Liquid would drift. The shim validates the values
  the app is about to write, which is where those values now live.

## 6. Second pass: prices out of the templates

The first pass moved the *stored* prices into `price`. It then turned out that
only 7 of 15 dates had one: the other four course templates assigned a
hardcoded bilingual literal (`Normalpreis: € 2200 (für unsere Alumni € 2050)`,
`Teilnahmegebühr: € 2100`, `Normalpreis: € 1500`) and rendered that instead.

Three consequences, all now fixed:

- Those prices never reached `/api/trainings.json`, so no consumer could quote
  them.
- The booking summary added in the same week showed no price for a course whose
  card, a scroll away, showed €2200.
- `timeline_msa_online.html` held `€ 2100` as a literal while the same 2100 sat
  in the data for the same dates, free to drift.

All 15 dates now carry a `price`. The literals are gone, `timeline_course.html`
forwards `pricing=` to all six templates (three of them never did, which was
invisible while the price was a literal), and `price.alumni` models the reduced
rate for former participants. Unlike `early_bird` it has no deadline, so it is
always rendered and always published.

`TestEveryPublishedDateHasAPrice` asserts the invariant against the real data
file.

Note what this changes downstream: arc42.de and arc42.org gain prices for
IMPROVE, REQ4ARC and ADOC where they previously showed none. docs.arc42.org and
faq.arc42.org render no prices at all and are unaffected.

## 7. Known gap

`city` is still occasionally prose: `"Mannheim / Frankfurt (t.b.d.)"` encodes
"undecided" inside a place name. The German connector was removed, but deciding
whether an undecided venue deserves a field of its own is a content question,
not a modelling one, and it is left open.
