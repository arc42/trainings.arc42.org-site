package main

import (
	"log"
	"net/http"

	"arc42-trainings-admin/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("startup: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})
	log.Printf("trainings-admin listening on %s (repo %s, env %s)", cfg.Addr, cfg.GitHubRepo, cfg.Environment)
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatal(err)
	}
}
