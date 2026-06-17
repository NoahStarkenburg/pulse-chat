# Pulse Chat - common commands.
#
# `just` is a modern, cross-platform alternative to `make`. Install it via:
#   winget install Casey.Just     (Windows)
#   brew install just             (macOS)
#   cargo install just            (anywhere)
#
# Run `just` (no args) to see the list of available recipes.
# Run `just <recipe>` to execute one.

# Default: show available recipes.
default:
    @just --list

# --- Local development -------------------------------------------------------

# Run the server (Phase 1+).
run:
    go run ./cmd/server

# Run with debug logging.
run-debug:
    PULSE_LOG_LEVEL=debug go run ./cmd/server

# --- Quality gates -----------------------------------------------------------

# Run the linter. Install golangci-lint first:
#   go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
lint:
    golangci-lint run ./...

# Run all tests.
test:
    go test -race -timeout 60s ./...

# Run tests with coverage. Opens an HTML report in the browser.
coverage:
    go test -race -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out

# Tidy module deps after adding/removing imports.
tidy:
    go mod tidy

# Format the codebase (gofmt + goimports).
fmt:
    gofmt -w -s .
    @echo "If you have goimports installed, also run: goimports -w ."

# One-shot normalize formatting after first Go install. Run this once,
# commit the changes, then re-enable gofmt/gofumpt/goimports in .golangci.yml.
fix-fmt:
    @echo "Installing goimports if needed..."
    @go install golang.org/x/tools/cmd/goimports@latest
    gofmt -w -s .
    goimports -w -local github.com/NoahStarkenburg/pulse-chat .
    @echo
    @echo "Done. Now re-enable gofmt + gofumpt + goimports in .golangci.yml"
    @echo "(uncomment the three lines under 'FORMATTING LINTERS') and commit."

# --- Local infra (Docker Compose) --------------------------------------------

# Start all infra services in the background.
up:
    docker compose -f deployments/docker-compose.yml up -d

# Start a single service.
up-one service:
    docker compose -f deployments/docker-compose.yml up -d {{service}}

# Stop all services (data preserved).
down:
    docker compose -f deployments/docker-compose.yml down

# Stop all services AND wipe their data volumes. Use when you want a clean slate.
nuke:
    docker compose -f deployments/docker-compose.yml down -v

# Tail logs from a service. Example: `just logs postgres`
logs service:
    docker compose -f deployments/docker-compose.yml logs -f {{service}}

# --- Build -------------------------------------------------------------------

# Build the server binary into ./bin/server.
build:
    go build -trimpath -ldflags="-s -w" -o bin/server ./cmd/server

# Build the Docker image.
docker-build:
    docker build -t pulse-chat:dev -f Dockerfile .

# --- Protobuf / gRPC (Phase 5b) ----------------------------------------------
# Uncomment these when you reach Phase 5b. You'll need `buf` installed:
#   go install github.com/bufbuild/buf/cmd/buf@latest
#
# proto-gen:
#     buf generate
#
# proto-lint:
#     buf lint
#
# proto-breaking:
#     buf breaking --against '.git#branch=main'

# --- Architecture Decision Records (ADRs) ------------------------------------

# Create a new ADR. Usage: just adr "use sqlc for typed queries"
adr title:
    @next=$(printf "%04d" $(( $(ls decisions/*.md 2>/dev/null | grep -E '^decisions/[0-9]{4}-' | wc -l) + 0 ))); \
    slug=$(echo "{{title}}" | tr '[:upper:]' '[:lower:]' | tr ' ' '-' | tr -cd 'a-z0-9-'); \
    file="decisions/${next}-${slug}.md"; \
    cp decisions/template.md "$file"; \
    echo "Created $file. Edit it now."

# --- Load testing (Phase 6+) -------------------------------------------------
# k6 must be installed: winget install k6 --source winget
# loadtest script:
#     just loadtest websocket-fanout

# loadtest script:
#     k6 run loadtest/{{script}}.js

# --- Hygiene -----------------------------------------------------------------

# Remove build artifacts.
clean:
    rm -rf bin/ coverage.out coverage.html
