#!/bin/sh
# All Kubernetes writes target the disposable cluster created below.
set -eu
cd "$(dirname "$0")/.."
for dependency in k3d kubectl python3 go; do
    command -v "$dependency" >/dev/null || { echo "Required: $dependency" >&2; exit 1; }
done
test -x bin/kqt || { echo 'Build bin/kqt first (just build).' >&2; exit 1; }
test_root=$(mktemp -d "${TMPDIR:-/tmp}/kqt-live.XXXXXXXX")
cluster_name="kqt-smoke-$(date +%s)-$$"
created=false
cleanup() {
    if [ "$created" = true ]; then KUBECONFIG="$test_root/config" k3d cluster delete "$cluster_name"; fi
    rm -rf "$test_root"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
created=true
k3d cluster create "$cluster_name" --image rancher/k3s:v1.36.2-k3s1 --agents 0 --no-lb --kubeconfig-update-default=false --kubeconfig-switch-context=false --wait --timeout 90s
k3d kubeconfig get "$cluster_name" > "$test_root/config"
kubectl --kubeconfig "$test_root/config" apply -f scripts/live-fixture.yaml
kubectl --kubeconfig "$test_root/config" wait --for=condition=Established crd/widgets.exploration.kqt.dev --timeout=30s
kubectl --kubeconfig "$test_root/config" apply -f scripts/live-widget.yaml
kubectl --kubeconfig "$test_root/config" wait --for=condition=Ready pod/kqt-log-fixture -n kqt-test --timeout=90s
kubectl --kubeconfig "$test_root/config" rollout status deployment/kqt-deployment -n kqt-test --timeout=90s
kubectl --kubeconfig "$test_root/config" rollout status statefulset/kqt-stateful -n kqt-test --timeout=90s
kubectl --kubeconfig "$test_root/config" get pod kqt-log-fixture -n kqt-test -o json > "$test_root/pod.json"
python3 - "$test_root" <<'PY'
import datetime, json, pathlib, sys
root = pathlib.Path(sys.argv[1])
pod = json.loads((root / 'pod.json').read_text())
event = {'apiVersion': 'v1', 'kind': 'Event', 'metadata': {'name': 'kqt-fixture-event', 'namespace': 'kqt-test'},
         'involvedObject': {'apiVersion': 'v1', 'kind': 'Pod', 'name': 'kqt-log-fixture', 'namespace': 'kqt-test', 'uid': pod['metadata']['uid']},
         'type': 'Normal', 'reason': 'KqtFixture', 'message': 'kqt-event-marker', 'count': 1,
         'lastTimestamp': datetime.datetime.now(datetime.timezone.utc).isoformat().replace('+00:00', 'Z')}
(root / 'event.json').write_text(json.dumps(event))
PY
kubectl --kubeconfig "$test_root/config" apply -f "$test_root/event.json"
KQT_LIVE_KUBECONFIG="$test_root/config" go test -race ./internal/kube -run '^TestLiveOperator$' -v -count=1 -timeout=4m
python3 scripts/tui-smoke.py --kubeconfig "$test_root/config" "$@"
python3 scripts/operator-smoke.py --kubeconfig "$test_root/config"
# Run mutation qualification last so short-lived terminating pods cannot affect
# the baseline operator/overview fixture counts.
python3 scripts/features-smoke.py --feature watch --kubeconfig "$test_root/config"
for feature in TestLiveResources TestLiveAccess TestLiveOverview TestLiveActions; do
    KQT_LIVE_KUBECONFIG="$test_root/config" go test -race ./internal/kube -run "^${feature}$" -v -count=1 -timeout=2m
done
