.DEFAULT_GOAL := help

.PHONY: help dev build stop site check-links clean install update shell logs \
        app-test app-check app-build app-demo app-stop app-preview check-go \
        check-flyctl fly-deploy fly-status fly-logs fly-secrets fly-releases

# This site's fixed local dev port. Every arc42 site has its own so their dev
# servers can run side by side; see raw/port-assignment.md in meta.arc42.org.
# Changing it here is not enough: docker-compose.yml maps it and the Dockerfile
# CMD passes it to Jekyll so its startup banner names the real port.
SITE_PORT   := 4040
APP_DIR     := admin-app
FLY_APP     := arc42-trainings-admin
PREVIEW_DIR := preview-out

help: ## Show this help
	@printf "\ntrainings.arc42.org — two halves, hosted in two places:\n"
	@printf "  the site      Jekyll, static, GitHub Pages   → targets below\n"
	@printf "  the admin app Go, container, fly.io          → app-* and fly-* targets\n\n"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
	@printf "\n  Both halves normally ship from CI on push to main. 'make fly-deploy' is the\n"
	@printf "  manual escape hatch for the admin app — see admin-app/README.md.\n\n"

dev: ## Start the local Jekyll dev server with live reload (http://localhost:4040)
	@echo "==> Open http://localhost:$(SITE_PORT)  (NOT http://0.0.0.0:$(SITE_PORT) — Firefox refuses to connect to 0.0.0.0)"
	@docker compose down --remove-orphans >/dev/null 2>&1 || true
	@holder=$$(docker ps --filter "publish=$(SITE_PORT)" --format '{{.Names}}'); \
	if [ -n "$$holder" ]; then \
		echo "==> Port $(SITE_PORT) is already in use by another container: $$holder"; \
		echo "==> That's likely a dev server from a sibling arc42 site repo. Stop it first, e.g.:"; \
		echo "==>   docker stop $$holder"; \
		exit 1; \
	fi
	docker compose up --build jekyll

build: ## Build the Docker dev image from the Gemfile-pinned gems
	docker compose build

stop: ## Stop and remove the Jekyll dev container (the demo is not a container — see app-stop)
	docker compose down

site: build ## Generate the static site into _site/
	docker compose run --rm jekyll bundle exec jekyll build

check-links: site ## Validate internal links, images, and HTML in the built _site (html-proofer)
	docker compose run --rm jekyll bundle exec htmlproofer ./_site --disable-external --allow-hash-href

clean: ## Remove generated _site, Docker volumes, the admin binary and demo/preview output
	rm -rf _site .sass-cache .jekyll-cache .jekyll-metadata
	rm -f $(APP_DIR)/admin
	rm -rf demo-out $(PREVIEW_DIR)
	-docker compose down -v --remove-orphans

install: build ## Install/refresh gems into the dev image after editing the Gemfile
	docker compose run --rm jekyll bundle install

update: build ## Update gems to their latest allowed versions (rewrites Gemfile.lock)
	docker compose run --rm jekyll bundle update

shell: build ## Open a shell inside the dev container for debugging
	docker compose run --rm jekyll bash

logs: ## Tail logs from the running dev container
	docker compose logs -f jekyll

# ------------------------------------------------------- trainings admin app (Go)
#
# The admin app in $(APP_DIR) is a separate program with a separate lifecycle:
# Go, not Ruby; a container on fly.io, not GitHub Pages. It has no local mode
# (it needs real GitHub OAuth credentials to do anything), so the loop is
# "change, `make app-check`, ship". See admin-app/README.md for how it works.

app-test: check-go ## Run the admin app's Go test suite
	cd $(APP_DIR) && go test ./...

app-check: check-go ## Run exactly what CI runs before it deploys: tests, vet, gofmt
	cd $(APP_DIR) && go test ./...
	cd $(APP_DIR) && go vet ./...
	@unformatted=$$(cd $(APP_DIR) && gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		printf "==> gofmt would rewrite:\n%s\n" "$$unformatted"; exit 1; \
	fi
	@printf "==> tests, vet and gofmt are clean — this is what CI checks\n"

app-demo: check-go ## Run the admin app offline on :8080 against a fake GitHub (nothing is published)
	@printf "==> The demo reads _data/trainings.yml and never writes it.\n"
	@printf "==> Publishing writes the proposed file to demo-out/ and opens nothing.\n"
	cd $(APP_DIR) && go run ./cmd/demo -repo ..

# `make stop` is docker compose down, and the demo is not a container: it is a
# plain Go process. Ctrl-C ends it in the foreground, which is the normal case.
# This target is for the other one — started in the background, or the `go run`
# wrapper killed on its own, which leaves the compiled child still holding :8080.
#
# Matching on "-repo", not on the path: `go run` execs the binary it built from a
# directory it names itself, sometimes the build cache and sometimes a temp dir,
# so the only stable part of that child's command line is the name it was given
# and the flag it was passed. The "[o]" keeps the pattern from matching the shell
# running this recipe, whose command line contains it too — BSD pgrep skips the
# caller's ancestors, GNU pgrep does not, and this way it does not matter which
# one is installed.
app-stop: ## Stop a demo left running in the background (Ctrl-C is enough in the foreground)
	@pids=$$(pgrep -f '/dem[o] -repo' 2>/dev/null || true); \
	if [ -z "$$pids" ]; then printf "==> No demo is running.\n"; exit 0; fi; \
	kill $$pids 2>/dev/null || true; \
	printf "==> stopped the demo (pid%s)\n" "$$(echo $$pids | sed 's/^/ /')"

app-preview: check-go ## Render every page to preview-out/ as HTML files, without a server
	@mkdir -p $(PREVIEW_DIR)/static
	cd $(APP_DIR) && PREVIEW_DIR=$$(cd .. && pwd)/$(PREVIEW_DIR) go test ./internal/web -run TestDumpPreview -count=1
	@printf "==> open %s/list.html\n" "$(PREVIEW_DIR)"

app-build: check-go ## Compile the admin app to admin-app/admin (a local build check only)
	@# fly builds its own image from admin-app/Dockerfile; this binary is never
	@# deployed, it just fails fast on a compile error without a Docker round trip.
	cd $(APP_DIR) && go build -o admin .

check-go:
	@command -v go >/dev/null 2>&1 || { \
		printf "\n  Go is not installed (the admin app needs Go 1.23+).\n"; \
		printf "  https://go.dev/dl/  — or: brew install go\n\n"; exit 1; }

# --------------------------------------------------------- fly.io (the admin app)
#
# The normal way the app reaches production is a push to main touching
# admin-app/**: .github/workflows/deploy-admin-app.yml runs the tests and then
# `flyctl deploy`. These targets are for the cases CI cannot cover — GitHub
# Actions is down, or you want to try a branch on the real thing, which is the
# only way to exercise GitHub OAuth and a real pull request.
#
# There is no fly-ssh target: the production image is FROM scratch and contains
# one static binary, so there is no shell on the machine to open.

fly-deploy: check-flyctl app-check ## Deploy the admin app in the CURRENT working tree to fly.io
	@branch=$$(git rev-parse --abbrev-ref HEAD); \
	sha=$$(git rev-parse --short HEAD); \
	dirty=$$(git status --porcelain -- $(APP_DIR) | wc -l | tr -d ' '); \
	printf "\n==> Deploying $(APP_DIR)/ to fly app %s (https://trainings-admin.arc42.org)\n" "$(FLY_APP)"; \
	printf "==> from branch %s at %s" "$$branch" "$$sha"; \
	if [ "$$dirty" != "0" ]; then printf ", plus %s uncommitted change(s) in $(APP_DIR)/" "$$dirty"; fi; \
	printf "\n"; \
	if [ "$$branch" != "main" ] || [ "$$dirty" != "0" ]; then \
		printf "==> NOTE: fly builds from your working tree, not from main. What ships here\n"; \
		printf "==>       has not been through review, and the next push to main replaces it.\n"; \
	fi; \
	if [ "$(YES)" != "1" ]; then \
		printf "==> Type 'deploy' to continue (or YES=1 make fly-deploy to skip this): "; \
		read -r answer; \
		[ "$$answer" = "deploy" ] || { printf "==> aborted, nothing was deployed\n"; exit 1; }; \
	fi
	cd $(APP_DIR) && flyctl deploy --remote-only

fly-status: check-flyctl ## Show the fly app, its machines and their health checks
	@# "stopped" machines are the normal resting state: min_machines_running = 0,
	@# so fly stops them when idle and cold-starts one on the next request.
	cd $(APP_DIR) && flyctl status

fly-logs: check-flyctl ## Tail the admin app's production logs (Ctrl-C to stop)
	cd $(APP_DIR) && flyctl logs

fly-releases: check-flyctl ## List what has been deployed, newest first
	cd $(APP_DIR) && flyctl releases

fly-secrets: check-flyctl ## List the NAMES of the fly secrets (values are never readable)
	@# Set one with: flyctl secrets set NAME=value -a $(FLY_APP)
	@# Setting a secret restarts the machine, which discards every open draft.
	cd $(APP_DIR) && flyctl secrets list

check-flyctl:
	@command -v flyctl >/dev/null 2>&1 || { \
		printf "\n  flyctl is not installed.\n"; \
		printf "  https://fly.io/docs/flyctl/install/  — or: brew install flyctl\n\n"; exit 1; }
	@flyctl auth whoami >/dev/null 2>&1 || { \
		printf "\n  flyctl is installed but not signed in — run: flyctl auth login\n"; \
		printf "  You also need access to the '$(FLY_APP)' fly app.\n\n"; exit 1; }
