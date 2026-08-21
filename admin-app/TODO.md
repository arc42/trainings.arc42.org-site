# Admin app — known usability problems

Reported from real use, not from a review. Each entry records what was seen,
what actually causes it, and what should change. Nothing here is implemented
yet.

## 1. Placeholders read as stored values

**Seen:** the Certification field could not be emptied — "iSAQB CPSA-Foundation"
stayed visible however often Delete was pressed.

**Cause:** the field was already empty. That string is a `placeholder`
(`courseform.gohtml:67`), and there was never anything to delete. It reads as
content because three things line up:

- the example strings are plausible real values — `iSAQB CPSA-Foundation`,
  `msa-dez-2026`, `26-12 MSA`, `München`, `DE`. Of the thirteen placeholders in
  the templates, exactly one — the trainers field — says "e.g.".
- `input::placeholder` is `--ink-muted` (#646769, 5.60:1), the same tone the
  app uses for real secondary text, in the same size and family as a value.
  `app.css:396` argues this deliberately: "placeholders are text and hold the
  same floor". The accessibility floor is right; using the *content* tone to
  meet it is what removes the distinction.
- the hint that resolves it — "Leave empty when it prepares for none" — is
  collapsed behind the "?", so it is invisible at exactly the moment it is
  needed.

**Change:**

1. Prefix every example with `e.g. `, following the trainers field. An
   "e.g." string cannot be mistaken for a stored value, whatever its colour.
   Skip it where the placeholder is a format mask rather than an example.
2. Give placeholders a form distinct from content — italic, plus a lighter
   tone — keeping ≥4.5:1. Once italic carries the signal, the tone no longer
   has to. These are not in conflict.
3. Mark genuinely optional fields as optional in the label, so "may be empty"
   does not depend on opening the hint.

Guard it with a test over the templates: every `placeholder=` starts with
`e.g. ` or is a documented format mask.

## 2. The "?" help button

**Seen:** the "?" is not centred in its circle, and the buttons look like
different colours from field to field.

**Cause, glyph:** `button.help` (`app.css:214`) sets `padding: 0`,
`line-height: 1` and `font: inherit`, with no centring. The glyph lands where
inline text layout puts it inside a 1.75rem box, not in the middle of it.
`font: inherit` also means the "?" takes the size and weight of whatever label
it sits in, so it drifts between contexts.

**Cause, colour:** nothing in the CSS varies this button by field. It has
exactly three fills — rest `--surface-1` (#f5eff2), hover `--surface-hover`
(#e5d1d7), expanded `--surface-2`. What the screenshot shows is a hovered
button beside a resting one. The jump from #f5eff2 to #e5d1d7 is large enough
that the two read as different components rather than one control in two
states.

**Change:** centre with `display: inline-flex; align-items: center;
justify-content: center;` and set an explicit `font-size` instead of
inheriting. Soften hover to roughly `--surface-2`, and let the stronger fill
and border mark the expanded state — that is the one that carries meaning.

While in there: the button is inserted before the control but after the label
text (`form.js:34`). Check it lands consistently in the `.row` pairs, where the
label text wraps differently.

## 3. Date ID and booking code suggest MSA whatever course is selected

**Seen:** placeholders `msa-dez-2026` and `26-12 MSA` on a date for another
course.

**Cause:** both are hardcoded in `dateform.gohtml:30,36`.

**Change:** derive them. The form already has everything needed — each course
`<option>` carries `data-code-token` and its `defaultsOf` values, and `form.js`
already derives the *actual* id and code from course plus start date through
`claim()`. The placeholder should come from that same derivation and update
when the course changes, rather than being a fixed string. With scripting off,
derive server-side from the pre-selected course, or render no placeholder at
all — a wrong example is worse than none.

Check whether the location placeholders (`München`, `DE`, `dateform.gohtml:75,81`)
should follow: those defaults are already per-course.

## 4. Hints are hidden when they are most needed

Not reported directly, but it caused (1). Every hint starts collapsed. The
rules that prevent a mistake — "leave empty when it prepares for none", "must
be unique — a duplicate silently misroutes a registration" — are only visible
to someone who already suspected there was a rule. Worth deciding whether some
hints should start open, or whether the first use of the form should.
