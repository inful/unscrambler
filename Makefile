.PHONY: dev dev-web dev-explain dev-draw templ build test

# Regenerate templ components (also runs before each air build).
templ:
	templ generate ./views/...

# Live reload — run one per terminal to review games side by side.
dev: dev-web

dev-web:
	air

dev-explain:
	air -c .air.explain.toml

dev-draw:
	air -c .air.draw.toml

build:
	go build -o ./tmp/web ./cmd/web
	go build -o ./tmp/explain ./cmd/explain
	go build -o ./tmp/draw ./cmd/draw

test:
	go test ./...
