// Command demo runs the real admin app against a fake GitHub, so the whole
// thing can be clicked through — or shown to somebody — offline.
//
// It is the same server, the same handlers and the same sign-in flow as
// production. Only the far side is different: instead of api.github.com it
// talks to internal/ghfake, which serves this checkout's own _data/trainings.yml
// and schema and accepts the three calls that open a pull request. Nothing
// leaves the machine, no credentials are involved, and the repository is only
// ever read.
//
// It lives in its own package on purpose. The production image builds the root
// package alone (see admin-app/Dockerfile), so no part of this file — and no
// part of the fake it starts — can end up in the deployed binary. There is
// deliberately no demo switch inside the app itself: a flag that turns off
// sign-in in a real deployment is exactly the thing worth not having.
//
// Usage, from the repository root:
//
//	make app-demo
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"arc42-trainings-admin/internal/config"
	"arc42-trainings-admin/internal/ghfake"
	"arc42-trainings-admin/internal/web"
)

const (
	dataPath   = "_data/trainings.yml"
	schemaPath = "api/trainings.schema.json"
	// Fixed, obviously-fake stand-ins for the three production secrets. They
	// are checked for length by config.Load, never used as credentials: the
	// only party that sees them is the fake in this same process.
	demoClientID     = "demo-client-id-not-a-secret"
	demoClientSecret = "demo-client-secret-not-a-secret"
	demoSessionKey   = "demo-session-key-not-a-secret-0000000000"
)

func main() {
	repo := flag.String("repo", "..", "path to the repository checkout to read trainings.yml from")
	addr := flag.String("addr", ":8080", "address to listen on")
	out := flag.String("out", "demo-out", "directory to write proposed files to (relative to -repo)")
	login := flag.String("login", "demo-maintainer", "the GitHub login the demo signs you in as")
	flag.Parse()

	// Absolute paths throughout: the demo is started from the repository root
	// via make but runs with admin-app/ as its working directory, so a relative
	// path in the banner would point at neither.
	root, err := filepath.Abs(*repo)
	if err != nil {
		log.Fatalf("demo: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, dataPath))
	if err != nil {
		log.Fatalf("demo: %v\n\nRun this from the repository root (make app-demo), or pass -repo.", err)
	}
	schema, err := os.ReadFile(filepath.Join(root, schemaPath))
	if err != nil {
		log.Fatalf("demo: %v", err)
	}
	outDir := filepath.Join(root, *out)

	fake := ghfake.New(ghfake.Options{
		Repo: "arc42/trainings.arc42.org-site", Login: *login, CanPush: true,
		Files: map[string][]byte{dataPath: data, schemaPath: schema},
		OnProposal: func(p ghfake.Proposal) {
			savedTo, err := save(outDir, p)
			if err != nil {
				log.Printf("demo: could not save the proposal: %v", err)
				return
			}
			log.Printf("proposal %d on branch %s — nothing was sent anywhere", p.Number, p.Branch)
			log.Printf("  saved to  %s", savedTo)
			log.Printf("  compare   diff -u %s %s", filepath.Join(root, dataPath), savedTo)
		},
		OnUnexpected: func(method, path string) {
			log.Printf("demo: the app called %s %s, which the fake does not implement", method, path)
		},
	})

	// The fake listens on its own loopback port; the app is pointed at it in
	// place of api.github.com, which also moves sign-in there (gh.EndpointFor).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("demo: %v", err)
	}
	apiBase := "http://" + ln.Addr().String()
	fake.SetBaseURL(apiBase)
	go func() { log.Fatal(http.Serve(ln, fake.Handler())) }()

	publicURL := "http://localhost" + *addr
	if !strings.HasPrefix(*addr, ":") {
		publicURL = "http://" + *addr
	}
	srv, err := web.NewServer(config.Config{
		Addr:         *addr,
		GitHubRepo:   "arc42/trainings.arc42.org-site",
		ClientID:     demoClientID,
		ClientSecret: demoClientSecret,
		SessionKey:   demoSessionKey,
		// DEVELOPMENT, so the session cookie is not marked Secure — the demo is
		// plain http on localhost. It is the only thing this changes.
		Environment: "DEVELOPMENT",
		PublicURL:   publicURL,
	}, apiBase, publicURL)
	if err != nil {
		log.Fatalf("demo: %v", err)
	}

	banner(publicURL, filepath.Join(root, dataPath), outDir)
	log.Fatal(http.ListenAndServe(*addr, srv.Routes()))
}

// save writes a proposal's file next to the others, named after its branch, so
// the result of an editing session can be read and diffed afterwards.
func save(dir string, p ghfake.Proposal) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := strings.ReplaceAll(p.Branch, "/", "_") + ".yml"
	path := filepath.Join(dir, name)
	return path, os.WriteFile(path, []byte(p.Content), 0o644)
}

func banner(publicURL, dataFile, outDir string) {
	fmt.Printf(`
  arc42 trainings admin — offline demo

  Open           %s
  Sign in        one click, no GitHub account, no network

  Reads          %s   (never written)
  Publishing     writes the proposed file to %s/
                 and opens nothing: no branch, no commit, no pull request

  Everything you see is the real app. Only GitHub is a stand-in, running in
  this same process. Ctrl-C to stop.

`, publicURL, dataFile, outDir)
}
