default:
    @just --list

build:
    mise exec -- go build -trimpath -o bin/kqt ./cmd/kqt

run *args:
    mise exec -- go run ./cmd/kqt {{args}}

demo:
    mise exec -- go run ./cmd/kqt --demo

check:
    mise exec -- go test -race ./...
    mise exec -- go vet ./...
    python3 -m unittest discover -s scripts -p '*_test.py'

smoke: build
    python3 scripts/tui-smoke.py --binary bin/kqt
    python3 scripts/operator-smoke.py --binary bin/kqt
    python3 scripts/features-smoke.py --feature relations
    python3 scripts/features-smoke.py --feature manifest
    python3 scripts/features-smoke.py --feature actions

smoke-live: build
    sh scripts/live-smoke.sh
