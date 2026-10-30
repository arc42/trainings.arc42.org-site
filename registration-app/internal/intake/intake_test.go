package intake

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"arc42-registration/internal/feed"
)

type fakeFeed map[string]feed.Result

func (f fakeFeed) Lookup(_ context.Context, code string) (feed.Entry, feed.Result) {
	res, ok := f[code]
	if !ok {
		return feed.Entry{}, feed.Unknown
	}
	status := "open"
	if code == "27-03 MSA" {
		status = "waitlist"
	}
	return feed.Entry{CourseShortTitle: "MSA", Date: feed.Date{Code: code, Status: status}}, res
}

func checker() *Checker {
	return &Checker{
		Feed:           fakeFeed{"26-12 MSA": feed.Bookable, "26-09 MSA-EN": feed.Closed, "27-03 MSA": feed.Bookable},
		Limiter:        NewLimiter(5, time.Hour, time.Now),
		AllowedOrigins: []string{"https://trainings.arc42.org"},
	}
}

func germanForm() url.Values {
	return url.Values{
		"Nachname": {"Müller"}, "Vorname": {"Anna"}, "Email": {"anna@example.org"},
		"Kurs": {"26-12 MSA"}, "Rechnungsadresse": {"Firma X\nStraße 1\n12345 Ort"},
		"Bemerkungen": {"PO 4711"}, "language": {"de"}, "via": {"arc42.de"},
		"form_source": {"https://trainings.arc42.org/anmeldung/"},
		"_gotcha":     {""}, "company_website": {""}, "js_ok": {"1"},
	}
}

func TestAcceptsARealGermanRegistration(t *testing.T) {
	d := checker().Check(context.Background(), Input{Form: germanForm(), Origin: "https://trainings.arc42.org", IP: "1.2.3.4"})
	if d.Outcome != Accept {
		t.Fatalf("outcome %v (%s), want Accept", d.Outcome, d.Reason)
	}
	r := d.Reg
	if r.Lang != "de" || r.LastName != "Müller" || r.Emails[0] != "anna@example.org" || r.Via != "arc42.de" {
		t.Errorf("registration = %+v", r)
	}
	if !d.Found || len(d.Hints) != 0 {
		t.Errorf("found=%v hints=%v", d.Found, d.Hints)
	}
}

func TestReadsTheEnglishFieldNames(t *testing.T) {
	f := url.Values{
		"last name": {"Smith"}, "first name": {"Jo"}, "Email": {"jo@example.org"},
		"Kurs": {"26-12 MSA"}, "Billing address": {"ACME Ltd, London"}, "Comments": {"hi"}, "language": {"en"},
		"js_ok": {"1"},
	}
	d := checker().Check(context.Background(), Input{Form: f, IP: "1.2.3.4"})
	if d.Outcome != Accept || d.Reg.Lang != "en" || d.Reg.LastName != "Smith" || d.Reg.Billing != "ACME Ltd, London" || d.Reg.Comments != "hi" {
		t.Fatalf("decision = %+v", d)
	}
}

func TestOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(f url.Values, in *Input)
		want   Outcome
		reason string
	}{
		{"foreign origin", func(f url.Values, in *Input) { in.Origin = "https://evil.example" }, Drop, "origin"},
		{"no origin header is fine", func(f url.Values, in *Input) { in.Origin = "" }, Accept, ""},
		{"Origin null from a no-referrer page is fine", func(f url.Values, in *Input) { in.Origin = "null" }, Accept, ""},
		{"gotcha honeypot", func(f url.Values, in *Input) { f.Set("_gotcha", "x") }, Drop, "honeypot"},
		{"custom honeypot", func(f url.Values, in *Input) { f.Set("company_website", "http://x") }, Drop, "honeypot"},
		{"unknown code", func(f url.Values, in *Input) { f.Set("Kurs", "99-99 FAKE") }, Drop, "unknown code"},
		{"closed code still goes through", func(f url.Values, in *Input) { f.Set("Kurs", "26-09 MSA-EN") }, Accept, ""},
		{"other", func(f url.Values, in *Input) { f.Set("Kurs", "sonstige") }, Accept, ""},
		{"no last name", func(f url.Values, in *Input) { f.Del("Nachname") }, Reject, "required"},
		{"no billing address", func(f url.Values, in *Input) { f.Set("Rechnungsadresse", "  ") }, Reject, "required"},
		{"bad email", func(f url.Values, in *Input) { f.Set("Email", "anna@localhost") }, Reject, "email"},
		{"display name in email", func(f url.Values, in *Input) { f.Set("Email", "Anna <anna@example.org>") }, Reject, "email"},
		{"comments too long", func(f url.Values, in *Input) { f.Set("Bemerkungen", strings.Repeat("x", 4001)) }, Reject, "too long"},
		{"4000 characters of umlauts are fine", func(f url.Values, in *Input) { f.Set("Bemerkungen", strings.Repeat("ü", 4000)) }, Accept, ""},
		{"link in comments", func(f url.Values, in *Input) { f.Set("Bemerkungen", "see https://forum.example/t/1") }, Reject, "link"},
		{"www in comments", func(f url.Values, in *Input) { f.Set("Bemerkungen", "www.forum.example") }, Reject, "link"},
		{"link in billing", func(f url.Values, in *Input) { f.Set("Rechnungsadresse", "Firma X\nhttp://x.example") }, Reject, "link"},
		{"link in a name", func(f url.Values, in *Input) { f.Set("VornameTN", "HTTPS://spam.example") }, Reject, "name"},
		{"link in a later participant", func(f url.Values, in *Input) {
			f["NachnameTN"] = []string{"Meier", "www.spam.example"}
			f["VornameTN"] = []string{"Eva", "Tom"}
		}, Reject, "name"},
		{"link in the company", func(f url.Values, in *Input) { f.Set("Firma", "http://spam.example") }, Reject, "company"},
		{"ten participants are fine", func(f url.Values, in *Input) { f["NachnameTN"] = strings.Split(strings.Repeat("Meier,", 10)[:59], ",") }, Accept, ""},
		{"eleven participants come from a script", func(f url.Values, in *Input) { f["NachnameTN"] = strings.Split(strings.Repeat("Meier,", 11)[:65], ",") }, Reject, "too many"},
		{"a later participant's name too long", func(f url.Values, in *Input) { f["NachnameTN"] = []string{"Meier", strings.Repeat("x", 201)} }, Reject, "too long"},
		{"a bare domain is a company name", func(f url.Values, in *Input) { f.Set("Rechnungsadresse", "arc42.de GmbH\nStraße 1") }, Accept, ""},
		{"one signal is a hint", func(f url.Values, in *Input) { f.Del("js_ok") }, Accept, ""},
		{"two signals drop", func(f url.Values, in *Input) { f.Del("js_ok"); f.Set("Rechnungsadresse", "Cusco") }, Drop, "score"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := germanForm()
			in := Input{Form: f, Origin: "https://trainings.arc42.org", IP: "1.2.3.4"}
			c.mutate(f, &in)
			d := checker().Check(context.Background(), in)
			if d.Outcome != c.want || !strings.Contains(d.Reason, c.reason) {
				t.Errorf("outcome %v reason %q, want %v containing %q", d.Outcome, d.Reason, c.want, c.reason)
			}
		})
	}
}

// The form posts every participant block under the same four names, empty
// fields included, so the n-th values are one person. An empty block is
// skipped; a salutation outside the select's values is discarded.
func TestPairsTheParticipantBlocks(t *testing.T) {
	f := germanForm()
	f["AnredeTN"] = []string{"Frau", "", "Herr", "Dr."}
	f["NachnameTN"] = []string{"Meier", "", "Kurz", "Lang"}
	f["VornameTN"] = []string{"Eva", "", "", "Tim"}
	f["EmailTN"] = []string{"eva@example.org", "", "kurz@example.org", ""}
	f.Set("Firma", "Beispiel GmbH")
	d := checker().Check(context.Background(), Input{Form: f, IP: "1.2.3.4"})
	if d.Outcome != Accept {
		t.Fatalf("outcome %v (%s)", d.Outcome, d.Reason)
	}
	want := []Participant{
		{Salutation: "Frau", Last: "Meier", First: "Eva", Email: "eva@example.org"},
		{Salutation: "Herr", Last: "Kurz", Email: "kurz@example.org"},
		{Last: "Lang", First: "Tim"},
	}
	if fmt.Sprint(d.Reg.Participants) != fmt.Sprint(want) || d.Reg.Company != "Beispiel GmbH" {
		t.Errorf("participants %+v company %q, want %+v", d.Reg.Participants, d.Reg.Company, want)
	}
}

func TestNoParticipantBlockMeansNoParticipants(t *testing.T) {
	f := germanForm()
	f["AnredeTN"], f["NachnameTN"], f["VornameTN"], f["EmailTN"] = []string{"Frau"}, []string{""}, []string{""}, []string{""}
	d := checker().Check(context.Background(), Input{Form: f, IP: "1.2.3.4"})
	if d.Outcome != Accept || len(d.Reg.Participants) != 0 {
		t.Errorf("outcome %v participants %+v", d.Outcome, d.Reg.Participants)
	}
}

// The body cap must admit a form with every participant block at its limit.
func TestMaxFormBytesCoversTenFullParticipants(t *testing.T) {
	f := germanForm()
	for _, k := range []string{"NachnameTN", "VornameTN"} {
		f[k] = strings.Split(strings.Repeat(strings.Repeat("ü", 200)+",", MaxParticipants-1)+strings.Repeat("ü", 200), ",")
	}
	if n := int64(len(f.Encode())); n > MaxFormBytes() {
		t.Errorf("a legal form encodes to %d bytes, cap is %d", n, MaxFormBytes())
	}
}

func TestClosedAndFeedDownAreFlagged(t *testing.T) {
	c := checker()
	f := germanForm()
	f.Set("Kurs", "26-09 MSA-EN")
	if d := c.Check(context.Background(), Input{Form: f, IP: "a"}); !has(d.Hints, HintClosed) {
		t.Errorf("hints %v lack %s", d.Hints, HintClosed)
	}
	c.Feed = fakeFeed{"26-12 MSA": feed.Unavailable}
	d := c.Check(context.Background(), Input{Form: germanForm(), IP: "b"})
	if d.Outcome != Accept || !has(d.Hints, HintFeedDown) || d.Found {
		t.Errorf("feed down: %+v", d)
	}
}

// While the feed is down every code is accepted unchecked, so nothing else
// stops a control character in a forged code from reaching the back-office
// subject line. No real booking code contains one.
func TestAControlCharacterInTheCodeIsDroppedEvenWhileTheFeedIsDown(t *testing.T) {
	c := checker()
	for _, code := range []string{"26-12 MSA\r\nX", "26-12 MSA\x07", "26-12\tMSA"} {
		c.Feed = fakeFeed{code: feed.Unavailable}
		f := germanForm()
		f.Set("Kurs", code)
		d := c.Check(context.Background(), Input{Form: f, IP: fmt.Sprint(len(code))})
		if d.Outcome != Drop || !strings.Contains(d.Reason, "control") {
			t.Errorf("code %q: outcome %v (%s), want Drop for a control character", code, d.Outcome, d.Reason)
		}
	}
}

// Every field at its limit, in 4-byte characters, is a legal submission. Its
// form encoding is 12 bytes per character, so the body cap has to follow the
// field limits or it turns a legal registration into a bare 413.
func TestMaxFormBytesCoversEveryFieldAtItsLimit(t *testing.T) {
	f := url.Values{}
	for key, max := range maxLen {
		f.Set(fieldNames[key][0], strings.Repeat("𝄞", max))
	}
	if n := len(f.Encode()); int64(n) > MaxFormBytes() {
		t.Errorf("a legal form encodes to %d bytes, cap is %d", n, MaxFormBytes())
	}
}

// Several addresses are allowed (the input has `multiple`), but only the
// first receives the registrant mail, and the back office is told.
func TestSeveralEmails(t *testing.T) {
	f := germanForm()
	f.Set("Email", "anna@example.org, Boss@Example.org")
	d := checker().Check(context.Background(), Input{Form: f, IP: "a"})
	if d.Outcome != Accept || len(d.Reg.Emails) != 2 || d.Reg.Emails[1] != "boss@example.org" || !has(d.Hints, HintSeveralEmails) {
		t.Errorf("decision = %+v", d)
	}
}

func TestRateLimitDropsTheSixthWithinAnHour(t *testing.T) {
	c := checker()
	for i := 0; i < 5; i++ {
		if d := c.Check(context.Background(), Input{Form: germanForm(), IP: "9.9.9.9"}); d.Outcome != Accept {
			t.Fatalf("submission %d: %v %s", i+1, d.Outcome, d.Reason)
		}
	}
	if d := c.Check(context.Background(), Input{Form: germanForm(), IP: "9.9.9.9"}); d.Outcome != Drop {
		t.Errorf("sixth: %v, want Drop", d.Outcome)
	}
	if d := c.Check(context.Background(), Input{Form: germanForm(), IP: "8.8.8.8"}); d.Outcome != Accept {
		t.Errorf("other IP: %v, want Accept", d.Outcome)
	}
}

func TestLimiterWindowSlides(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	l := NewLimiter(1, time.Hour, func() time.Time { return now })
	if !l.Allow("x") || l.Allow("x") {
		t.Fatal("first allowed, second not")
	}
	now = now.Add(time.Hour + time.Second)
	if !l.Allow("x") {
		t.Error("allowed again after the window")
	}
}

// Each signal alone, against a registration that scores nothing.
func TestSignals(t *testing.T) {
	clean := Registration{LastName: "Müller", FirstName: "Anna", Emails: []string{"anna@example.org"}, Billing: "Firma X, Straße 1, 12345 Ort"}
	cases := []struct {
		name   string
		mutate func(r *Registration)
		jsOK   bool
		want   string
	}{
		{"clean", func(r *Registration) {}, true, ""},
		{"dotted gmail", func(r *Registration) { r.Emails = []string{"a.b.c.d.e1@gmail.com"} }, true, HintGmailDots},
		{"one dot is normal", func(r *Registration) { r.Emails = []string{"firstname.lastname@gmail.com"} }, true, ""},
		{"random case", func(r *Registration) { r.FirstName = "xKqTvBnM" }, true, HintOddCase},
		{"McDonald is a name", func(r *Registration) { r.LastName = "McDonald" }, true, ""},
		{"first equals last", func(r *Registration) { r.FirstName = "müller" }, true, HintSameName},
		{"no first name is not equal", func(r *Registration) { r.FirstName = "" }, true, ""},
		{"short billing", func(r *Registration) { r.Billing = "Cusco, PE" }, true, HintShortBilling},
		{"single-word billing", func(r *Registration) { r.Billing = "Musterfirmenstraße" }, true, HintShortBilling},
		{"com.de", func(r *Registration) { r.Emails = []string{"x@mail.com.de"} }, true, HintDoubleTLD},
		{"net.pl", func(r *Registration) { r.Emails = []string{"x@y.net.pl"} }, true, HintDoubleTLD},
		{"co.uk is a registry", func(r *Registration) { r.Emails = []string{"x@firm.co.uk"} }, true, ""},
		{"com.br is a registry", func(r *Registration) { r.Emails = []string{"x@firm.com.br"} }, true, ""},
		{"subdomain is fine", func(r *Registration) { r.Emails = []string{"x@mail.firm.de"} }, true, ""},
		{"no script", func(r *Registration) {}, false, HintNoScript},
	}
	for _, c := range cases {
		r := clean
		c.mutate(&r)
		got := signals(r, c.jsOK)
		if c.want == "" && len(got) != 0 || c.want != "" && (len(got) != 1 || got[0] != c.want) {
			t.Errorf("%s: signals %v, want %q", c.name, got, c.want)
		}
	}
}

// Generators fill every name field from one string: that is two signals.
func TestFourIdenticalNamesScoreTwice(t *testing.T) {
	r := Registration{LastName: "Bob", FirstName: "Bob", Participants: []Participant{{Last: "Bob", First: "bob"}},
		Emails: []string{"bob@example.org"}, Billing: "Firma X, Straße 1, 12345 Ort"}
	if got := signals(r, true); len(got) != 2 || !has(got, HintSameName) || !has(got, HintSameNames) {
		t.Errorf("signals %v", got)
	}
}

// The submission that started the redesign (spec 2026-10-25, section 1). With
// its forum link it is rejected; a bot that learns to leave the link out is
// still dropped by the score.
func TestTheSpamSampleOf25October(t *testing.T) {
	sample := func() url.Values {
		return url.Values{
			"Nachname": {"AllenHeavy"}, "Vorname": {"AllenHeavy"}, "Email": {"allen@mailbox.com.de"},
			"Kurs": {"26-12 MSA"}, "Rechnungsadresse": {"Cusco"},
			"Bemerkungen": {"Great forum https://forum.example/t/42 see https://forum.example/t/42"},
			"language":    {"de"},
		}
	}
	d := checker().Check(context.Background(), Input{Form: sample(), IP: "s1"})
	if d.Outcome != Reject {
		t.Errorf("with the link: outcome %v (%s), want Reject", d.Outcome, d.Reason)
	}
	f := sample()
	f.Set("Bemerkungen", "Great forum")
	d = checker().Check(context.Background(), Input{Form: f, IP: "s2"})
	if d.Outcome != Drop || !strings.HasPrefix(d.Reason, "score") {
		t.Errorf("without the link: outcome %v (%s), want Drop by score", d.Outcome, d.Reason)
	}
}

// A real registration from someone with scripts off still goes through, and
// the back office sees why it carries a hint.
func TestAPlainRealRegistrationScoresZero(t *testing.T) {
	d := checker().Check(context.Background(), Input{Form: germanForm(), IP: "p1"})
	if d.Outcome != Accept || len(d.Hints) != 0 {
		t.Errorf("outcome %v, hints %v", d.Outcome, d.Hints)
	}
	f := germanForm()
	f.Del("js_ok")
	d = checker().Check(context.Background(), Input{Form: f, IP: "p2"})
	if d.Outcome != Accept || len(d.Hints) != 1 || d.Hints[0] != HintNoScript {
		t.Errorf("without script: outcome %v, hints %v", d.Outcome, d.Hints)
	}
}

func TestNewIDShape(t *testing.T) {
	re := regexp.MustCompile(`^R-[2-9A-HJKMNP-Z]{5}$`)
	for i := 0; i < 200; i++ {
		if id := NewID(); !re.MatchString(id) {
			t.Fatalf("NewID() = %q", id)
		}
	}
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// An IPv6 client usually controls a whole /64; counting each address
// separately would give it billions of budgets.
func TestIPv6AddressesShareTheirSlash64Budget(t *testing.T) {
	c := checker()
	for i := 1; i <= 5; i++ {
		ip := fmt.Sprintf("2001:db8:1:2::%d", i)
		if d := c.Check(context.Background(), Input{Form: germanForm(), IP: ip}); d.Outcome != Accept {
			t.Fatalf("submission %d from %s: %v %s", i, ip, d.Outcome, d.Reason)
		}
	}
	if d := c.Check(context.Background(), Input{Form: germanForm(), IP: "2001:db8:1:2::99"}); d.Outcome != Drop {
		t.Errorf("sixth from the same /64: %v, want Drop", d.Outcome)
	}
	if d := c.Check(context.Background(), Input{Form: germanForm(), IP: "2001:db8:1:3::1"}); d.Outcome != Accept {
		t.Errorf("another /64: %v, want Accept", d.Outcome)
	}
}

// The back office sees at a glance that a registration is for the waiting list.
func TestWaitlistRegistrationIsMarkedForTheBackOffice(t *testing.T) {
	f := germanForm()
	f.Set("Kurs", "27-03 MSA")
	d := checker().Check(context.Background(), Input{Form: f, IP: "a"})
	if d.Outcome != Accept || !has(d.Hints, HintWaitlist) {
		t.Errorf("decision = %+v", d)
	}
	if d := checker().Check(context.Background(), Input{Form: germanForm(), IP: "b"}); has(d.Hints, HintWaitlist) {
		t.Error("an open date is marked as waiting list")
	}
}

// "How did you hear about this course?" is optional: a choice from the
// dropdown plus free text, both field names readable in a Formspark
// notification too, one pair per language.
func TestReadsHowTheyHeardAboutTheCourse(t *testing.T) {
	c := checker()
	f := germanForm()
	f.Set("Aufmerksam durch", "Buch")
	f.Set("Aufmerksam durch (Details)", "arc42 by Example")
	d := c.Check(context.Background(), Input{Form: f, IP: "h1"})
	if d.Outcome != Accept || d.Reg.HeardVia != "Buch" || d.Reg.HeardViaDetail != "arc42 by Example" {
		t.Errorf("DE: outcome %v, heard via %q / %q", d.Outcome, d.Reg.HeardVia, d.Reg.HeardViaDetail)
	}
	f = germanForm()
	f.Set("language", "en")
	f.Set("Heard via", "Other")
	f.Set("Heard via (details)", "a podcast")
	d = c.Check(context.Background(), Input{Form: f, IP: "h2"})
	if d.Reg.HeardVia != "Other" || d.Reg.HeardViaDetail != "a podcast" {
		t.Errorf("EN: heard via %q / %q", d.Reg.HeardVia, d.Reg.HeardViaDetail)
	}
	if d := c.Check(context.Background(), Input{Form: germanForm(), IP: "h3"}); d.Outcome != Accept || d.Reg.HeardVia != "" {
		t.Errorf("without the fields: outcome %v, heard via %q", d.Outcome, d.Reg.HeardVia)
	}
	f = germanForm()
	f.Set("Aufmerksam durch (Details)", strings.Repeat("x", 201))
	if d := c.Check(context.Background(), Input{Form: f, IP: "h4"}); d.Outcome != Reject {
		t.Errorf("a 201-character detail: outcome %v, want Reject", d.Outcome)
	}
}
