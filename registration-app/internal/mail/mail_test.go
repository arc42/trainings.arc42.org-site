package mail

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"arc42-registration/internal/feed"
	"arc42-registration/internal/intake"
	"arc42-registration/internal/store"
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

func TestConfirmedMails(t *testing.T) {
	reg := intake.Registration{
		ID: "R-7F3KQ", LastName: "Müller", FirstName: "Anna", Email: "anna@example.org",
		Emails: []string{"anna@example.org"}, Code: "26-12 MSA", Billing: "Firma X\nStraße 1\n12345 Ort", Comments: "",
		Company: "Firma X", Participants: []intake.Participant{
			{Salutation: "Frau", Last: "Meier", First: "Eva", Email: "eva@example.org"}, {Last: "Lang", First: "Tim"}},
		HeardVia: "Buch", HeardViaDetail: "arc42 by Example",
		Via: "arc42.de", FormSource: "https://trainings.arc42.org/anmeldung/",
	}
	at := time.Date(2026, 10, 26, 13, 3, 0, 0, time.UTC)
	for _, l := range []string{"de", "en"} {
		reg.Lang = l
		c := token.Claims{ID: "R-7F3KQ", Code: "26-12 MSA", Email: "anna@example.org", LastName: "Müller", Lang: l}
		full, err := Confirmed(c, Confirmation{Reg: &reg, Facts: FactsFor(dez, l), Hints: []string{intake.HintGmailDots, intake.HintClosed},
			CorrectedFrom: "anna@exmaple.org", At: at, Via: "code"})
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "confirmed_"+l+".txt", full.Subject+"\n\n"+full.Text)
		want := map[string]string{"de": "[trainings.arc42.org] ANMELDUNG R-7F3KQ 26-12 MSA (BESTÄTIGT)",
			"en": "[trainings.arc42.org] REGISTRATION R-7F3KQ 26-12 MSA (CONFIRMED)"}[l]
		if full.Subject != want {
			t.Errorf("subject %q, want %q: filters match on it", full.Subject, want)
		}
		for _, missing := range []string{"down", "absent"} {
			short, err := Confirmed(c, Confirmation{Missing: missing, Facts: FactsFor(dez, l), At: at, Via: "link"})
			if err != nil {
				t.Fatal(err)
			}
			golden(t, "confirmed_"+missing+"_"+l+".txt", short.Subject+"\n\n"+short.Text)
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
func TestConfirmedMailSaysHowTheyHeard(t *testing.T) {
	reg := intake.Registration{ID: "R-7F3KQ", LastName: "Müller", Email: "anna@example.org", Code: "26-12 MSA",
		Billing: "X", HeardVia: "Buch", HeardViaDetail: "arc42 by Example"}
	for l, want := range map[string]string{
		"de": "Aufmerksam durch:  Buch (arc42 by Example)",
		"en": "Heard via:                Buch (arc42 by Example)",
	} {
		reg.Lang = l
		r, _ := Confirmed(token.Claims{ID: reg.ID, Lang: l}, Confirmation{Reg: &reg, Facts: FactsFor(dez, l)})
		if !strings.Contains(r.Text, want) {
			t.Errorf("%s: missing %q in\n%s", l, want, r.Text)
		}
	}
	reg.HeardVia, reg.HeardViaDetail, reg.Lang = "", "", "de"
	if r, _ := Confirmed(token.Claims{ID: reg.ID}, Confirmation{Reg: &reg}); strings.Contains(r.Text, "Aufmerksam durch") {
		t.Error("an empty answer still printed the line")
	}
}

// People confirm their e-mail address, not their registration: the
// registration is what they sent; the address is what we check (Gernot,
// 29 Sep 2026). And a waitlist registration hears that we will get in
// touch about the next steps, not a promise of a free seat.
func TestRegistrantMailAsksToConfirmTheAddress(t *testing.T) {
	for l, want := range map[string][]string{
		"de": {"E-Mail-Adresse", "um das weitere Vorgehen zu klären"},
		"en": {"e-mail address", "to agree on the next steps"},
	} {
		w := dez
		w.Status = "waitlist"
		r, _ := Registrant(l, FactsFor(w, l), false, confirmURL, "482913")
		all := r.Subject + r.Text + r.HTML
		for _, s := range want {
			if !strings.Contains(all, s) {
				t.Errorf("%s: missing %q", l, s)
			}
		}
		for _, banned := range []string{"Anmeldung bestätigen", "bestätigen Sie Ihre Anmeldung", "confirm your registration", "Confirm registration", "sobald ein Platz", "as soon as a seat"} {
			if strings.Contains(all, banned) {
				t.Errorf("%s: still says %q", l, banned)
			}
		}
	}
}

func TestSummaryMail(t *testing.T) {
	mon := time.Date(2026, 10, 26, 6, 0, 30, 0, time.UTC) // 07:00:30 in Berlin
	at := func(d time.Duration) time.Time { return mon.Add(-d) }
	rows := []store.Record{ // sorted below, as store.All returns them
		{Reg: intake.Registration{ID: "R-DROP1", Code: "26-12 MSA", LastName: "AllenHeavy", FirstName: "AllenHeavy",
			Email: "allen@mail.com.de", Billing: "Cusco"}, Status: store.StatusDropped,
			DropReason: "score: same-name,short-billing,double-tld", CreatedAt: at(140 * time.Hour)},
		{Reg: intake.Registration{ID: "R-OLD01", Code: "26-12 MSA", LastName: "Weber", FirstName: "Eva",
			Email: "eva@example.org", Billing: "Weber GmbH\nHauptstr. 3"}, Status: store.StatusConfirmed,
			CreatedAt: at(9 * 24 * time.Hour), ConfirmedAt: at(130 * time.Hour), ReportedAt: at(7 * 24 * time.Hour)},
		{Reg: intake.Registration{ID: "R-CONF1", Code: "26-12 MSA", LastName: "Müller", FirstName: "Anna",
			Email: "anna@example.org", Billing: "Firma X\nStraße 1\n12345 Ort",
			Comments: "Bitte Rechnung an die Einkaufsabteilung, Bestellnummer folgt. Siehe auch https://example.org/po und www.example.org"},
			Hints: []string{"several-emails"}, Status: store.StatusConfirmed, CreatedAt: at(100 * time.Hour),
			ConfirmedAt: at(99 * time.Hour), ConfirmedVia: "code"},
		{Reg: intake.Registration{ID: "R-PEND1", Code: "sonstige", LastName: "Smith", Email: "jo@example.com",
			Billing: "ACME Ltd, 1 Main Street, London"}, Status: store.StatusPending, CreatedAt: at(30 * time.Hour)},
		{Reg: intake.Registration{ID: "R-PEND2", Code: "26-12 MSA", LastName: "Klein", FirstName: "Ute",
			Email: "ute@example.de"}, Status: store.StatusPending, MailFailed: true, CreatedAt: at(6 * 24 * time.Hour)},
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	r, err := Summary(Week{Number: 44, From: time.Date(2026, 10, 19, 5, 0, 30, 0, time.UTC), To: mon, Due: mon, Rows: rows,
		NoSheet: map[string]string{"R-CONF1": "retry", "R-OLD01": "manual"}})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "summary_de.txt", r.Subject+"\n\n"+r.Text)
	if strings.Contains(r.Text, "http") || strings.Contains(r.Text, "www.") || strings.Contains(r.Text, "—") {
		t.Error("a link or an em-dash in the summary")
	}

	empty, err := Summary(Week{Number: 44, To: mon.Add(26 * time.Hour), Due: mon, Late: true})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "summary_empty_de.txt", empty.Subject+"\n\n"+empty.Text)
}

// Each kind of refusal gets its own remedy; the key's is the long one.
func TestSheetAlertMails(t *testing.T) {
	at := time.Date(2026, 11, 3, 9, 15, 0, 0, time.UTC)
	for name, p := range map[string]SheetProblem{
		"sheet_alert_token_de.txt": {Stage: "token", Status: 400, Detail: `{"error":"invalid_grant","error_description":"Invalid JWT Signature."}`},
		"sheet_alert_key_de.txt":   {Stage: "key", Detail: "sheet: GOOGLE_SA_KEY is not JSON: invalid character 'x'"},
		"sheet_alert_403_de.txt":   {Stage: "append", Status: 403, Detail: `{"error":{"code":403,"message":"The caller does not have permission","status":"PERMISSION_DENIED"}}`},
	} {
		p.At, p.ID, p.SheetID, p.Tab = at, "R-7F3KQ", "1JtivdvHqR-test", "Tabelle1"
		if p.Stage != "key" { // unreadable key: no account known
			p.Account = "registration-sheet@arc42-registration-write-sheet.iam.gserviceaccount.com"
		}
		r, err := SheetAlert(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(r.Text, "—") {
			t.Errorf("%s: em-dash in a user-visible mail", name)
		}
		golden(t, name, r.Subject+"\n\n"+r.Text)
	}
}
