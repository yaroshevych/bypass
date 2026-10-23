default:
    @just --list

run *args:
    go run ./cmd/keepassui {{args}}

build:
    go build -trimpath -o bin/keepassui ./cmd/keepassui

test:
    go test -race ./...

check:
    @test -z "$(gofmt -l cmd internal evals)"
    go vet ./...
    go test -race ./...

eval:
    go test -tags=integration -count=1 -timeout=120s -v ./evals
