package mail

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arc42-registration/internal/feed"
	"arc42-registration/internal/intake"
	"arc42-registration/internal/token"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/mail -update, then read the file)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file.\n--- got ---\n%s", name, got)
	}
}

var dez = feed.Entry{
	CourseTitle: "Mastering Software Architectures", CourseShortTitle: "MSA",
	Date: feed.Date{Code: "26-12 MSA", Start: "2026-12-01", End: "2026-12-04", City: "München",
		Format: "public", Trainers: []string{"Peter Hruschka", "Gernot Starke"},
		Price: &feed.Price{Amount: 2890, Currency: "EUR"}},
}

const confirmURL = "https://register.arc42.org/confirm?t=TOKEN"

func TestRegistrantMails(t *testing.T) {
	for _, l := range []string{"de", "en"} {
		r, err := Registrant(l, FactsFor(dez, l), false, confirmURL, "482913")
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "registrant_"+l+".txt", r.Subject+"\n\n"+r.Text)
		golden(t, "registrant_"+l+".html", r.HTML)
		other, err := Registrant(l, nil, true, confirmURL, "482913")
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "registrant_other_"+l+".txt", other.Subject+"\n\n"+other.Text)
	}
}

func TestBackofficeMails(t *testing.T) {
	reg := intake.Registration{
		ID: "R-7F3KQ", LastName: "Müller", FirstName: "Anna", Email: "anna@example.org",
		Code: "26-12 MSA", Billing: "Firma X\nStraße 1\n12345 Ort", Comments: "",
		Via: "arc42.de", FormSource: "https://trainings.arc42.org/anmeldung/",
	}
	for _, l := range []string{"de", "en"} {
		reg.Lang = l
		r, err := Backoffice(reg, FactsFor(dez, l), []string{intake.HintGmailDots, intake.HintClosed})
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "backoffice_"+l+".txt", r.Subject+"\n\n"+r.Text)
		c, err := Confirmed(token.Claims{ID: "R-7F3KQ", Code: "26-12 MSA", Email: "anna@example.org", LastName: "Müller", Lang: l})
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "confirmed_"+l+".txt", c.Subject+"\n\n"+c.Text)
		if strings.TrimSuffix(r.Subject, "(UNBESTÄTIGT)") != strings.TrimSuffix(c.Subject, "(BESTÄTIGT)") &&
			strings.TrimSuffix(r.Subject, "(UNCONFIRMED)") != strings.TrimSuffix(c.Subject, "(CONFIRMED)") {
			t.Errorf("subjects must differ only in the tag, so they thread:\n%s\n%s", r.Subject, c.Subject)
		}
	}
}

// The contract that keeps the registrant mail from being a spam relay: no
// text the registrant typed may appear in it, in either part, in either
// language.
func TestRegistrantMailEchoesNothingTyped(t *testing.T) {
	for _, l := range []string{"de", "en"} {
		r, _ := Registrant(l, FactsFor(dez, l), false, confirmURL, "482913")
		for _, typed := range []string{"Müller", "Anna", "Firma X", "Straße 1"} {
			if strings.Contains(r.Text, typed) || strings.Contains(r.HTML, typed) || strings.Contains(r.Subject, typed) {
				t.Errorf("%s registrant mail contains typed text %q", l, typed)
			}
		}
	}
}

// No price at all in the mail the registrant receives, not even the regular
// one: many clients have special agreements, and a price in writing from us
// reads as a quote. Prices are settled with the invoice (Gernot, 28 Sep 2026).
func TestNoPriceInTheRegistrantMail(t *testing.T) {
	for _, l := range []string{"de", "en"} {
		r, _ := Registrant(l, FactsFor(dez, l), false, confirmURL, "482913")
		low := strings.ToLower(r.Text + r.HTML)
		for _, banned := range []string{"preis", "price", "€", "eur", "2.890", "2,890", "früh", "early", "alumni", "2.690", "2,690"} {
			if strings.Contains(low, banned) {
				t.Errorf("%s mail mentions %q", l, banned)
			}
		}
	}
}

func TestOnlineDateSaysOnline(t *testing.T) {
	e := dez
	e.Format, e.City = "online", ""
	if f := FactsFor(e, "en"); f.Where != "Online" {
		t.Errorf("Where = %q", f.Where)
	}
}

func TestHTMLEscapesFacts(t *testing.T) {
	e := dez
	e.CourseTitle = `<script>alert(1)</script>`
	r, _ := Registrant("en", FactsFor(e, "en"), false, confirmURL, "482913")
	if strings.Contains(r.HTML, "<script>") {
		t.Error("HTML part did not escape a course title")
	}
}

// When the course list could not be read, a real booking has no facts. It
// must not be told it chose "Sonstige"/"other".
func TestRegistrantWithoutFactsIsNotOther(t *testing.T) {
	for _, l := range []string{"de", "en"} {
		r, err := Registrant(l, nil, false, confirmURL, "482913")
		if err != nil {
			t.Fatal(err)
		}
		all := r.Subject + r.Text + r.HTML
		if strings.Contains(all, "Sonstige") || strings.Contains(all, `"other"`) || strings.Contains(r.Subject, "Anfrage") || strings.Contains(r.Subject, "request") {
			t.Errorf("%s: a booking without facts reads like 'other':\n%s\n%s", l, r.Subject, r.Text)
		}
		if !strings.Contains(r.Text, confirmURL) {
			t.Errorf("%s: confirm link missing", l)
		}
	}
}

// A waiting-list date has no flow of its own, only a note in the registrant
// mail, so nobody reads the confirmation as a booked seat.
func TestWaitlistDateGetsANoteInTheRegistrantMail(t *testing.T) {
	w := dez
	w.Status = "waitlist"
	for _, l := range []string{"de", "en"} {
		note := map[string]string{"de": "Warteliste", "en": "waiting list"}[l]
		r, _ := Registrant(l, FactsFor(w, l), false, confirmURL, "482913")
		if !strings.Contains(r.Text, note) || !strings.Contains(r.HTML, note) {
			t.Errorf("%s: waitlist date without a waiting-list note:\n%s", l, r.Text)
		}
		open, _ := Registrant(l, FactsFor(dez, l), false, confirmURL, "482913")
		if strings.Contains(open.Text, note) {
			t.Errorf("%s: an open date mentions the waiting list", l)
		}
	}
}

// The code leads the mail: it is what people type on the page that stayed
// open, grouped 3+3 so it reads and types easily. The link stays as the
// fallback for a closed tab or another device.
func TestRegistrantMailLeadsWithTheCode(t *testing.T) {
	for _, l := range []string{"de", "en"} {
		r, err := Registrant(l, FactsFor(dez, l), false, confirmURL, "482913")
		if err != nil {
			t.Fatal(err)
		}
		for name, body := range map[string]string{"text": r.Text, "html": r.HTML} {
			code, link := strings.Index(body, "482 913"), strings.Index(body, confirmURL)
			if code < 0 || link < 0 || code > link {
				t.Errorf("%s %s: code at %d, link at %d; want both, code first", l, name, code, link)
			}
		}
	}
}

// "How did you hear about this course?" reaches the back office as one line,
// the choice with its details in brackets, and stays out of the way when the
// registrant left both empty.
func TestBackofficeMailSaysHowTheyHeard(t *testing.T) {
	reg := intake.Registration{ID: "R-7F3KQ", LastName: "Müller", Email: "anna@example.org", Code: "26-12 MSA",
		Billing: "X", HeardVia: "Buch", HeardViaDetail: "arc42 by Example"}
	for l, want := range map[string]string{
		"de": "Aufmerksam durch:  Buch (arc42 by Example)",
		"en": "Heard via:                Buch (arc42 by Example)",
	} {
		reg.Lang = l
		r, _ := Backoffice(reg, FactsFor(dez, l), nil)
		if !strings.Contains(r.Text, want) {
			t.Errorf("%s: missing %q in\n%s", l, want, r.Text)
		}
	}
	reg.HeardVia, reg.HeardViaDetail, reg.Lang = "", "", "de"
	if r, _ := Backoffice(reg, FactsFor(dez, "de"), nil); strings.Contains(r.Text, "Aufmerksam durch") {
		t.Error("an empty answer still printed the line")
	}
}
