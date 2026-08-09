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
	publicURL := "http://localhost:8080"
	if cfg.Environment == "PRODUCTION" {
		publicURL = "https://arc42-trainings-admin.fly.dev"
	}
	srv, err := web.NewServer(cfg, "https://api.github.com", publicURL)
	if err != nil {
		log.Fatalf("startup: %v", err)
	}
	log.Printf("trainings-admin listening on %s (repo %s, env %s)", cfg.Addr, cfg.GitHubRepo, cfg.Environment)
	if err := http.ListenAndServe(cfg.Addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
