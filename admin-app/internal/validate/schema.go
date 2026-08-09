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
	type jsonDate struct {
		ID       string   `json:"id"`
		Code     string   `json:"code"`
		Start    string   `json:"start"`
		End      string   `json:"end"`
		City     string   `json:"city,omitempty"`
		Country  string   `json:"country,omitempty"`
		Language string   `json:"language"`
		Format   string   `json:"format"`
		Trainers []string `json:"trainers,omitempty"`
		Pricing  string   `json:"pricing,omitempty"`
		FewSeats string   `json:"few_seats,omitempty"`
		URL      string   `json:"url"`
		Status   string   `json:"status"`
	}
	type jsonCourse struct {
		ID            string     `json:"id"`
		ShortTitle    string     `json:"short_title"`
		Title         string     `json:"title"`
		Blurb         string     `json:"blurb,omitempty"`
		Certification *string    `json:"certification"`
		Credits       *string    `json:"credits"`
		URL           string     `json:"url"`
		Trainers      []string   `json:"trainers"`
		Dates         []jsonDate `json:"dates"`
	}
	out := struct {
		Generated string       `json:"generated"`
		Courses   []jsonCourse `json:"courses"`
	}{Generated: time.Now().UTC().Format(time.RFC3339)}

	for _, c := range t.Courses {
		jc := jsonCourse{
			ID: c.ID, ShortTitle: c.ShortTitle, Title: c.Title, Blurb: c.Blurb,
			URL: c.URL, Trainers: c.Trainers,
		}
		if c.Certification != "" {
			v := c.Certification
			jc.Certification = &v
		}
		if c.Credits != "" {
			v := c.Credits
			jc.Credits = &v
		}
		for _, d := range c.Dates {
			jc.Dates = append(jc.Dates, jsonDate{
				ID: d.ID, Code: d.Code, Start: d.Start, End: d.End,
				City: d.City, Country: d.Country, Language: d.Language,
				Format: d.Format, Trainers: d.Trainers, Pricing: d.Pricing,
				FewSeats: d.FewSeats, URL: d.URL, Status: d.Status,
			})
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
