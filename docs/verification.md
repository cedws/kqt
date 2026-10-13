# Initial verification

Verified locally on 2026-10-12 with Go 1.27.1 on macOS arm64.

- Native build passed; the executable is `bin/kqt`.
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
remain unqualified. Continuous logs and shells were outside this milestone;
their subsequent qualification appears below.

## Operator tools qualification (2026-10-13)

The new live-log, selected-event, metrics, shell, and forwarding implementation
passed 49 unit/contract/model tests with the race detector. The optional
`TestLiveOperator` qualification additionally passed in a disposable
v1.36.2+k3s1 cluster. Native build and all four CGO-disabled cross-builds passed:
darwin/amd64, darwin/arm64, linux/amd64, linux/arm64. Architecture was inspected;
cross-built executables were not run on every target platform.

Live API checks verified scoped events, pod/node metrics, actual shell command
output, missing-shell errors, and HTTP traffic through a service's named target
port. Both WebSocket and forced compatible-upgrade-failure SPDY paths passed.
A service account permitted pod reads but denied exec and forwarding; both
denials surfaced correctly. Scaling the fixture deployment to three pods while
following logs attached the new pod automatically, then the fixture returned to
two pods. Cancellation stopped log collection and closed forwarding listeners.

Unit checks cover stalled-handshake cancellation, both client-go upgrade-error
types, UID replacement rejection, timestamp replay suppression including distinct
equal-time lines, bounded queues, metrics quantity aggregation/numeric sorting,
raw manifest preservation, stale response rejection, export overwrite refusal,
and context-switch cleanup. Terminal input consumes a local disconnect before
the remote consumer starts reading, and exits before terminal ownership returns.

The existing three-layout PTY checks and saved-state restart checks passed against
the live fixture. `scripts/operator-smoke.py` passed in demo and live modes at
80x24. The live shell was resized to 132x36 and its remote terminal reported that
size; Ctrl+C interrupted a remote sleep, a subsequent command ran, normal exit
and Ctrl+] returned to kqt, and a missing shell produced a recoverable error.
Log pause/search/export, live/snapshot switching, scoped events, metrics columns,
forward inspection/stop, and automatic forwarding cleanup on quit all passed.
Terminal attributes matched their original values on exit. All disposable
clusters were removed; the user's default kubeconfig was not changed.

`go vet`, workflow linting, Python/shell syntax checks, and diff checks passed.
CI and `just smoke` now include the demo operator check; `just smoke-live` runs
the optional API qualification and live operator PTY check. Provider-specific
authentication refresh, every Kubernetes/proxy/server version, and exhaustive
network failure scenarios remain outside this coverage.

## Log presentation and FreeBSD builds (2026-10-13)

Regression checks reproduced coloured application logs through the live backend
and UI queue, and verified cleaned snapshots, search, and private exports. ANSI
colour, cursor, title, clipboard, and hyperlink sequences are removed completely.
Log views at 40x12, 80x24, 180x48, and 240x76 retain the connection header, fill
the main pane without inspector fragments, and fit the terminal width and height.
Closing logs restores the ordinary detail viewport dimensions.

Race tests, static checks, and the existing three-layout demo terminal checks
passed. FreeBSD amd64 and arm64 cross-builds with CGO disabled passed; executable
headers and Go build metadata confirm the target OS and architecture. The build
and release workflow now covers all six macOS/Linux/FreeBSD targets. FreeBSD
runtime and terminal behavior have not been qualified on a FreeBSD host.

## Mixed-format log highlighting (2026-10-13)

Race tests, vet, and the three-layout demo/operator PTY checks passed with mixed
JSON and plain-text demo logs. Regression tests across all 18 bundled themes
cover source/timestamp/level prefixes, JSON arrays, escaped strings, large integer
precision, severity values, Unicode widths, and fallback for malformed or partial
JSON. Ordinary prose and similarly named fields do not become severity labels.
Snapshot/live views retain exact cleaned text; searches span styled tokens and
private exports contain no colour codes. Cache eviction and theme invalidation
also passed. The refreshed log screenshot comes from an actual demo PTY capture
and was visually inspected for JSON and plain-text colours. Both FreeBSD targets
were rebuilt successfully with the final highlighting changes.

## Decoded Secret toggle (2026-10-13)

Race tests, vet, native build, and both FreeBSD cross-builds passed. `x` is checked
in describe/YAML/JSON panels and inspector views across the three layouts; tests
also verify UTF-8/multiline/binary decoding, unchanged original API data, default
redaction, preserved scrolling, and terminal dimensions down to 40x12. Reading
requires a fresh individual Secret fetch and rejects a replaced UID. Errors,
cancelled requests, late context/selection replies, and automatic refreshes are
covered. Closing details, changing selection/scope/context, quitting, and editing
hide the values; custom table columns stay redacted and reveal state never changes
the saved config. Editing still fetches original base64 values.

The three-layout demo PTY checks passed with reveal, JSON switching, hiding, and
closing a revealed panel; decoded values disappeared from the terminal screen
after hiding/closing. Existing operator terminal checks also passed. The live
fixture harness includes the toggle, but was not rerun against a live Kubernetes
cluster for this change.

## Readable status and events (2026-10-13)

`E` / `:status` now shows a themed status summary with workload counts, pod
container states/restarts/last exits, and conditions with reasons/messages/times.
Regression checks cover readiness versus negative-condition polarity, unknown
custom predicates, stale observed generations, blocked/incomplete deployments,
terminal control sanitation, generic custom status fields, and resources without
status. Rendering leaves original API objects unchanged. Scoped events are
filtered locally and sorted newest first without modifying backend-owned rows.
Event-read failures leave status visible; replaced UIDs and late responses are
rejected. `S` still reverses the table sort, and entering status hides temporarily
revealed Secrets.

Race tests, vet, native builds, and both FreeBSD cross-builds passed. The three-layout demo PTY checks cover
opening status, container summaries, event scrolling, refresh, and closing; the
operator terminal check also passed. Status views fit 40x12, 80x24, and 180x48,
replace the main pane, and preserve the connection header. The new status PNG
was captured from the actual demo PTY and visually inspected. The disposable
live-cluster harness was updated for the view, but not rerun for this change.
