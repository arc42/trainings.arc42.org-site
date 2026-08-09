package main

import (
	"log"
	"net/http"

	"arc42-trainings-admin/internal/config"
	"arc42-trainings-admin/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("startup: %v", err)
	}
	srv, err := web.NewServer(cfg, "https://api.github.com", cfg.PublicURL)
	if err != nil {
		log.Fatalf("startup: %v", err)
	}
	// The callback is logged because it MUST equal the OAuth app's registered
	// callback URL; a mismatch is otherwise only visible as a GitHub error page.
	log.Printf("trainings-admin listening on %s (repo %s, env %s, callback %s/auth/callback)",
		cfg.Addr, cfg.GitHubRepo, cfg.Environment, cfg.PublicURL)
	if err := http.ListenAndServe(cfg.Addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
