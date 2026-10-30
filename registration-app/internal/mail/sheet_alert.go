package mail

import (
	"bytes"
	"strings"
	"time"
)

// SheetProblem is a refusal from Google that a person has to fix (package
// sheet, Refused): the key is no longer valid, or the spreadsheet is not
// reachable for the service account.
type SheetProblem struct {
	At      time.Time
	ID      string // the registration whose rows were refused; empty for "key"
	Stage   string // "token": the key; "append": the spreadsheet; "key": GOOGLE_SA_KEY unreadable at start
	Status  int
	Detail  string // Google's answer, cut short
	Account string // the service account's address
	SheetID string
	Tab     string
}

// SheetAlert renders the mail that goes to the back office at once when the
// sheet refuses, with what to do about it. German only, like the summary:
// it goes to the back office, not to registrants.
func SheetAlert(p SheetProblem) (Rendered, error) {
	// The Cloud project is the domain part of the account's address:
	// <name>@<project>.iam.gserviceaccount.com.
	_, domain, _ := strings.Cut(p.Account, "@")
	project, _, _ := strings.Cut(domain, ".")
	data := map[string]any{
		"At": long(p.At), "ID": p.ID, "Stage": p.Stage, "Status": p.Status, "Detail": p.Detail,
		"Account": p.Account, "Project": project, "SheetID": p.SheetID, "Tab": p.Tab,
	}
	var t bytes.Buffer
	if err := textT.ExecuteTemplate(&t, "sheet_alert_de.txt", data); err != nil {
		return Rendered{}, err
	}
	why := "Zugriff verweigert"
	switch p.Stage {
	case "token":
		why = "Schlüssel ungültig"
	case "key":
		why = "Schlüssel unlesbar"
	}
	return Rendered{Subject: "[trainings.arc42.org] ACHTUNG: Google Sheet nicht beschreibbar (" + why + ")", Text: t.String()}, nil
}
