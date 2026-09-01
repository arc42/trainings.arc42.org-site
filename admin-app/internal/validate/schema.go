package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"arc42-trainings-admin/internal/model"
)

// FeedJSON renders the model in the shape api/trainings.schema.json describes,
// so the repository's own schema can judge the draft before it becomes a PR.
func FeedJSON(t model.Trainings) ([]byte, error) {
	// NOTE the fields that are deliberately ABSENT: the deprecated prose keys
	// `pricing`, `credits` and `few_seats`. The published feed still carries
	// them (see api/trainings.json), generated per build, but they are optional
	// in the schema and reproducing them here would mean a second Go
	// implementation of German wording that already exists once in Liquid. Two
	// implementations of the same sentence drift; one does not. What this shim
	// exists to catch is a malformed *value* the app is about to write, and the
	// structured fields below are where those values now live.
	type jsonEarlyBird struct {
		Amount int    `json:"amount"`
		Until  string `json:"until"`
	}
	type jsonPrice struct {
		Amount    int            `json:"amount"`
		Currency  string         `json:"currency"`
		Alumni    int            `json:"alumni,omitempty"`
		EarlyBird *jsonEarlyBird `json:"early_bird,omitempty"`
	}
	type jsonCredits struct {
		Methodical    int `json:"methodical,omitempty"`
		Technical     int `json:"technical,omitempty"`
		Communication int `json:"communication,omitempty"`
	}
	type jsonDate struct {
		ID           string     `json:"id"`
		Code         string     `json:"code"`
		Start        string     `json:"start"`
		End          string     `json:"end"`
		City         string     `json:"city,omitempty"`
		Country      string     `json:"country,omitempty"`
		Language     string     `json:"language"`
		Format       string     `json:"format"`
		Trainers     []string   `json:"trainers,omitempty"`
		Price        *jsonPrice `json:"price,omitempty"`
		SeatsLimited bool       `json:"seats_limited,omitempty"`
		URL          string     `json:"url"`
		Status       string     `json:"status"`
	}
	type jsonCourse struct {
		ID            string       `json:"id"`
		ShortTitle    string       `json:"short_title"`
		Title         string       `json:"title"`
		Blurb         string       `json:"blurb,omitempty"`
		Certification *string      `json:"certification"`
		CreditPoints  *jsonCredits `json:"credit_points,omitempty"`
		URL           string       `json:"url"`
		URLEn         string       `json:"url_en,omitempty"`
		Trainers      []string     `json:"trainers"`
		Dates         []jsonDate   `json:"dates"`
	}
	out := struct {
		Generated string       `json:"generated"`
		Courses   []jsonCourse `json:"courses"`
	}{Generated: time.Now().UTC().Format(time.RFC3339)}

	for _, c := range t.Courses {
		jc := jsonCourse{
			ID: c.ID, ShortTitle: c.ShortTitle, Title: c.Title, Blurb: c.Blurb,
			URL: c.URL, URLEn: c.URLEn, Trainers: c.Trainers,
		}
		if c.Certification != "" {
			v := c.Certification
			jc.Certification = &v
		}
		if !c.CreditPoints.Empty() {
			jc.CreditPoints = &jsonCredits{
				Methodical:    c.CreditPoints.Methodical,
				Technical:     c.CreditPoints.Technical,
				Communication: c.CreditPoints.Communication,
			}
		}
		for _, d := range c.Dates {
			jd := jsonDate{
				ID: d.ID, Code: d.Code, Start: d.Start, End: d.End,
				City: d.City, Country: d.Country, Language: d.Language,
				Format: d.Format, Trainers: d.Trainers,
				SeatsLimited: d.SeatsLimited, URL: d.URL, Status: d.Status,
			}
			if d.Price != nil && d.Price.Amount > 0 {
				cur := d.Price.Currency
				if cur == "" {
					cur = "EUR"
				}
				jd.Price = &jsonPrice{Amount: d.Price.Amount, Currency: cur, Alumni: d.Price.Alumni}
				if eb := d.Price.EarlyBird; eb != nil && eb.Amount > 0 {
					jd.Price.EarlyBird = &jsonEarlyBird{Amount: eb.Amount, Until: eb.Until}
				}
			}
			jc.Dates = append(jc.Dates, jd)
		}
		out.Courses = append(out.Courses, jc)
	}
	return json.Marshal(out)
}

// Schema validates a rendered feed against the repository's draft-07 schema.
func Schema(schemaJSON, feedJSON []byte) ([]Problem, error) {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7
	if err := compiler.AddResource("trainings.schema.json", bytes.NewReader(schemaJSON)); err != nil {
		return nil, fmt.Errorf("load schema: %w", err)
	}
	sch, err := compiler.Compile("trainings.schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile schema: %w", err)
	}
	var doc any
	if err := json.Unmarshal(feedJSON, &doc); err != nil {
		return nil, fmt.Errorf("decode feed: %w", err)
	}
	if err := sch.Validate(doc); err != nil {
		var ve *jsonschema.ValidationError
		if ok := asValidationError(err, &ve); ok {
			return flatten(ve), nil
		}
		return []Problem{{Message: err.Error()}}, nil
	}
	return nil, nil
}

func asValidationError(err error, target **jsonschema.ValidationError) bool {
	ve, ok := err.(*jsonschema.ValidationError)
	if ok {
		*target = ve
	}
	return ok
}

// flatten turns the schema library's error tree into leaf problems, which are
// the ones that name an actual field.
func flatten(ve *jsonschema.ValidationError) []Problem {
	if len(ve.Causes) == 0 {
		return []Problem{{
			Field:   strings.TrimPrefix(ve.InstanceLocation, "/"),
			Message: ve.Message,
		}}
	}
	var out []Problem
	for _, c := range ve.Causes {
		out = append(out, flatten(c)...)
	}
	return out
}
