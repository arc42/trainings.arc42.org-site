# Makefile for the arc42 trainings site (Jekyll).
# Wraps the local Docker workflow so you don't need to remember the commands.
# All targets use the pinned image defined in ./Dockerfile + docker-compose.yml.

.DEFAULT_GOAL := help
.PHONY: help build serve clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2}'

build: ## Build the static site into _site/, then serve it at http://localhost:4000
	docker compose run --rm jekyll bundle exec jekyll build
	docker compose up

serve: ## Serve locally at http://localhost:4000 with live reload
	docker compose up

clean: ## Remove generated _site/ and Jekyll caches
	rm -rf _site .jekyll-metadata .sass-cache
