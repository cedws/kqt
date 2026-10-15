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
rejected. At this milestone `S` reversed the table sort, and entering status hid temporarily
revealed Secrets.

Race tests, vet, native builds, and both FreeBSD cross-builds passed. The three-layout demo PTY checks cover
opening status, container summaries, event scrolling, refresh, and closing; the
operator terminal check also passed. Status views fit 40x12, 80x24, and 180x48,
replace the main pane, and preserve the connection header. The new status PNG
was captured from the actual demo PTY and visually inspected. The disposable
live-cluster harness was updated for the view, but not rerun for this change.

## Live events (2026-10-13)

`:events` follows a namespace/all-namespace list/watch stream by default. `E` /
`:status` keeps resource events live by UID and periodically fetches fresh status.
Contract tests cover paginated lists, opaque resource versions, bookmarks,
out-of-scope objects, repeat-count updates, deletions, reconnect after EOF, both
HTTP and stream 410 expiry, relist gap notices, permissions, idle and stalled
handshake cancellation, and independence from ordinary request timeouts. Helpers
handle both core and events.k8s.io event field shapes.

Race/model checks cover count coalescing, record/byte limits, chronological
eviction, oversized records, heap integrity, pause with continued collection,
follow versus browsing/inspector focus, search, private export/overwrite refusal,
save confirmations, stale replies, status-read failures, replaced resource UIDs,
view/scope cleanup, and terminal dimensions down to 40x12.

The race and static checks and three-layout demo/live terminal suites passed.
Event-specific demo checks exercised selected/global pause, search, export, and
namespace changes. A disposable Kubernetes v1.36.2+k3s1 cluster verified actual
watch add/update/delete notifications, repeat counts, permission denial, and
cancellation. Live terminal checks changed an event count while paused, verified
the frozen display, resumed to the updated count, and exported that update with
one displayed row. Existing logs, shells, forwarding, terminal restoration, and
saved-preference checks also passed. All disposable clusters were removed;
the user's default kubeconfig was not changed.

Native and both FreeBSD cross-builds passed. Refreshed status and event screenshots
were captured from actual demo PTYs and visually inspected. FreeBSD runtime,
production-scale load, and provider-specific watch authentication refresh remain
outside this qualification.

## Column ordering and mouse controls (2026-10-13)

`S` now opens resource status (with `E` retained), and `R` reverses the current
order. Headers accept mouse clicks; `O` cycles to the next visible column and
sorts ascending. Shift+Left/Right or `[` / `]` cycles backward/forward. `:sort`
supplies a searchable picker and explicit asc/desc
commands. Orders persist per resource/layout, including changing a previously
saved descending order back to ascending.

Race and model checks cover sorting through mouse/keyboard/commands, selection
preservation, search-ranking precedence, numeric counts, exact timestamps,
custom/Unicode columns and horizontal clipping, layout persistence, input/modal
isolation, ignored mouse releases/modifiers/borders, row/inspector selection,
wheel scrolling, hidden Secret data, and live-event pause/follow behavior.
Column cycling additionally covers wraparound, hidden sorts, custom/metric
columns, and a one-column table; cycling preserves selection and never reverses.
The three-layout demo PTY checks sent actual SGR mouse press/release sequences,
Shift+arrow sequences, `O`, `R`, and sort-picker input. Terminal reporting and
attributes restored on exit, and preference restart checks passed.

The live operator PTY check passed in a disposable Kubernetes v1.36.2+k3s1
cluster with mouse reporting enabled. Status/live events, logs, shell handoff,
remote resize, Ctrl+C, disconnect, forwarding, and terminal restoration passed;
the cluster was removed. Native and both FreeBSD cross-builds passed. The
Kubernetes watch transport is unchanged by this UI update. Actual pointer use in
every terminal emulator and FreeBSD runtime remain unqualified.

Release checks exposed a timing-sensitive timeout in the stream-expiry contract
test. Watch cleanup now cancels its HTTP request before stopping the decoder,
so a read already waiting for the next frame cannot delay reconnection. Coverage
includes both EOF and still-open stream errors, retains the checkpoint/count/
scope assertions, and reports the received states on timeout. Ten repeated race
runs passed on macOS, and the full race suite passed in a Linux amd64 Go 1.27.1
container.

## Command history and forwarding cancellation (2026-10-13)

The colon prompt retains the last 50 nonempty submitted commands in the config's
`command_history`. Up/Down recall older/newer entries without executing them;
returning past the newest entry restores the unfinished draft and its cursor.
Cancelled input and search text are excluded. Coverage includes the limit,
restart restoration, YAML/TOML saves, comment and editor-setting preservation,
completion after recall, and input isolation.

Forward cancellation now works from the selected pod/service's resource list
and details, as well as the forwards picker and its detail panel. Source and
resolved-pod UIDs keep service forwards associated with the correct resource;
recreated resources and unrelated forwards are excluded. Regression checks
cover wrapped cancellation errors, late readiness after a stop, genuine failure
reporting, and preserving the selected forward when filtered results change.

The terminal harness records window-size changes at transcript byte offsets so
old output retains its original wrapping during replay. Fixtures cover ASCII,
Unicode and colour sequences, scroll regions, and resizing without new output.
Live operator checks exercise real pod and service traffic, stop forwards using
`x` in the resource list, forwards list, and forward details, verify closed TCP
listeners, and check cleanup on quit.

The full race suite, static checks, eight Python tests, three-layout demo PTY
checks and restart checks passed. The demo and live operator PTY checks passed,
including history recall and draft restoration, pod/service cancellation from
each supported view, resize replay, terminal restoration, and cleanup on quit.
Live API checks passed with WebSocket and forced SPDY fallback in a disposable
Kubernetes v1.36.2+k3s1 cluster; the cluster and its volumes were removed.

## v0.1.0: resource relationships

Relationship contract/model checks cover owner chains and cycles, replaced
selected resources/owners, namespace boundaries, exact-name navigation, service
selectors, ingress backends, PVC/PV references, cancellation, and obsolete replies.
The demo includes owner chains, ingress, and bound storage fixtures. The full
race/static/Python checks passed before this feature's commit; a dedicated PTY
check exercises the picker and navigation through workloads, services and storage.

## v0.1.0: manifest copy and export

Round-trip checks verify YAML/JSON data fidelity, clean metadata/status, source
immutability, Secret redaction and canonical revealed data. File checks cover
private permissions, quoted paths, exclusive creation and cancellation. The PTY
workflow checks a real OSC52 request, reusable JSON, overwrite refusal and export
from a JSON panel; clipboard acceptance by every terminal remains unqualified.

## v0.1.0: workload actions

Action checks cover scale/restart, CronJob suspend/resume/manual Jobs, deletion,
invalid inputs, exact-name confirmation, cancellation and obsolete replies.
HTTP contracts assert namespace routing and UID/resourceVersion preconditions
on patches and deletes. Manual Jobs preserve the template, labels, annotations
and CronJob controller reference without requiring finalizer permissions.
The demo terminal check exercises every action and verifies exported API values.

The action race/static checks and demo PTY flow passed. Live qualification in a
disposable Kubernetes v1.36.2+k3s1 cluster verified scale, rollout restart,
CronJob suspend/resume, real manual Job creation and preconditioned deletion.
Status-only version advances are accepted; changed specifications are rejected.
The write uses a fresh resource version, with bounded retries for API conflicts.
The test cluster and volumes were removed after qualification.

## v0.1.0: live resource tables

The generic list/watch lifecycle retains paginated checkpoints, bookmarks,
reconnect/backoff, expiry recovery and cancellation. Resource resyncs stage a
complete replacement snapshot; missing/deleted rows cannot survive a relist.
A bounded channel applies backpressure instead of dropping resource updates.
Model checks cover changes, selection, obsolete scope messages, denied-watch
polling fallback and clearing stale revealed Secrets. Native contracts cover
namespace/label filtering, cluster-scoped URLs and still-open expired streams.
Existing event fixtures now include the labels their API selector requests.

Full race/static checks, the three-layout/restart PTY suite and all four feature
PTY flows passed. A disposable real cluster confirmed watch add/update/delete
notifications, immediate table updates and live refresh of an open JSON panel.
The watch indicator remains visible alongside discovery warnings. The test
cluster and its volumes were removed after qualification.

## v0.1.0: permissions and read-only mode

Native HTTP contracts verify SelfSubjectAccessReview scope/subresources, allowed
and denied decisions, unavailable/malformed reviews and SelfSubjectReview
identity. Model tests cover local mutation gates, confirmation rechecks, config/
flag precedence, usable exports, clear mode labels and obsolete replies. The
read-only PTY workflow checks the permission panel, blocked edit/action/shell
commands, a single-action check and a successful private export.

Full race/static checks and the read-only PTY flow passed. A real limited service
account reported its actual service-account identity, allowed pod reads/listing,
and denied patch/delete/exec checks in a disposable cluster. Single-action
refresh preserves its query. The test cluster and volumes were removed.

## v0.1.0: cluster overview

Overview checks cover namespace/all-namespace counts, failing pods and rollout
progress, node pressure, exact quantity aggregation, sample coverage and partial
permission failures. Missing sections and metrics stay unavailable instead of
becoming healthy zero values. The UI checks cover rendering, problem navigation,
refresh and obsolete replies. The summary uses bounded parallel reads and a
five-second minimum refresh interval, independent of resource-table watches.

Overview race/static and terminal checks passed. Real-cluster qualification
verified pod/workload/node counts and reported allocatable quantities; a limited
reader retained allowed pod data while node and workload sections were marked
unavailable. Both test identities used a disposable cluster, which was removed.

Final v0.1.0 qualification passed the full race/static/Python suite, all six
feature PTY workflows, three demo layouts and restart checks. The complete
real-cluster run passed existing API/operator/editor/layout tests, native shell
and forward cleanup, external watch-driven table/JSON updates, limited-reader
permission and overview checks, and workload mutations. Its disposable cluster,
network and volumes were removed. FreeBSD/Linux runtime and terminal clipboard
acceptance remain platform-specific qualification limits.

## v0.2.0: metadata tabs and additional themes

Metadata checks cover fresh resource reads, labels/annotations/finalizers,
sorted keys, multiline and Unicode values, empty sections, terminal-control
sanitization, Secret redaction, private active-tab exports, and obsolete replies.
Tab switching retains the resource and event session; metadata does not follow
hidden events. Refresh preserves the active tab. Pinned tabs and content fit
40x12, 80x24, and 180x48 terminals.

Ayu Dark/Light/Mirage and all six Everforest background variants bring the
bundled theme count to 27. YAML/JSON/log highlighting checks cover every theme.
An isolated demo terminal verified all nine new themes through the picker,
saved preferences, JSON and metadata views, cancelled previews, and terminal
restoration. Foreground, muted, accent, and status text contrast was checked
against both panel and main backgrounds; the selected-row foreground was also
checked. All new palettes meet 4.5:1 for those text/background pairs.

The full race/static/Python suite, three-layout and restart terminal checks,
operator checks (including metadata tabs, reload and export), and all six
existing feature terminal workflows passed before the release commit.

## Configurable download directory

Checks cover YAML/TOML persistence, comment preservation, invalid settings,
home expansion, relative destinations, absolute overrides, new directory/file
permissions, overwrite protection, cancellation and captured save destinations.
All manifest, log, event, status and metadata prompts share the same path rules.
Long paths retain their filenames during terminal resizes at 40, 80 and 120
columns. Setting the directory or cancelling a prompt creates no export folders.

The race/static/Python suite and existing operator terminal workflow passed.
The downloads terminal workflow passed setting and clearing a directory with
spaces, every export type, unchanged default prompts, direct relative exports,
absolute overrides, collision protection, and resizing a save prompt. It runs
in CI and `just smoke` with a temporary config and isolated export directory.

## Log focus, following and mouse copy

`w` toggles wrapping and retains the choice through log reloads. `s` toggles
following the newest lines while collection continues. `f` switches to a
borderless logs-only terminal; Escape leaves focus before closing logs. The
live/snapshot switch is now uppercase `F`. Search and save prompts work in focus.

Dragging readable pane text highlights it and sends an OSC52 clipboard request
on release, with a 1.2-second bottom-right confirmation. Copy strips styling and
padding, preserves original hard line breaks across soft wraps, and handles
wide glyphs, combining characters and emoji. Log arrivals keep collecting in the
bounded buffer while a held drag freezes rendering. Single clicks still select
rows, focus panes and sort columns. Changed views and hidden Secret values cannot
be copied from a stale selection.

The full race/static/Python checks and existing three-layout, saved-state restart,
operator and feature PTY suite passed. Focused tests cover 40x12, 80x24 and
180x48, scrolling, loading/errors, pause, container-picker cancellation,
readable modal and inspector geometry,
Secret redaction and flash expiry. The new logs-controls PTY workflow checks real
mouse gestures, exact OSC52 payloads, live arrivals during selection, focus
resizing, search/save, and terminal restoration. CI and `just smoke` run it.
The screen recorder now handles erase-display commands used when leaving the
framed view. Clipboard acceptance depends on the terminal's OSC52 support; the
PTY tests verify requests without changing the user's system clipboard.

## Deployment rollout history and context aliases

`H` / `:rollouts` lists retained Deployment revisions newest first, including
images and change causes. Revision details show highlighted pod templates and
field comparisons against the Deployment snapshot taken at history load. `r`
or `:rollback REVISION` uses the existing exact-resource confirmation flow.
Rollback restores the template without overwriting replica count or strategy.
Read-only mode permits browsing and comparison while blocking rollback.

Tests cover revision ownership/namespace filtering, numeric ordering, missing
revisions, permission errors, replacement identities, template changes, paused
Deployments and exact template restoration with removed fields. An HTTP test
checks the real client-go request path, merge-patch type, UID/resourceVersion
preconditions and preservation of replicas. The isolated demo PTY workflow
exercises revision browsing, comparison, cancellation, wrong confirmation text,
confirmed rollback and export of the restored manifest. This does not claim
live-cluster controller reconciliation qualification.

Context aliases are stored in `context_aliases` and resolve to canonical
kubeconfig context names. The header shows the short alias; the picker includes
the real name, and saved route state remains attached to the canonical name.
`:alias NAME [CONTEXT]`, `:alias`, `:unalias NAME`, `:ctx ALIAS` and
`--context ALIAS` are supported without modifying kubeconfig. Tests cover
YAML/TOML persistence and removal, CLI resolution, completion, collisions,
missing targets, picker selection and canonical saved state. The contexts PTY
workflow checks saving, listing, completing, switching and removing an alias.

## Successful command history and reverse search

The last 50 successful commands are retained in submission order. Invalid names
and arguments, failed API/file requests and cancelled actions are excluded.
Asynchronous results are associated with their original context and operation
token, so superseded callbacks cannot add old commands to history. Actions enter
history only after successful completion; log commands use a stream-open signal,
which also handles successful idle streams with no output. Native Bubble Tea
editor, shell and clipboard control messages retain their normal lifecycle.

In the command prompt, `Ctrl+R` starts incremental case-insensitive history
search. Repeated `Ctrl+R` / Up finds older matches; `Ctrl+S` / Down finds newer
ones. Enter executes, Tab accepts for editing, and Escape / Ctrl+G restores the
original draft and cursor. Ctrl+R retains refresh behavior outside this prompt.
Tests cover failed/cancelled/superseded results, delayed export ordering,
persistence, bounds, reverse search, exhaustion, Unicode, backspace, empty
history, cancellation and terminal resizing. The command-history PTY workflow
checks actual control-key input, matching/accepting/running, missing matches,
draft restoration and failure exclusion in the saved config.

The full race/static/Python checks and existing three-layout, restart, operator
and feature terminal suite passed. The new workflows run in CI and `just smoke`.

## Shortcut hint colours

Shortcut keys use the active theme's accent colour and bold text; the colon and
description use the existing muted colour. This applies to footers, picker
hints, panel headings, inspector hints, resource tabs, history search and the
shell disconnect hint. The active resource tab remains bold and underlined.
Resource names and command-history text remain literal, sanitized text.

The `?` / `:help` view uses the same colours and colon separators for its
shortcut column, preserving description alignment. Its colours follow theme
changes without resetting the scroll position. The terminal suite checks both
entry points with Nord key and description colours.

Checks cover all 27 themes and 40-, 80- and 180-column views, including modal
composition after colour resets. Full race/static/Python checks passed. Terminal
tests check the actual foreground colours on shortcut keys and descriptions,
with the existing three-layout navigation/restart checks, operator workflow,
log controls, rollout history and reverse command search.

In the status panel's Annotations tab, valid
`kubectl.kubernetes.io/last-applied-configuration` JSON is indented and highlighted.
Formatting preserves key order and exact number literals without changing the
source object. Invalid JSON falls back to sanitized plain text, and Secret
redaction runs before formatting. Regression checks cover nested objects and
arrays, large numbers, malformed values, redaction and plain metadata exports.

Labels and annotations use compact, sorted `key: value` rows without blank lines
between entries. Multiline values and formatted last-applied JSON stay indented
under their keys. Existing metadata checks cover empty values, multiline content,
Unicode, redaction, exports and terminal sizing with this layout.
Finalizers use the same two-space indentation and highlighted names in compact
YAML-style list rows, preserving their API order.

## Log filtering and history controls (2026-10-15)

The log filter accepts literal includes/excludes, Go regular expressions,
severity lists/thresholds, and exact JSON fields with nested object paths.
Regression tests cover mixed formats, escaped JSON, precise large numbers,
quoted values, invalid expressions, bounded caches, and highlighting that keeps
Unicode graphemes and terminal widths intact. Filters survive log reloads and
container/live/previous switches; a newly selected resource resets the filter.

`O` opens saved log options with preset/custom time windows and line counts;
`T`/`N` toggle timestamps and source prefixes without restarting collection.
HTTP tests verify initial per-container limits, previous snapshots, zero/all
line counts, cancellation, and reconnect checkpoints that supersede the initial
window/tail. Demo aggregation checks verify limits reach each source. YAML/TOML
tests cover partial defaults, private saves, validation, comments, and restoring
the original live/snapshot line limits. Paused output stays frozen when display
options change; copies/exports follow the selected presentation.

Full race tests, static checks, and ten Python checks passed. The complete
three-layout terminal suite, operator workflow, and feature workflows passed,
including new `log-queries` and `log-options` scenarios for mixed JSON/plain
filters, plain exports, invalid input in focus mode, custom windows/counts,
saved defaults, and terminal restoration. Kubernetes request behavior was
qualified with local HTTP fixtures; these additions were not exercised against
an external cluster in this run.
