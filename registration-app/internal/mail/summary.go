package mail

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"time"

	"arc42-registration/internal/store"
	"arc42-registration/internal/token"
)

// Week is what one weekly summary covers (spec 2026-10-25, section 7).
type Week struct {
	Number   int       // ISO week, for the subject
	From, To time.Time // From is zero for the first summary ever
	// Due is when the summary was due; Late says it went out well after.
	Due  time.Time
	Late bool
	Rows []store.Record // every stored registration, oldest first
}

// summaryRow is one registration as the summary shows it: one line of
// facts, then short indented lines, never the whole registration.
type summaryRow struct {
	ID, When, Code, Name, Email, Billing, Comments string
	Hints, Reason, Confirmed, Link                 string
	MailFailed, Again                              bool
}

// Summary renders the weekly mail. German only: it goes to the back office,
// not to registrants. It is sent when every count is zero too, because a
// Monday without it is the alarm (spec 7.4).
func Summary(w Week) (Rendered, error) {
	groups := map[string][]summaryRow{}
	failed := 0
	for _, r := range w.Rows {
		row := summaryRow{
			ID: r.Reg.ID, When: short(r.CreatedAt), Code: orDash(r.Reg.Code),
			Name:  orDash(strings.Trim(r.Reg.LastName+", "+r.Reg.FirstName, ", ")),
			Email: orDash(r.Reg.Email), Billing: firstLine(r.Reg.Billing), Comments: excerpt(r.Reg.Comments, 80),
			Hints: strings.Join(r.Hints, ", "), Reason: r.DropReason,
			MailFailed: r.MailFailed, Again: !r.ReportedAt.IsZero(),
		}
		switch r.Status {
		case store.StatusConfirmed:
			row.Confirmed = short(r.ConfirmedAt) + map[string]string{"code": ", per Code", "link": ", per Link"}[r.ConfirmedVia]
		case store.StatusPending:
			if until := r.CreatedAt.Add(token.Valid); until.After(w.To) {
				row.Link = "gültig bis " + short(until)
			} else {
				row.Link = "abgelaufen, nicht bestätigt"
			}
			if r.MailFailed {
				failed++
			}
		}
		groups[r.Status] = append(groups[r.Status], row)
	}
	c, p, d := len(groups[store.StatusConfirmed]), len(groups[store.StatusPending]), len(groups[store.StatusDropped])
	data := map[string]any{
		"Number": w.Number, "To": long(w.To), "Late": w.Late, "Due": long(w.Due),
		"Confirmed": groups[store.StatusConfirmed], "Pending": groups[store.StatusPending], "Dropped": groups[store.StatusDropped],
		"C": c, "P": p, "D": d, "Failed": failed, "Empty": len(w.Rows) == 0,
	}
	if !w.From.IsZero() {
		data["From"] = long(w.From)
	}
	var t bytes.Buffer
	if err := textT.ExecuteTemplate(&t, "summary_de.txt", data); err != nil {
		return Rendered{}, err
	}
	subject := fmt.Sprintf("[trainings.arc42.org] Wochenübersicht Anmeldungen KW %d (%d bestätigt, %d offen, %d verworfen)", w.Number, c, p, d)
	return Rendered{Subject: subject, Text: t.String()}, nil
}

var weekday = [...]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}

func short(t time.Time) string {
	t = t.In(berlin)
	return weekday[t.Weekday()] + " " + t.Format("02.01. 15:04")
}

func long(t time.Time) string { return t.In(berlin).Format("02.01.2006, 15:04 Uhr") }

func orDash(s string) string {
	if s = strings.TrimSpace(s); s == "" {
		return "-"
	}
	return s
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i]) + " ..."
	}
	return orDash(s)
}

// linkish matches what a mail client would turn into a link. Intake rejects
// links in the fields shown here, so this is the second line of defence: the
// summary must never carry a clickable spam link into the back office.
var linkish = regexp.MustCompile(`(?i)http|www\.`)

func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + " ..."
	}
	s = linkish.ReplaceAllStringFunc(s, func(m string) string {
		if strings.EqualFold(m, "http") {
			return "hxxp"
		}
		return m[:3] + "[.]"
	})
	return orDash(s)
}
