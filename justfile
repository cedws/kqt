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
    python3 scripts/features-smoke.py --feature watch
    python3 scripts/features-smoke.py --feature access
    python3 scripts/features-smoke.py --feature overview
    python3 scripts/features-smoke.py --feature downloads
    python3 scripts/features-smoke.py --feature logs-controls
    python3 scripts/features-smoke.py --feature log-queries
    python3 scripts/features-smoke.py --feature log-options
    python3 scripts/features-smoke.py --feature rollouts
    python3 scripts/features-smoke.py --feature contexts
    python3 scripts/features-smoke.py --feature command-history

smoke-live: build
    sh scripts/live-smoke.sh
