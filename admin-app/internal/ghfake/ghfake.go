// Package ghfake is a stand-in for the parts of the GitHub REST API this app
// uses: sign-in, the permission check, reading a file, and the three calls that
// open a pull request.
//
// It exists twice over. The handler tests run against it instead of the
// network, and `cmd/demo` runs the real app against it so the whole thing can
// be clicked through — and shown to somebody — with no credentials, no network
// and no repository to damage.
//
// One rule governs everything here: **the fake must refuse what GitHub
// refuses.** A double that is more permissive than the real API is worse than
// no double at all, because the suite then certifies a flow that cannot work.
// That is not hypothetical: an earlier fake accepted every POST /git/refs,
// GitHub answers 422 for a ref that already exists, and so a proposal that
// collided with an existing branch failed only in production. The refusals
// modelled here are marked "GitHub says no"; add to them rather than relaxing
// them.
package ghfake

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Proposal is one published editing session, as it reached the fake.
type Proposal struct {
	Branch  string
	Path    string
	Content string
	Title   string
	Body    string
	Number  int
	URL     string
}

type Options struct {
	// Repo is "owner/name"; requests for any other repository 404, as GitHub's
	// would. Defaults to "arc42/trainings.arc42.org-site".
	Repo string
	// Login and CanPush answer "who is this" and "may they push here".
	Login   string
	CanPush bool
	// Files is the repository content, keyed by path. The app reads
	// _data/trainings.yml and api/trainings.schema.json.
	Files map[string][]byte
	// OnProposal is called when a pull request is opened, with everything the
	// app sent. The demo uses it to save the proposed file and print the diff.
	OnProposal func(Proposal)
	// OnUnexpected reports a call this fake does not implement. Tests fail on
	// it; the demo logs it. Nil means silence.
	OnUnexpected func(method, path string)
	// BaseURL, when set, is where this fake is reachable, and pull requests get
	// their html_url there instead of on github.com. The demo sets it so the
	// "Pull request opened" link leads to a page describing what would have
	// been proposed — rather than to a github.com URL that does not exist and
	// that an audience has no way of telling apart from a real one.
	BaseURL string
}

type Fake struct {
	mu        sync.Mutex
	opts      Options
	files     map[string][]byte
	refs      map[string]string // ref name -> commit sha
	commits   int
	prs       int
	calls     []string
	branches  map[string][]byte // branch -> proposed content of the edited file
	proposals []Proposal
}

const (
	// DemoToken is what the fake token endpoint hands out. It is not a
	// credential: nothing outside this process accepts it.
	DemoToken = "ghfake-token-not-a-credential"
	demoCode  = "ghfake-code"
	mainSHA   = "0000000000000000000000000000000000000001"
)

func New(o Options) *Fake {
	if o.Repo == "" {
		o.Repo = "arc42/trainings.arc42.org-site"
	}
	if o.Login == "" {
		o.Login = "demo-maintainer"
	}
	files := map[string][]byte{}
	for k, v := range o.Files {
		files[k] = v
	}
	return &Fake{
		opts:     o,
		files:    files,
		refs:     map[string]string{"refs/heads/main": mainSHA},
		branches: map[string][]byte{},
	}
}

// Calls returns the API calls seen so far, as "METHOD /path".
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// Proposals is how many pull requests have been opened against this fake.
func (f *Fake) Proposals() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prs
}

// blobSHA is git's own object id, so a committed file's sha changes exactly
// when its bytes do — which is what the app's concurrent-edit check compares.
func blobSHA(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (f *Fake) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		f.mu.Unlock()

		switch {
		case r.URL.Path == "/login/oauth/authorize":
			f.authorize(w, r)
		case r.URL.Path == "/login/oauth/access_token":
			writeJSON(w, http.StatusOK, map[string]any{
				"access_token": DemoToken, "token_type": "bearer", "scope": "public_repo",
			})
		case r.URL.Path == "/user":
			writeJSON(w, http.StatusOK, map[string]any{"login": f.opts.Login})
		case strings.HasPrefix(r.URL.Path, "/pull/"):
			f.pullPage(w, strings.TrimPrefix(r.URL.Path, "/pull/"))
		default:
			f.repoAPI(w, r)
		}
	})
}

// authorize stands in for GitHub's authorize screen. It is a page with a
// button rather than an immediate redirect, for two reasons: nobody watching a
// demo should be able to mistake it for having really signed in to GitHub, and
// a browser that prefetches links cannot sign anybody in by accident.
func (f *Fake) authorize(w http.ResponseWriter, r *http.Request) {
	redirect := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	if redirect == "" {
		http.Error(w, "missing redirect_uri", http.StatusBadRequest)
		return
	}
	back, err := url.Parse(redirect)
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	q := back.Query()
	q.Set("code", demoCode)
	q.Set("state", state)
	back.RawQuery = q.Encode()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8">
<title>Not GitHub — offline demo</title>
<body style="font:16px/1.5 system-ui;max-width:34rem;margin:4rem auto;padding:0 1rem">
<h1>This is not GitHub</h1>
<p>It is the offline demo's stand-in, running on your own machine. Nothing has
been sent anywhere, and no GitHub account is involved.</p>
<p>Continuing signs you in to the demo as <strong>%s</strong>, with permission
to push.</p>
<p><a href="%s" style="display:inline-block;padding:.6rem 1rem;background:#a04c5e;color:#fff;text-decoration:none;border-radius:.3rem">Authorize the demo</a></p>
</body>`, html.EscapeString(f.opts.Login), html.EscapeString(back.String()))
}

func (f *Fake) repoAPI(w http.ResponseWriter, r *http.Request) {
	prefix := "/repos/" + f.opts.Repo
	if !strings.HasPrefix(r.URL.Path, prefix) {
		// GitHub says no: an unknown repository is a 404, not an empty answer.
		f.unexpected(r)
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)

	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case rest == "" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"full_name":   f.opts.Repo,
			"permissions": map[string]any{"push": f.opts.CanPush},
		})

	case rest == "/git/ref/heads/main" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"object": map[string]any{"sha": f.refs["refs/heads/main"]},
		})

	case strings.HasPrefix(rest, "/contents/") && r.Method == http.MethodGet:
		path := strings.TrimPrefix(rest, "/contents/")
		content, ok := f.files[path]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"path": path, "sha": blobSHA(content), "encoding": "base64",
			"content": base64.StdEncoding.EncodeToString(content),
		})

	case rest == "/git/refs" && r.Method == http.MethodPost:
		var body struct{ Ref, SHA string }
		decode(r, &body)
		if _, exists := f.refs[body.Ref]; exists {
			// GitHub says no. This one is why the package exists: the branch
			// name has to be unique per proposal, and a double that accepted
			// duplicates hid that for months.
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "Reference already exists",
			})
			return
		}
		f.refs[body.Ref] = body.SHA
		writeJSON(w, http.StatusCreated, map[string]any{"ref": body.Ref})

	case strings.HasPrefix(rest, "/contents/") && r.Method == http.MethodPut:
		path := strings.TrimPrefix(rest, "/contents/")
		var body struct{ Message, Content, SHA, Branch string }
		decode(r, &body)
		if _, exists := f.refs["refs/heads/"+body.Branch]; !exists {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Branch not found"})
			return
		}
		if current, ok := f.files[path]; ok && body.SHA != blobSHA(current) {
			// GitHub says no: committing over a file whose blob has moved is a
			// 409, and that refusal is the app's concurrent-edit protection.
			writeJSON(w, http.StatusConflict, map[string]any{
				"message": path + " does not match " + body.SHA,
			})
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(body.Content)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "content is not base64"})
			return
		}
		// The commit lands on the branch only. main is left exactly as it was,
		// because a pull request does not change it — and a demo that quietly
		// "published" would teach the opposite of how this app works.
		f.commits++
		f.branches[body.Branch] = decoded
		f.refs["refs/heads/"+body.Branch] = fmt.Sprintf("%040d", f.commits+1)
		writeJSON(w, http.StatusOK, map[string]any{
			"content": map[string]any{"path": path, "sha": blobSHA(decoded)},
		})

	case rest == "/pulls" && r.Method == http.MethodPost:
		var body struct{ Title, Body, Head, Base string }
		decode(r, &body)
		if _, exists := f.refs["refs/heads/"+body.Head]; !exists {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "Head ref does not exist",
			})
			return
		}
		f.prs++
		host := "https://github.com/" + f.opts.Repo
		if f.opts.BaseURL != "" {
			host = strings.TrimRight(f.opts.BaseURL, "/")
		}
		p := Proposal{
			Branch: body.Head, Path: "_data/trainings.yml",
			Content: string(f.branches[body.Head]),
			Title:   body.Title, Body: body.Body, Number: f.prs,
			URL: fmt.Sprintf("%s/pull/%d", host, f.prs),
		}
		f.proposals = append(f.proposals, p)
		if f.opts.OnProposal != nil {
			f.opts.OnProposal(p)
		}
		writeJSON(w, http.StatusCreated, map[string]any{"number": p.Number, "html_url": p.URL})

	default:
		f.unexpected(r)
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
	}
}

// pullPage stands in for the pull request GitHub would have shown. It exists so
// a demo can follow the link at the end of the flow and see exactly what the
// app would have proposed — including the fact that it went nowhere.
func (f *Fake) pullPage(w http.ResponseWriter, number string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var p Proposal
	for _, q := range f.proposals {
		if fmt.Sprint(q.Number) == number {
			p = q
		}
	}
	if p.Number == 0 {
		http.Error(w, "no such proposal", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8">
<title>Proposal %d — offline demo</title>
<body style="font:16px/1.5 system-ui;max-width:52rem;margin:3rem auto;padding:0 1rem">
<p style="background:#f5eff2;border-left:4px solid #a04c5e;padding:.8rem 1rem">
This is not a pull request. It is the offline demo showing what the app would
have sent to GitHub. No branch, no commit and no pull request exist anywhere.</p>
<h1>%s</h1>
<p><strong>branch</strong> <code>%s</code> → <code>main</code> &nbsp;·&nbsp; <strong>file</strong> <code>%s</code></p>
<pre style="white-space:pre-wrap">%s</pre>
<h2>Proposed file</h2>
<pre style="background:#f7f7f8;padding:1rem;overflow:auto">%s</pre>
</body>`, p.Number, html.EscapeString(p.Title), html.EscapeString(p.Branch),
		html.EscapeString(p.Path), html.EscapeString(p.Body), html.EscapeString(p.Content))
}

func (f *Fake) unexpected(r *http.Request) {
	if f.opts.OnUnexpected != nil {
		f.opts.OnUnexpected(r.Method, r.URL.Path)
	}
}

func decode(r *http.Request, v any) {
	_ = json.NewDecoder(r.Body).Decode(v)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// SetBaseURL records where this fake is reachable, once its listener has a
// port. See Options.BaseURL.
func (f *Fake) SetBaseURL(u string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opts.BaseURL = u
}
