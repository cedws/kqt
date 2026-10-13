default:
    @just --list

build:
    mise exec -- go build -trimpath -o bin/ket ./cmd/ket

run *args:
    mise exec -- go run ./cmd/ket {{args}}

demo:
    mise exec -- go run ./cmd/ket --demo

check:
    mise exec -- go test -race ./...
    mise exec -- go vet ./...

smoke: build
    python3 scripts/tui-smoke.py --binary bin/ket

smoke-live: build
    sh scripts/live-smoke.sh
