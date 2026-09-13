package model

import (
	"strings"
	"testing"
)

func aDate() Date {
	return Date{
		ID: "msa-online-sep-2026", Code: "26-09 MSA-EN",
		Start: "2026-09-21", End: "2026-09-24",
		Country: "DE", Language: "en", Format: "online",
		Trainers: []string{"Dr. Gernot Starke"},
		Price:    &Price{Amount: 2890, Currency: "EUR"},
		Status:   "open",
	}
}

// The change this whole report exists for: one checkbox, one row.
func TestMarkingFewSeatsIsOneRow(t *testing.T) {
	before := aDate()
	after := before
	after.SeatsLimited = true

	got := DiffDates(before, after)
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(got), got)
	}
	if got[0].Key != "seats_limited" || got[0].Before != "no" || got[0].After != "yes" {
		t.Errorf("row = %+v", got[0])
	}
}

func TestUnchangedFieldsAreNotReported(t *testing.T) {
	d := aDate()
	if got := DiffDates(d, d); len(got) != 0 {
		t.Errorf("identical dates produced %d rows: %+v", len(got), got)
	}
}

// A price that prints the same is the same price: nobody reviewing a proposal
// wants a row saying "€ 2,890 → € 2,890" because a currency default moved.
func TestPriceIsComparedAsRendered(t *testing.T) {
	before := aDate()
	after := before
	after.Price = &Price{Amount: 2890, Currency: "EUR"}
	if got := DiffDates(before, after); len(got) != 0 {
		t.Errorf("equal prices produced %+v", got)
	}

	after.Price = &Price{Amount: 2690, Currency: "EUR", EarlyBird: &EarlyBird{Amount: 2490, Until: "2026-08-08"}}
	got := DiffDates(before, after)
	if len(got) != 1 || got[0].Key != "price" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Before != "€ 2,890" || got[0].After != "€ 2,690, early bird € 2,490 until 2026-08-08" {
		t.Errorf("price row = %+v", got[0])
	}
}

// Addition and removal are the same comparison against the zero value, which
// is the only reason there is one function here instead of three.
func TestTheZeroDateDescribesAdditionAndRemoval(t *testing.T) {
	added := DiffDates(Date{}, aDate())
	if len(added) == 0 {
		t.Fatal("an added date reported no fields")
	}
	for _, f := range added {
		if f.Before != NoValue {
			t.Errorf("added field %q has a before value %q", f.Key, f.Before)
		}
	}
	// seats_limited is false on both sides of an ordinary addition, so it must
	// not appear as "no → no".
	for _, f := range added {
		if f.Key == "seats_limited" {
			t.Error("an addition with no seat limit reported the seat limit")
		}
	}
	removed := DiffDates(aDate(), Date{})
	if len(removed) != len(added) {
		t.Errorf("removal reported %d fields, addition %d", len(removed), len(added))
	}
	for _, f := range removed {
		if f.After != NoValue {
			t.Errorf("removed field %q has an after value %q", f.Key, f.After)
		}
	}
}

func TestValuesAreRenderedForPeople(t *testing.T) {
	before := aDate()
	after := before
	after.Language = "de"
	after.City = "Köln"
	after.Status = "full"

	byKey := map[string]FieldChange{}
	for _, f := range DiffDates(before, after) {
		byKey[f.Key] = f
	}
	for _, c := range []struct{ key, before, after string }{
		{"language", "English", "German"},
		{"city", NoValue, "Köln"},
		{"status", "open", "full"},
	} {
		got, ok := byKey[c.key]
		if !ok {
			t.Errorf("no row for %q", c.key)
			continue
		}
		if got.Before != c.before || got.After != c.after {
			t.Errorf("%s = %q → %q, want %q → %q", c.key, got.Before, got.After, c.before, c.after)
		}
	}
}

func TestCourseFieldsAreCompared(t *testing.T) {
	before := Course{ID: "msa", ShortTitle: "MSA", CreditPoints: &CreditPoints{Methodical: 20}}
	after := before
	after.CreditPoints = &CreditPoints{Methodical: 20, Technical: 10}

	got := DiffCourses(before, after)
	if len(got) != 1 || got[0].Key != "credit_points" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Before != "20 methodical" || got[0].After != "20 methodical, 10 technical" {
		t.Errorf("credits row = %+v", got[0])
	}
}

func TestMoneyIsGrouped(t *testing.T) {
	cases := []struct {
		price *Price
		want  string
	}{
		{nil, ""},
		{&Price{Amount: 0, Currency: "EUR"}, ""},
		{&Price{Amount: 990, Currency: "EUR"}, "€ 990"},
		{&Price{Amount: 2890, Currency: "EUR"}, "€ 2,890"},
		{&Price{Amount: 12500, Currency: "CHF"}, "CHF 12,500"},
		{&Price{Amount: 2890, Currency: "EUR", Alumni: 2390}, "€ 2,890, alumni € 2,390"},
	}
	for _, c := range cases {
		if got := FormatPrice(c.price); got != c.want {
			t.Errorf("FormatPrice(%+v) = %q, want %q", c.price, got, c.want)
		}
	}
}

// The list shows the main amount alone. It shares formatMoney with
// FormatPrice so the two can never spell one amount two ways — the pairs below
// are the same prices TestMoneyIsGrouped feeds the long form.
func TestTheShortPriceIsTheHeadlineAmountOnly(t *testing.T) {
	cases := []struct {
		price *Price
		want  string
	}{
		{nil, ""},
		{&Price{Amount: 0, Currency: "EUR"}, ""},
		{&Price{Amount: 990, Currency: "EUR"}, "€ 990"},
		{&Price{Amount: 2100, Currency: "EUR"}, "€ 2,100"},
		{&Price{Amount: 12500, Currency: "CHF"}, "CHF 12,500"},
		// The detail the long form spells out is dropped, not summarised: one
		// line per date has room for the number the operator is looking for
		// and nothing else.
		{&Price{Amount: 2890, Currency: "EUR", Alumni: 2390,
			EarlyBird: &EarlyBird{Amount: 2490, Until: "2026-08-08"}}, "€ 2,890"},
	}
	for _, c := range cases {
		if got := FormatPriceShort(c.price); got != c.want {
			t.Errorf("FormatPriceShort(%+v) = %q, want %q", c.price, got, c.want)
		}
	}
}

// A price that renders at all must render identically in both views, or the
// list and the change report describe the same number differently.
func TestShortAndLongPriceAgreeOnTheAmount(t *testing.T) {
	p := &Price{Amount: 2890, Currency: "EUR", Alumni: 2390}
	if short, long := FormatPriceShort(p), FormatPrice(p); !strings.HasPrefix(long, short) {
		t.Errorf("FormatPrice(%+v) = %q does not start with FormatPriceShort = %q", p, long, short)
	}
}

func TestSpanCollapsesAOneDayRun(t *testing.T) {
	cases := []struct{ start, end, want string }{
		{"2026-09-21", "2026-09-24", "2026-09-21 to 2026-09-24"},
		{"2026-09-21", "2026-09-21", "2026-09-21"},
		{"2026-09-21", "", "2026-09-21"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := (Date{Start: c.start, End: c.end}).Span(); got != c.want {
			t.Errorf("Span(%q,%q) = %q, want %q", c.start, c.end, got, c.want)
		}
	}
}
