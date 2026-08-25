package web

import (
	"fmt"
	"html"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"

	"arc42-trainings-admin/internal/gh"
	"arc42-trainings-admin/internal/validate"
)

func unifiedDiff(before, after []byte) string {
	edits := myers.ComputeEdits(span.URIFromPath(dataPath), string(before), string(after))
	return fmt.Sprint(gotextdiff.ToUnified(dataPath, dataPath, string(before), edits))
}

// safeDiff escapes the diff for embedding inside a <pre> block ourselves,
// using package "html" rather than html/template's auto-escaper. The
// auto-escaper additionally replaces "+" with "&#43;" (a defense against
// legacy UTF-7 charset sniffing), which would mangle every added line of a
// unified diff. We still need &, <, >, etc. escaped, since diff lines come
// from user-supplied training data — so we do that escaping explicitly and
// mark the result template.HTML so it is not escaped a second time.
func safeDiff(diff string) template.HTML {
	return template.HTML(html.EscapeString(diff))
}

func prTitle(changes []Change) string {
	if len(changes) == 1 {
		return "Training dates: 1 change"
	}
	return fmt.Sprintf("Training dates: %d changes", len(changes))
}

func prBody(changes []Change, login string) string {
	var b strings.Builder
	b.WriteString("Opened from the trainings admin app by @" + login + ".\n\n")
	for _, c := range changes {
		b.WriteString(fmt.Sprintf("- **%s** `%s` — %s\n", c.Kind, c.DateID, c.Summary))
	}
	b.WriteString("\nCI validates this against `api/trainings.schema.json` and " +
		"`scripts/validate_trainings.rb`. Merging republishes the feed and " +
		"notifies the four consumer sites.\n")
	return b.String()
}

var unsafeRef = regexp.MustCompile(`[^a-z0-9-]+`)

// branchName builds the ref a proposal is pushed to. The date and the slug are
// there to make the PR list readable; the random tail is there to make the name
// unique.
//
// Uniqueness is not cosmetic. GitHub answers POST /git/refs for an existing ref
// with 422 "Reference already exists", which aborts the whole proposal before
// the commit and the PR — so without the tail, the *second* proposal of the day
// about the same date is simply impossible. That is not an exotic sequence: fix
// a typo on a date in the morning, remove the date in the afternoon, and the
// removal is refused. Deriving the tail from the clock instead only narrows the
// window; two maintainers can still submit within the same second.
func branchName(now time.Time, changes []Change) string {
	slug := "edit"
	if len(changes) > 0 {
		slug = unsafeRef.ReplaceAllString(strings.ToLower(changes[0].DateID), "-")
		slug = strings.Trim(slug, "-")
	}
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	// A DateID that slugs down to nothing (all punctuation) would otherwise
	// leave an empty segment and a "--" run in the middle of the ref.
	if slug == "" {
		slug = "edit"
	}
	return fmt.Sprintf("trainings-admin/%s-%s-%s", now.Format("2006-01-02"), slug, newID()[:6])
}

func (s *Server) handlePropose(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, ok := s.drafts.Get(sess.ID)
	if !ok || !d.Dirty() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	before, _, _, err := client.ReadFile(r.Context(), dataPath)
	if err != nil {
		s.fail(w, "could not read the current file from GitHub", err)
		return
	}
	problems := validate.Rules(d.Doc.Model())
	problems = append(problems, s.schemaProblems(r, client, d)...)

	s.render(w, "propose.gohtml", map[string]any{
		"Title": "Review & propose", "Draft": d, "Login": sess.Login,
		"Diff":     safeDiff(unifiedDiff(before, d.Doc.Bytes())),
		"Problems": problems,
		"PRTitle":  prTitle(d.Changes),
		"PRBody":   prBody(d.Changes, sess.Login),
	})
}

// schemaProblems validates against the repository's own schema, fetched live so
// it cannot drift from what CI will enforce.
func (s *Server) schemaProblems(r *http.Request, client *gh.Client, d *Draft) []validate.Problem {
	schema, _, _, err := client.ReadFile(r.Context(), schemaPath)
	if err != nil {
		return []validate.Problem{{Message: "could not fetch the schema: " + err.Error()}}
	}
	feed, err := validate.FeedJSON(d.Doc.Model())
	if err != nil {
		return []validate.Problem{{Message: "could not render the feed: " + err.Error()}}
	}
	problems, err := validate.Schema(schema, feed)
	if err != nil {
		return []validate.Problem{{Message: err.Error()}}
	}
	return problems
}

func (s *Server) handleProposeSubmit(w http.ResponseWriter, r *http.Request, sess Session, client *gh.Client) {
	d, ok := s.drafts.Get(sess.ID)
	if !ok || !d.Dirty() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.fail(w, "could not read the form", err)
		return
	}
	// Optimistic concurrency: if the blob moved since the draft was loaded,
	// somebody else changed the file. Never overwrite silently — and never
	// discard the draft, which is the user's only copy.
	_, fileSHA, headSHA, err := client.ReadFile(r.Context(), dataPath)
	if err != nil {
		s.fail(w, "could not re-read the file from GitHub", err)
		return
	}
	if fileSHA != d.FileSHA {
		w.WriteHeader(http.StatusConflict)
		s.render(w, "conflict.gohtml", map[string]any{
			"Title": "The file changed on GitHub", "Draft": d, "Login": sess.Login,
		})
		return
	}
	if problems := validate.Rules(d.Doc.Model()); len(problems) > 0 {
		s.render(w, "propose.gohtml", map[string]any{
			"Title": "Fix these first", "Draft": d, "Login": sess.Login,
			"Problems": problems, "PRTitle": r.PostFormValue("title"), "PRBody": r.PostFormValue("body"),
		})
		return
	}

	url, err := client.OpenPR(r.Context(), gh.PRRequest{
		Branch:  branchName(time.Now(), d.Changes),
		Path:    dataPath,
		Content: string(d.Doc.Bytes()),
		BaseSHA: headSHA,
		FileSHA: fileSHA,
		Title:   r.PostFormValue("title"),
		Body:    r.PostFormValue("body"),
	})
	if err != nil {
		s.fail(w, "could not open the pull request", err)
		return
	}
	s.drafts.Discard(sess.ID)
	s.render(w, "published.gohtml", map[string]any{
		"Title": "Pull request opened", "URL": url, "Login": sess.Login,
	})
}
