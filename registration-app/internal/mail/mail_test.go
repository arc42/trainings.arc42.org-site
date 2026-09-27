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
		r, err := Registrant(l, FactsFor(dez, l), false, confirmURL)
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "registrant_"+l+".txt", r.Subject+"\n\n"+r.Text)
		golden(t, "registrant_"+l+".html", r.HTML)
		other, err := Registrant(l, nil, true, confirmURL)
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
		r, _ := Registrant(l, FactsFor(dez, l), false, confirmURL)
		for _, typed := range []string{"Müller", "Anna", "Firma X", "Straße 1"} {
			if strings.Contains(r.Text, typed) || strings.Contains(r.HTML, typed) || strings.Contains(r.Subject, typed) {
				t.Errorf("%s registrant mail contains typed text %q", l, typed)
			}
		}
	}
}

func TestNoEarlyBirdOrAlumniInTheRegistrantMail(t *testing.T) {
	for _, l := range []string{"de", "en"} {
		r, _ := Registrant(l, FactsFor(dez, l), false, confirmURL)
		low := strings.ToLower(r.Text + r.HTML)
		for _, banned := range []string{"früh", "early", "alumni", "2.690", "2,690"} {
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
	r, _ := Registrant("en", FactsFor(e, "en"), false, confirmURL)
	if strings.Contains(r.HTML, "<script>") {
		t.Error("HTML part did not escape a course title")
	}
}

// When the course list could not be read, a real booking has no facts. It
// must not be told it chose "Sonstige"/"other".
func TestRegistrantWithoutFactsIsNotOther(t *testing.T) {
	for _, l := range []string{"de", "en"} {
		r, err := Registrant(l, nil, false, confirmURL)
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
		r, _ := Registrant(l, FactsFor(w, l), false, confirmURL)
		if !strings.Contains(r.Text, note) || !strings.Contains(r.HTML, note) {
			t.Errorf("%s: waitlist date without a waiting-list note:\n%s", l, r.Text)
		}
		open, _ := Registrant(l, FactsFor(dez, l), false, confirmURL)
		if strings.Contains(open.Text, note) {
			t.Errorf("%s: an open date mentions the waiting list", l)
		}
	}
}
