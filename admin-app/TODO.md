# Admin app — known usability problems

Reported from real use, not from a review. Each entry records what was seen,
what actually causes it, and what should change. Resolved entries are deleted
rather than ticked off — git remembers them.

## 1. Hints are hidden when they are most needed

Not reported directly, but it caused the Certification confusion: the field was
empty and the hint saying it may be empty was folded away. Every hint starts
collapsed. The
rules that prevent a mistake — "leave empty when it prepares for none", "must
be unique — a duplicate silently misroutes a registration" — are only visible
to someone who already suspected there was a rule. Worth deciding whether some
hints should start open, or whether the first use of the form should.
