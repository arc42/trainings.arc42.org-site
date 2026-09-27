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
		"_gotcha":     {""}, "company_website": {""},
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
		"Kurs": {"26-12 MSA"}, "Billing address": {"ACME"}, "Comments": {"hi"}, "language": {"en"},
	}
	d := checker().Check(context.Background(), Input{Form: f, IP: "1.2.3.4"})
	if d.Outcome != Accept || d.Reg.Lang != "en" || d.Reg.LastName != "Smith" || d.Reg.Billing != "ACME" || d.Reg.Comments != "hi" {
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

func TestHints(t *testing.T) {
	cases := []struct {
		name string
		reg  Registration
		want string
	}{
		{"dotted gmail", Registration{Emails: []string{"a.b.c.d.e1@gmail.com"}}, HintGmailDots},
		{"one dot is normal", Registration{Emails: []string{"firstname.lastname@gmail.com"}}, ""},
		{"url in name", Registration{LastName: "see https://spam.example", Emails: []string{"a@b.de"}}, HintURLInName},
		{"random case", Registration{FirstName: "xKqTvBnM", Emails: []string{"a@b.de"}}, HintOddCase},
		{"McDonald is a name", Registration{LastName: "McDonald", Emails: []string{"a@b.de"}}, ""},
	}
	for _, c := range cases {
		got := hintsFor(c.reg)
		if c.want == "" && len(got) != 0 || c.want != "" && !has(got, c.want) {
			t.Errorf("%s: hints %v, want %q", c.name, got, c.want)
		}
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
