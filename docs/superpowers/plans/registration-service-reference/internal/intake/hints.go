package intake

import (
	"strings"
	"unicode"
)

// Hint keys. The mail package words them per language. Hints never block a
// registration (spec 4.1): each one is a signal a person glances at.
const (
	HintGmailDots     = "gmail-dots"     // a.b.c.d@gmail.com, the probing pattern seen in the spam log
	HintURLInName     = "url-in-name"    // a link where a name belongs
	HintOddCase       = "odd-case"       // xKqTvBnM-style random strings
	HintSeveralEmails = "several-emails" // the Email field held more than one address
	HintClosed        = "closed"         // the date was closed after the page was loaded
	HintFeedDown      = "feed-down"      // the course list could not be read; code not checked
)

func hintsFor(r Registration) []string {
	var hs []string
	for _, e := range r.Emails {
		local, domain, _ := strings.Cut(e, "@")
		if (domain == "gmail.com" || domain == "googlemail.com") && strings.Count(local, ".") >= 3 {
			hs = append(hs, HintGmailDots)
			break
		}
	}
	names := []string{r.LastName, r.FirstName, r.ParticipantLast, r.ParticipantFirst}
	for _, n := range names {
		l := strings.ToLower(n)
		if strings.Contains(l, "http://") || strings.Contains(l, "https://") || strings.Contains(l, "www.") {
			hs = append(hs, HintURLInName)
			break
		}
	}
	for _, n := range names {
		if oddCase(n) {
			hs = append(hs, HintOddCase)
			break
		}
	}
	if len(r.Emails) > 1 {
		hs = append(hs, HintSeveralEmails)
	}
	return hs
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
