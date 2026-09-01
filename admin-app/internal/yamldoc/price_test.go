package yamldoc

import (
	"strings"
	"testing"

	"arc42-trainings-admin/internal/model"
)

// The nested mappings are the only two in trainings.yml, and this package
// splices text rather than marshalling, so what RenderDate emits has to survive
// a re-parse byte for byte. These tests are that guarantee.

func TestModelReadsPriceAndCreditPoints(t *testing.T) {
	doc, err := Parse(realFile(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	m := doc.Model()

	row, ok := m.FindDate("msa-mar-2027")
	if !ok {
		t.Fatal("msa-mar-2027 not found in the real trainings.yml")
	}
	p := row.Date.Price
	if p == nil {
		t.Fatal("price did not decode")
	}
	if p.Amount != 2890 || p.Currency != "EUR" {
		t.Errorf("price = %d %s, want 2890 EUR", p.Amount, p.Currency)
	}
	if p.EarlyBird == nil {
		t.Fatal("early_bird did not decode")
	}
	if p.EarlyBird.Amount != 2690 || p.EarlyBird.Until != "2026-11-02" {
		t.Errorf("early bird = %d until %q, want 2690 until 2026-11-02",
			p.EarlyBird.Amount, p.EarlyBird.Until)
	}

	var improve *model.Course
	for i := range m.Courses {
		if m.Courses[i].ID == "improve" {
			improve = &m.Courses[i]
		}
	}
	if improve == nil {
		t.Fatal("course improve not found")
	}
	if improve.CreditPoints == nil {
		t.Fatal("credit_points did not decode")
	}
	if improve.CreditPoints.Methodical != 20 || improve.CreditPoints.Technical != 10 {
		t.Errorf("credit_points = %+v, want 20 methodical / 10 technical", *improve.CreditPoints)
	}
	if improve.CreditPoints.Communication != 0 {
		t.Errorf("absent category decoded as %d, want 0", improve.CreditPoints.Communication)
	}
}

// The deadline has to come back as a string. Unquoted YYYY-MM-DD is a timestamp
// to YAML, which is the same trap start/end have always carried.
func TestPriceRoundTripsThroughRenderAndParse(t *testing.T) {
	doc, _ := Parse([]byte(twoDates))
	row, _ := doc.Model().FindDate("a")
	d := row.Date
	d.Price = &model.Price{
		Amount:    2890,
		Currency:  "EUR",
		Alumni:    2050,
		EarlyBird: &model.EarlyBird{Amount: 2690, Until: "2027-01-15"},
	}
	d.SeatsLimited = true
	if err := doc.UpdateDate("a", d); err != nil {
		t.Fatalf("UpdateDate: %v", err)
	}
	out := string(doc.Bytes())

	if !strings.Contains(out, `until: "2027-01-15"`) {
		t.Errorf("early-bird deadline lost its quotes:\n%s", out)
	}
	if strings.Contains(out, `amount: "2890"`) {
		t.Error("amount was quoted; it must stay a number")
	}
	if !strings.Contains(out, "seats_limited: true") {
		t.Error("seats_limited was not emitted")
	}

	reparsed, err := Parse(doc.Bytes())
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	r2, _ := reparsed.Model().FindDate("a")
	got := r2.Date.Price
	if got == nil || got.Amount != 2890 || got.Currency != "EUR" {
		t.Fatalf("price round-tripped as %+v", got)
	}
	if got.Alumni != 2050 {
		t.Errorf("alumni price round-tripped as %d, want 2050", got.Alumni)
	}
	if got.EarlyBird == nil || got.EarlyBird.Amount != 2690 || got.EarlyBird.Until != "2027-01-15" {
		t.Errorf("early bird round-tripped as %+v", got.EarlyBird)
	}
	if !r2.Date.SeatsLimited {
		t.Error("seats_limited round-tripped as false")
	}
	// The neighbour must be untouched, as for every other edit in this package.
	if !strings.Contains(out, `id: b`) {
		t.Error("the other date was disturbed")
	}
}

// Absent is absent. An empty mapping would be worse than no key, and a zero
// amount is not a free course, it is a missing one.
func TestRenderOmitsAbsentPriceSeatsAndCredits(t *testing.T) {
	out := RenderDate(model.Date{
		ID: "x", Code: "X", Start: "2027-01-01", End: "2027-01-02",
		Language: "de", Format: "online", Status: "open", URL: "https://example.org/x",
	}, "      ")
	for _, unwanted := range []string{"price:", "amount:", "seats_limited:", "early_bird:", "alumni:"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("emitted %q for a date that has none:\n%s", unwanted, out)
		}
	}

	c := RenderCourse(model.Course{ID: "c", ShortTitle: "C", Title: "C", URL: "https://example.org"}, "  ")
	if strings.Contains(c, "credit_points:") {
		t.Errorf("emitted credit_points for a course with none:\n%s", c)
	}
	// The retired string field wrote "credits: null" to hold the key open. A
	// mapping has no such form, so nothing at all should appear.
	if strings.Contains(c, "credits") {
		t.Errorf("the retired credits key came back:\n%s", c)
	}
}

func TestRenderCourseEmitsOnlyNonZeroCreditCategories(t *testing.T) {
	out := RenderCourse(model.Course{
		ID: "c", ShortTitle: "C", Title: "C", URL: "https://example.org",
		CreditPoints: &model.CreditPoints{Methodical: 20, Communication: 10},
	}, "  ")
	if !strings.Contains(out, "methodical: 20") || !strings.Contains(out, "communication: 10") {
		t.Errorf("categories missing:\n%s", out)
	}
	if strings.Contains(out, "technical:") {
		t.Errorf("zero category was emitted:\n%s", out)
	}
}

// Every date in the real file carries a price. That is the invariant the
// consumer sites depend on: a course whose price lives only in a template is
// a course no other site can quote.
func TestEveryPublishedDateHasAPrice(t *testing.T) {
	doc, err := Parse(realFile(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, c := range doc.Model().Courses {
		for _, d := range c.Dates {
			if d.Price == nil || d.Price.Amount <= 0 {
				t.Errorf("date %s (course %s) has no price", d.ID, c.ID)
			}
		}
	}
}

// The alumni rate never expires, unlike the early-bird one, so it is rendered
// and published unconditionally.
func TestAlumniPriceIsWrittenWhenPresent(t *testing.T) {
	out := RenderDate(model.Date{
		ID: "x", Code: "X", Start: "2027-01-01", End: "2027-01-02",
		Language: "de", Format: "public", City: "Köln", Status: "open",
		URL:   "https://example.org/x",
		Price: &model.Price{Amount: 2200, Currency: "EUR", Alumni: 2050},
	}, "      ")
	if !strings.Contains(out, "alumni: 2050") {
		t.Errorf("alumni price not emitted:\n%s", out)
	}
	if strings.Contains(out, `alumni: "2050"`) {
		t.Error("alumni price was quoted; it must stay a number")
	}
}

// A price with no currency is stored as EUR rather than as a blank that would
// fail the published schema later, where the message is further from the edit.
func TestRenderDefaultsCurrencyToEUR(t *testing.T) {
	out := RenderDate(model.Date{
		ID: "x", Code: "X", Start: "2027-01-01", End: "2027-01-02",
		Language: "de", Format: "online", Status: "open", URL: "https://example.org/x",
		Price: &model.Price{Amount: 2100},
	}, "      ")
	if !strings.Contains(out, "currency: EUR") {
		t.Errorf("currency did not default to EUR:\n%s", out)
	}
}
