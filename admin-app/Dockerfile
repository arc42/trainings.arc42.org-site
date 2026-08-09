# Production image for the trainings admin app.
#
# No Ruby here. Ruby lives only in Dockerfile.dev, where it backs the
# validate_trainings.rb cross-check that runs as part of the test suite; the
# production binary is pure Go and never shells out to it.
#
# Build context gotcha: this file is built with admin-app/ itself as the
# context — `flyctl deploy` runs from inside admin-app/, and so does the
# `docker build` invocation below — so COPY paths here are plain (go.mod, not
# admin-app/go.mod). Dockerfile.dev is the opposite: docker-compose.yml sets
# its context to the repo root, because its test run needs _data/, api/ and
# scripts/ alongside the Go sources, so its COPY paths are prefixed with
# admin-app/. The two files genuinely differ here — copying one pattern into
# the other silently breaks whichever build you didn't test.

# Build
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/admin .

# Run — scratch, because the app has no state, no shell needs and no Ruby.
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/admin /admin
EXPOSE 8080
ENTRYPOINT ["/admin"]
