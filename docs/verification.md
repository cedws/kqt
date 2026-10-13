# Initial verification

Verified locally on 2026-10-12 with Go 1.27.1 on macOS arm64.

- Native build passed; the executable is `bin/ket`.
- `go test -race ./...` and `go vet ./...` passed.
- A Linux amd64 build with CGO disabled passed.
- Offline help, version, theme listing, example configuration, and validation
  worked with redirected output. The UI requires a terminal.
- PTY checks passed in demo mode at 80x24 (classic), 180x48 (right inspector),
  and 80x40 (bottom inspector). Checks cover navigation, filtering, namespace
  scope, workload drill-down/back, YAML, container selection, current/previous
  logs, CRD browsing, secret redaction, themes, and help. Normal exit restored
  the alternate screen and the original terminal attributes.
- The same three terminal checks passed against a disposable k3d cluster
  running Kubernetes v1.36.2+k3s1, using a separate temporary kubeconfig. They
  verified a real pod list, YAML, pod log output, API discovery of a newly
  installed Widget CRD, custom-resource status, and redacted secret values.
  The cluster was removed after verification.

The backend tests additionally verify request paths/options, namespace and
all-namespace requests, label/field selectors, pagination, discovery aliases,
RBAC errors, cancellation including discovery, and session-only context
resolution without rewriting kubeconfig. Model tests cover obsolete results,
stale snapshot retention, unknown resource errors, stable selection, inspector
focus/scroll, typed search, late log results, and terminal dimensions.

This does not qualify production-scale performance, every authentication
provider, every Kubernetes version, or a Windows terminal. Exec/OIDC support
uses client-go; provider-specific login flows were not tested. Tables use
polling and logs use bounded snapshots. At the initial milestone, no cluster mutation feature was shipped. Resource editing was added and qualified in the update below.

Reproduce the checks with `just check`, `just smoke`, and `just smoke-live`.
The live check needs Docker, k3d, kubectl, and network access for its pod image.

## YAML syntax highlighting

Verified on 2026-10-12: the native build, Linux amd64 build, race tests, and vet
passed after adding highlighting. Tests check exact displayed text and widths
across all 18 themes, including quoted strings, Unicode, comments, anchors,
numeric and boolean values, multiline block scalars, and trailing whitespace.
They also check secret redaction, terminal control sanitization, theme preview
and cancellation, and inspector scroll preservation.

Demo PTY checks passed again at 80x24, 180x48, and 80x40. The harness verifies
the active syntax colour on actual YAML keys, alongside navigation, scrolling,
and terminal restoration. The Kubernetes backend was unchanged for this update.

## Saved preferences and operator features

Verified on 2026-10-12 with 33 tests: race tests, vet, native build, and Linux
amd64 build passed. Tests cover YAML config migration and atomic writes,
comment/unrelated-setting preservation, saved view/namespace and flag precedence,
layout inheritance and saved columns, command/argument completion, fuzzy pod
names, stable connection labels, ten-second theme announcements, highlighted
JSON and inspector switching, aggregate logs/concurrency/bounds, and editor
argument parsing, unchanged edits, immutable identity, version conflicts,
strict YAML, fresh Secret data, and private retained edit files.

Demo and real Kubernetes PTY runs passed at 80x24, 180x48, and 80x40, including
restarts without view/namespace/layout flags. Restart checks also observed stable
connected labels across repeated refreshes and expiry of the theme name.
The live cluster ran v1.36.2+k3s1 with a standalone pod, a two-pod deployment,
a two-pod statefulset, a Secret, and a Widget CRD. Checks verified current logs,
aggregate workload logs, wrapped messages at 80 columns, editor handoff,
versioned annotation updates, highlighted JSON/YAML, and terminal restoration.
All Kubernetes writes targeted this disposable cluster, which was removed.

The editor fixture uses a real child process configured through YAML, changes a
resource annotation, and returns to the TUI. Provider-specific editors/auth flows
and continuous log streaming were not qualified. Shell support was investigated
separately and is not implemented.
