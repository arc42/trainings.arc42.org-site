# Production image for the registration service. Same shape as
# admin-app/Dockerfile: built with registration-app/ as the context, one static
# binary FROM scratch. time/tzdata is compiled in (internal/feed), because
# scratch has no zoneinfo and "is this date past" is decided in Berlin time.
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/registration .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/registration /registration
EXPOSE 8080
ENTRYPOINT ["/registration"]
