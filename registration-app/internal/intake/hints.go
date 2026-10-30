package intake

import (
	"strings"
	"unicode"
)

// Hint keys. The mail package words them per language. A hint never blocks a
// registration on its own: each one is a signal a person glances at. The
// scored ones (spam signals, see signals) drop a submission once two of them
// come together (spec 2026-10-25, section 6).
const (
	HintGmailDots     = "gmail-dots"     // a.b.c.d@gmail.com, the probing pattern seen in the spam log
	HintOddCase       = "odd-case"       // xKqTvBnM-style random strings
	HintSameName      = "same-name"      // first name equals last name
	HintSameNames     = "same-names"     // the registrant's two names equal the first participant's two
	HintShortBilling  = "short-billing"  // billing address too short or a single word to send an invoice to
	HintDoubleTLD     = "double-tld"     // e-mail domain like x.com.de
	HintNoScript      = "no-script"      // js_ok came back empty: the page's script did not run
	HintSeveralEmails = "several-emails" // the Email field held more than one address
	HintClosed        = "closed"         // the date was closed after the page was loaded
	HintFeedDown      = "feed-down"      // the course list could not be read; code not checked
	HintRecipientCap  = "recipient-cap"  // the confirmation mail was withheld: too many to this address today
	HintWaitlist      = "waitlist"       // the date is fully booked; the registrant mail says so
)

// dropAt is the number of spam signals that drops a submission silently. One
// signal alone stays a hint: each of them also fits some real person (a double
// name, a script blocker, a terse billing address).
const dropAt = 2

// signals returns the scored spam signals, one point each.
func signals(r Registration, jsOK bool) []string {
	var hs []string
	for _, e := range r.Emails {
		local, domain, _ := strings.Cut(e, "@")
		if (domain == "gmail.com" || domain == "googlemail.com") && strings.Count(local, ".") >= 3 {
			hs = append(hs, HintGmailDots)
			break
		}
	}
	for _, n := range r.names() {
		if oddCase(n) {
			hs = append(hs, HintOddCase)
			break
		}
	}
	if r.FirstName != "" && strings.EqualFold(r.FirstName, r.LastName) {
		hs = append(hs, HintSameName)
	}
	if len(r.Participants) > 0 && sameNames(r.LastName, r.FirstName, r.Participants[0].Last, r.Participants[0].First) {
		hs = append(hs, HintSameNames)
	}
	if len([]rune(r.Billing)) < 10 || len(strings.Fields(r.Billing)) < 2 {
		hs = append(hs, HintShortBilling)
	}
	for _, e := range r.Emails {
		if doubleTLD(e) {
			hs = append(hs, HintDoubleTLD)
			break
		}
	}
	if !jsOK {
		hs = append(hs, HintNoScript)
	}
	return hs
}

func sameNames(first string, rest ...string) bool {
	for _, n := range rest {
		if !strings.EqualFold(first, n) {
			return false
		}
	}
	return first != ""
}

// hintsFor returns the hints that are information, not spam signals.
func hintsFor(r Registration) []string {
	var hs []string
	if len(r.Emails) > 1 {
		hs = append(hs, HintSeveralEmails)
	}
	return hs
}

// hasLink finds what a person would paste as a link. Bare domains ("arc42.de")
// are not matched: they turn up in real company names and addresses.
func hasLink(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "http://") || strings.Contains(l, "https://") || strings.Contains(l, "www.")
}

// genericSLD are labels that sit under a country code in throwaway domains
// such as x.com.de or x.net.pl.
var genericSLD = map[string]bool{"com": true, "net": true, "org": true, "co": true, "info": true, "biz": true}

// realSLD are the country registries where such a second level is the normal
// way to register (x.co.uk, x.com.br); an address there scores nothing.
var realSLD = map[string]bool{
	"co.uk": true, "org.uk": true, "com.au": true, "net.au": true, "org.au": true,
	"com.br": true, "net.br": true, "org.br": true, "co.nz": true, "org.nz": true,
	"co.jp": true, "com.cn": true, "com.hk": true, "com.tw": true, "com.sg": true,
	"co.in": true, "co.za": true, "co.il": true, "co.kr": true, "com.mx": true,
	"com.ar": true, "com.tr": true, "com.my": true, "co.id": true, "com.ua": true,
}

// doubleTLD reports an address whose domain ends in a generic label under a
// country code, outside the registries that use that shape.
func doubleTLD(email string) bool {
	_, domain, _ := strings.Cut(email, "@")
	labels := strings.Split(domain, ".")
	if len(labels) < 3 {
		return false
	}
	sld, tld := labels[len(labels)-2], labels[len(labels)-1]
	return len(tld) == 2 && genericSLD[sld] && !realSLD[sld+"."+tld]
}

// oddCase flags a word whose letters switch between upper and lower case four
// or more times. "McDonald" switches three times and passes.
func oddCase(s string) bool {
	for _, word := range strings.Fields(s) {
		switches, prevUpper, seen := 0, false, false
		for _, r := range word {
			if !unicode.IsLetter(r) {
				continue
			}
			up := unicode.IsUpper(r)
			if seen && up != prevUpper {
				switches++
			}
			prevUpper, seen = up, true
		}
		if switches >= 4 {
			return true
		}
	}
	return false
}
