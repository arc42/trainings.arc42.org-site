.DEFAULT_GOAL := help

.PHONY: help dev build stop site check-links clean install update shell logs app app-test app-stop app-logs app-shell

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

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
	docker compose up --build

build: ## Build the Docker dev image from the Gemfile-pinned gems
	docker compose build

stop: ## Stop and remove the running dev container
	docker compose down

site: build ## Generate the static site into _site/
	docker compose run --rm jekyll bundle exec jekyll build

check-links: site ## Validate internal links, images, and HTML in the built _site (html-proofer)
	docker compose run --rm jekyll bundle exec htmlproofer ./_site --disable-external --allow-hash-href

clean: ## Remove generated _site AND the Docker cache volumes (a true reset)
	rm -rf _site .sass-cache .jekyll-cache .jekyll-metadata
	-docker compose down -v --remove-orphans

install: build ## Install/refresh gems into the dev image after editing the Gemfile
	docker compose run --rm jekyll bundle install

update: build ## Update gems to their latest allowed versions (rewrites Gemfile.lock)
	docker compose run --rm jekyll bundle update

shell: build ## Open a shell inside the dev container for debugging
	docker compose run --rm jekyll bash

logs: ## Tail logs from the running dev container
	docker compose logs -f jekyll

app: ## Run the trainings admin app at http://localhost:8080
	@test -f admin-app/.env || { \
		echo "==> admin-app/.env is missing."; \
		echo "==> Run: cp admin-app/.env.template admin-app/.env  and fill it in."; \
		exit 1; \
	}
	@# SESSION_KEY is not looked up anywhere - it is just a local random key that
	@# encrypts the session cookie. Making a human generate randomness is a bad
	@# prompt, so fill it in here when it is empty. Never overwrite an existing
	@# one: that would sign everybody out. Production sets it as a fly secret.
	@if ! grep -qE '^SESSION_KEY=.{32,}' admin-app/.env; then \
		key=$$(openssl rand -hex 32); \
		if grep -qE '^SESSION_KEY=' admin-app/.env; then \
			awk -v k="$$key" '/^SESSION_KEY=/ { print "SESSION_KEY=" k; next } { print }' \
				admin-app/.env > admin-app/.env.tmp && mv admin-app/.env.tmp admin-app/.env; \
		else \
			printf 'SESSION_KEY=%s\n' "$$key" >> admin-app/.env; \
		fi; \
		echo "==> Generated a SESSION_KEY in admin-app/.env (local cookie key - nothing to look up)."; \
	fi
	@holder=$$(docker ps --filter "publish=8080" --format '{{.Names}}'); \
	if [ -n "$$holder" ]; then \
		echo "==> Port 8080 is already in use by another container: $$holder"; \
		echo "==> Stop it first, e.g.:  docker stop $$holder"; \
		exit 1; \
	fi
	@echo "==> Open http://localhost:8080"
	docker compose up --build admin

app-test: ## Run the admin app's Go test suite (includes the Ruby cross-check)
	docker compose run --rm --no-deps admin go test ./... -v

app-stop: ## Stop and remove the running admin container
	docker compose rm -sf admin

app-logs: ## Tail logs from the running admin container
	docker compose logs -f admin

app-shell: ## Open a shell inside the admin container
	docker compose run --rm --no-deps admin sh
