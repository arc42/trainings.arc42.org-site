.DEFAULT_GOAL := help

.PHONY: help dev build stop site check-links clean install update shell logs \
        app-test app-check app-build check-go check-flyctl \
        fly-deploy fly-status fly-logs fly-secrets fly-releases

APP_DIR := admin-app
FLY_APP := arc42-trainings-admin

help: ## Show this help
	@printf "\ntrainings.arc42.org — two halves, hosted in two places:\n"
	@printf "  the site      Jekyll, static, GitHub Pages   → targets below\n"
	@printf "  the admin app Go, container, fly.io          → app-* and fly-* targets\n\n"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
	@printf "\n  Both halves normally ship from CI on push to main. 'make fly-deploy' is the\n"
	@printf "  manual escape hatch for the admin app — see admin-app/README.md.\n\n"

dev: ## Start the local Jekyll dev server with live reload (http://localhost:4000)
	@echo "==> Open http://localhost:4000  (NOT http://0.0.0.0:4000 — Firefox refuses to connect to 0.0.0.0)"
	@docker compose down --remove-orphans >/dev/null 2>&1 || true
	@holder=$$(docker ps --filter "publish=4000" --format '{{.Names}}'); \
	if [ -n "$$holder" ]; then \
		echo "==> Port 4000 is already in use by another container: $$holder"; \
		echo "==> That's likely a dev server from a sibling arc42 site repo. Stop it first, e.g.:"; \
		echo "==>   docker stop $$holder"; \
		exit 1; \
	fi
	docker compose up --build jekyll

build: ## Build the Docker dev image from the Gemfile-pinned gems
	docker compose build

stop: ## Stop and remove the running dev container
	docker compose down

site: build ## Generate the static site into _site/
	docker compose run --rm jekyll bundle exec jekyll build

check-links: site ## Validate internal links, images, and HTML in the built _site (html-proofer)
	docker compose run --rm jekyll bundle exec htmlproofer ./_site --disable-external --allow-hash-href

clean: ## Remove generated _site, the Docker cache volumes and the admin binary
	rm -rf _site .sass-cache .jekyll-cache .jekyll-metadata
	rm -f $(APP_DIR)/admin
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
