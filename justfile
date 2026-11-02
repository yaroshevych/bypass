default:
    @just --list

run *args:
    go run ./cmd/bypass {{args}}

# Open and unlock the local development vault; excluded from normal builds.
debug *args:
    @go run -tags debug ./cmd/bypass --debug-env .env {{args}}

# Request a vault field from a running `just debug`; use a fresh demo identity.
# Example: just debug-run 'bypass://GitHub#password' dev-rw
debug-run reference agent="demo-agent":
    #!/usr/bin/env sh
    set -eu
    reference={{quote(reference)}}
    case "$reference" in
        bypass://*) ;;
        *) printf '%s\n' 'reference must start with bypass://' >&2; exit 2 ;;
    esac
    suffix="$(python3 -c 'import secrets; print(secrets.token_hex(16))')"
    recipient="$(python3 -c 'import os, shutil; print(os.path.realpath(shutil.which("true")))')"
    BYPASS_IDENTITY={{quote(agent)}}-"$suffix" BYPASS_DEMO_SECRET="$reference" \
        go run -tags debug ./cmd/bypass run --timeout 5m -- "$recipient"
    printf '%s\n' 'permission approved; command completed (secret not printed)'

build:
    go build -trimpath -o bin/bypass ./cmd/bypass

# Stop project evals and remove binaries, generated artifacts, and temporary fixtures.
clean *args:
    python3 scripts/clean.py {{args}}

test:
    go test -race ./...

check:
    @test -z "$(gofmt -l cmd internal evals)"
    go vet ./...
    go test -race ./...
    go test -tags=debug ./internal/ui ./cmd/bypass

eval:
    go test -race -tags=integration -parallel=4 -count=1 -timeout=180s -v ./evals

# Strict local release check: native tests must not silently skip prerequisites.
verify:
    @command -v tmux >/dev/null
    @command -v keepassxc-cli >/dev/null
    @test -z "$(gofmt -l cmd internal evals)"
    go vet ./...
    go test -race -tags=integration -parallel=4 -count=1 -timeout=300s ./...

# Repeat terminal journeys to expose synchronization failures.
eval-repeat:
    @command -v tmux >/dev/null
    @command -v keepassxc-cli >/dev/null
    go test -race -tags=integration -parallel=4 -count=2 -timeout=300s ./evals

mutations:
    go run ./evals/mutations

# Coverage of application packages; tmux drives a separate application process.
coverage:
    mkdir -p evals/artifacts
    go test -race -coverprofile=evals/artifacts/coverage.out ./cmd/bypass ./internal/...
    go tool cover -func=evals/artifacts/coverage.out

# Synthetic native vault + real tmux journey; PNG, ANSI, text and metadata.
screenshot screen="workspace" appearance="classic" theme="macchiato" width="120" height="35": build
    python3 evals/screenshots/capture.py {{quote(screen)}} --appearance {{quote(appearance)}} --theme {{quote(theme)}} --width {{quote(width)}} --height {{quote(height)}}

screenshots: build
    python3 evals/screenshots/capture.py --all

screenshot-list:
    python3 evals/screenshots/capture.py --list

screenshot-check:
    python3 -m unittest discover -s evals/screenshots -p 'test_*.py'
