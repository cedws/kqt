# kqt

**A Kubernetes Query Tool** — a keyboard-first Kubernetes browser with
Maestro's terminal layout, themes, and Go UI libraries.

Inspired by [k9s](https://k9scli.io/), kqt uses Bubble Tea v2, Bubbles v2, Lip Gloss
v2, and the official Kubernetes client-go. It talks directly to Kubernetes;
kubectl is not required at runtime.

![kqt resource explorer with highlighted YAML](docs/screenshots/explorer.png)

## Run

Requires Go 1.27.1 or later; mise pins Go 1.27.1 for this repository.
With mise activated in your shell, the repository's mise configuration adds
`./bin/` to your `PATH` while you work here, so you can run the built binary as
`kqt`.

```sh
go build -trimpath -o bin/kqt ./cmd/kqt
kqt --demo                      # Offline sample cluster
kqt                             # Current context and saved view / namespace
kqt -A                          # Override saved scope: all namespaces
kqt --context staging -n apps --theme nord
kqt --view deploy --layout classic -l app=web
```

With mise and just, use `just build`, `just demo`, and `just check`.

## Binary builds

The **Build binaries** GitHub workflow runs on pushes (including tags), pull
requests, and manual dispatch. Pushing a [SemVer](https://semver.org/) tag such as
`v0.0.1` creates a [GitHub release](https://github.com/z0mbix/kqt/releases) after
the tests and all six builds pass. Tags with prerelease identifiers, such as
`v0.1.0-rc.1`, create prereleases. Invalid version tags fail before building.
Download the matching release archive, or use the workflow run's **Artifacts**
section for development builds:

| Platform | Artifact |
| --- | --- |
| macOS Intel | `kqt-darwin-amd64` |
| macOS Apple Silicon | `kqt-darwin-arm64` |
| Linux x86-64 | `kqt-linux-amd64` |
| Linux ARM64 | `kqt-linux-arm64` |
| FreeBSD x86-64 | `kqt-freebsd-amd64` |
| FreeBSD ARM64 | `kqt-freebsd-arm64` |

Each artifact contains a `.tar.gz` archive and its `.sha256` checksum file. The
archive includes the executable, README, and theme license notices. Extract it
and run the `kqt` executable inside, or copy it onto your PATH. Artifacts are kept
for 30 days; release downloads remain attached to their version. Releases also
include a combined `SHA256SUMS` file. From the download directory, verify the
chosen archive with `shasum -a 256 -c kqt-darwin-arm64.tar.gz.sha256` (adjust the
platform as needed).
Tag builds report the tag as their version; other builds report `dev-<commit>`.
All six targets are cross-compiled on Linux with CGO disabled.

## Kubernetes connection

`--kubeconfig` selects one kubeconfig file. Otherwise Kubernetes's normal
`KUBECONFIG` merging or `~/.kube/config` loading applies. `--context` overrides
current-context for this session; kqt never rewrites kubeconfig. Exec credential
plugins and OIDC are supported by client-go. Without a current context, a context
picker opens. UI preferences never silently select a different Kubernetes context.

## Explore

| Key | Action |
| --- | --- |
| `j/k`, arrows; `g/G`; `Ctrl+U/D` | Move, jump, page |
| `h/l`, arrows | Scroll wide tables or manifests |
| `:`; `Tab` / `Shift+Tab` | Command prompt; cycle completions forward / back |
| `/` | Fuzzy search; `web0003` matches `web-7c9f6d8b5-0003` |
| `n`, `:ns` | Namespace table; Enter selects one |
| `:ns apps`, `:ns -A`, `0` | Set namespace / all namespaces |
| `c`, `:ctx` | Search and switch kubeconfig contexts |
| `r`, `:resources` | Search APIs and discovered custom resources |
| `Enter` | Workload/service/node → pods; otherwise describe |
| `d`, `y` / `v`, `J` | Describe, highlighted YAML, highlighted JSON |
| `Tab`, then `J` / `y` | Focus inspector, then switch it to JSON / YAML |
| `L`, `P` | Live logs / previous log snapshots from a pod, deployment, or statefulset |
| `Space`, `/`, `f`, `o` in logs | Pause, search, switch live/snapshot, save displayed logs |
| `E`, `:events selected` | Events and conditions for the selected resource |
| `m`, `:metrics on` / `off` | Toggle pod/node CPU and memory columns |
| `s`, `:shell` | Open a shell in a running pod/container |
| `f`, `:forward LOCAL:REMOTE` | Forward a pod/service port to localhost |
| `F`, `:forwards` | View/manage forwards; `x` stops the selected forward |
| `e`, `:edit` | Open the selected resource in your editor |
| `t`, `:themes` | Fuzzy theme picker with reversible previews |
| `:layouts`, `:layout NAME` | Choose a saved layout |
| `:write NAME` | Save current arrangement, columns, sort, and wide state |
| `i`, `w`, `S` | Cycle inspector positions, wide fields, reverse sort |
| `Ctrl+R`, `Escape`, `?`, `q` | Refresh, back/close, help, quit |

Completion covers commands, resource names and aliases, theme/layout names,
contexts, known namespaces, and inspector/sort options. Tab only fills the
prompt; Enter executes. Printable navigation keys stay text in input fields.
Pickers use arrows or `Ctrl+J/K` while you type to search.

Examples: `:po`, `:deploy`, `:sts`, `:svc`, `:nodes`, `:pvc`, `:events`, `:crd`,
`:widgets.example.com`. Discovery uses preferred API versions, including CRDs.
If discovery is incomplete, a warning appears and built-in views remain usable.

`:labels app=web` applies a Kubernetes label selector at the API; `:labels`
clears it. Workload/service Enter follows real selectors; node Enter uses a node
field selector. Escape restores the parent route and selection.

Tables refresh every two seconds by default, with failure backoff. The connection
header stays **connected** during healthy background refreshes; **loading** is
reserved for connection/initial loading. Failed requests retain the last good
snapshot with a **STALE** label. Context and route changes cancel requests, clear
old data, and reject obsolete responses. Selection survives refresh and sort.

YAML and JSON use the active palette's syntax colours in full-screen panels and
inspectors. In the table, `J` opens JSON. With the inspector focused, `J` changes
only the inspector to JSON and `y` changes it back to YAML. In an open manifest
panel, `J` and `y` also switch formats. `:inspector json` / `:inspector yaml` are
available without changing focus. Secret values remain redacted throughout
these views. Highlighting preserves displayed text, quoting, and spacing.

## Logs

Select a pod, deployment, or statefulset and press `L`. For a workload, kqt reads
all pods matching its selector in its namespace. Multi-container resources offer
a container picker, including an all-containers option. Within logs, `c` selects
a container, `p` toggles previous logs, and `Ctrl+R` reloads.
Logs fill the main pane, keeping the cluster and namespace visible. Container
colour codes and other terminal escape sequences are removed before kqt applies
the selected theme. Highlighting is automatic for each line: complete JSON
objects/arrays get syntax colours, including severity colours for `level`,
`severity`, and `lvl` values. Plain-text logs get subtle timestamp/source colours
and highlighted levels such as `INFO`, `WARN`, and `ERROR`. Mixed formats work
together; incomplete JSON and other text stay readable. Messages keep their
original spacing and order, and saved logs contain plain text without colour codes.

Logs follow live by default. Workloads attach to new matching pods and stop
following removed pods; discovery reconciles every two seconds. Output from
multiple sources identifies each pod/container. A selected individual pod stays
bound to its UID; a replacement requires closing logs and selecting it again.
Streams reconnect using timestamps to avoid replaying already displayed lines.

`Space` pauses the display while collection continues in a bounded buffer. `/`
filters lines by text, ignoring case; `G` returns to following the bottom after
scrolling up. `o` saves the displayed, filtered buffer to a private file, refusing
to overwrite an existing file. Logs wrap by default; `w` toggles wrapping, and
`h/l` scrolls unwrapped lines.

`f` switches between live logs and snapshots. Previous logs (`P` / `p`) are
snapshots. Snapshots read up to 500 lines / 1 MiB per source, at most four sources
concurrently, and 4 MiB combined. Live collection starts with the last 200 lines
per source, retains at most 4 MiB / 10,000 lines, and follows at most 64 containers.
Discarded/truncated output and individual source errors are labelled. Closing
logs, switching contexts, and quitting cancel the streams.

## Events and metrics

`E` shows conditions and events for the selected resource, matched by immutable
UID and sorted newest first. `Ctrl+R` reloads the fresh resource and its events.
`:events` still opens the general events table; `:events selected` opens the
resource-specific panel. Events may have expired from Kubernetes retention.

`m` adds CPU and memory usage to pod/node tables. The cluster must provide the
[Metrics API](https://kubernetes.io/docs/tasks/debug/debug-cluster/resource-metrics-pipeline/),
normally through Metrics Server. Missing permissions, unavailable APIs, and
pending samples are reported without disrupting resource browsing. Usage sorts
numerically and is kept separate from the original YAML/JSON manifests.

Custom columns also accept `cpu`, `memory`, `cpu%`, and `memory%`. Percentages
compare pod usage against summed application-container limits, or node usage
against allocatable capacity. If any application container lacks the relevant
limit, the pod percentage is unavailable. Missing samples/limits display `—`.

## Shells and port forwarding

`s` opens `/bin/sh` in the selected running pod, offering a picker when several
containers are running. Configure another command with `shell: "/bin/bash"` in
the settings file. Arguments support quoting and run directly inside the chosen
container. Missing shells and Kubernetes permission denials are displayed.

The shell takes over the native terminal, including resize delivery, then returns
to kqt on exit. `Ctrl+C` goes to the remote process; `Ctrl+]` disconnects locally.
kqt verifies the pod UID immediately before opening the connection. Kubernetes's
exec endpoint addresses a pod by name, so that check is not an atomic UID
precondition. Shells use native client-go; kubectl is not required.

`f` prepares a forwarding command from the pod/service's declared TCP port, or
enter `:forward 8080:80` directly. A local port of `0` chooses a free port. Service
ports resolve named target ports against a ready matching pod; the actual pod and
local address are shown when ready. The selected pod stays fixed for that forward.

`F` / `:forwards` lists active and recently completed forwards. Select one to
inspect its destination and status; `x` stops it. `:forward stop ID` and
`:forward stop all` also work. Listeners bind only to `127.0.0.1`. Up to 16 forwards
can run concurrently; namespace changes retain them, context changes and quitting
close them. Forwarding uses native client-go. Shells and forwards try WebSocket
transport with SPDY fallback for compatible upgrade/proxy failures.

## Editing

`e` fetches a fresh, full resource and opens a private temporary YAML file in:

1. The config's `editor` command, if set.
2. `$EDITOR`, then `$VISUAL`, then `vi`.

Commands may contain quoted arguments, for example `code --wait` or `nvim -f`.
They run directly, without a shell. Use an editor's wait/foreground option so it
exits only after you finish editing.

Saving changes and exiting successfully applies a minimal JSON merge patch with
UID and resourceVersion preconditions. Exiting without changes performs no
write. Identity fields cannot change; status and managedFields are omitted and
cannot be edited. Kubernetes enforces schema validation and update permissions.
Conflicts, invalid YAML, or editor failures keep the private file and display its
path for recovery. Successful/unchanged edits remove the temporary file and
refresh the table. In demo mode, edits affect only the sample backend.

Secrets are redacted in browsing views; editing a Secret fetches its real data
so redaction placeholders cannot overwrite it. The edit file's directory and
file permissions are private. Pod logs and other resources may contain sensitive
content; ordinary Kubernetes RBAC still applies.

## Saved settings and layouts

Settings live in `$XDG_CONFIG_HOME/kqt/config.yaml`, or
`~/.config/kqt/config.yaml`. Missing files use defaults and are created when
preferences are saved. View and namespace are saved automatically, per context.
Theme selection, inspector format, and chosen layout are also saved. Launch flags
override saved values. A theme preview is not saved until Enter; Escape cancels it.
The theme name appears on the connection line for ten seconds after selection.

To keep settings from an earlier `ket` build, copy its `config.yaml` (or
`config.toml`) and `themes/` directory into `~/.config/kqt/`, respecting
`XDG_CONFIG_HOME` if set. Preserve any existing `kqt` configuration. Update
`KET_THEME` to `KQT_THEME` in your shell configuration.

```sh
kqt config                      # Print a YAML example
kqt config --validate
kqt config --validate --config ./my-config.yaml
```

```yaml
version: 1
theme: catppuccin-mocha
layout: troubleshooting
view: pods
namespace: default
inspector_format: yaml
refresh_interval: 2s
editor: "code --wait"

views:
  pods:
    columns: [namespace, name, ready, status, restarts, age, node]
    sort: name

layouts:
  troubleshooting:
    extends: inspector
    inspector_percent: 40
    wide: false
    views:
      pods:
        columns: [namespace, name, ready, status, restarts, node, age]
        sort: restarts
        descending: true
```

Built-in layouts are `classic` (no inspector), `inspector` (right pane at 120+
columns), and `bottom` (bottom pane at 24+ rows). Named layouts can inherit one
another, choose `none` / `right` / `bottom`, and override the inspector percentage,
columns, sorting, and wide mode. `:layouts` searches all built-in and saved names.
`:write NAME` saves the current configuration into the same YAML file; aliases
`:layout save NAME` and `:layouts NAME` also work. Smaller terminals collapse a
right inspector; full-screen details stay available.

`:columns name,status,age` saves columns for the active layout. `:sort age` and
`S` change sorting; `:write NAME` includes that sort in the saved profile.
JSON Pointer columns such as `/spec/replicas` and
`/metadata/labels/app.kubernetes.io~1name` are supported. Numeric fields sort
numerically; age uses creation time.

All 18 Maestro palettes are bundled. `kqt themes` lists them. `--theme`
overrides `KQT_THEME`, which overrides the saved theme. Custom themes retain the
Maestro TOML format under `themes/NAME.toml` beside the settings file:

```toml
version = 1
name = "company"
extends = "nord"
[colors]
accent = "#8be9fd"
```

[Palette attribution and upstream notices](internal/tui/THEMES.md) are retained.
Config errors and inheritance cycles fail visibly. Preference writes preserve
unrelated YAML settings and comments and replace the file atomically. An invalid
file is not overwritten. Restart to load manual configuration changes.

Existing default `config.toml` settings are loaded if no YAML file exists, then
carried into `config.yaml` on the first save; the old file stays unchanged.
Explicit TOML config paths are still supported. No connection credentials are
stored in kqt's UI config.

## Verification and remaining scope

```sh
go test -race ./...
go vet ./...
python3 scripts/tui-smoke.py --binary bin/kqt
```

`just check`, `just smoke`, and `just smoke-live` are provided. The live check uses
Docker, k3d, and kubectl to create a disposable cluster, apply controlled
fixtures, exercise the TUI, and clean up, with a separate temporary kubeconfig.
Optional `--snapshots artifacts/demo` records screen text and ANSI.

Tests cover API contracts, selectors, pagination, discovery, cancellation,
versioned editing, secret redaction, saved settings and migration, layout
inheritance, completion, fuzzy matching, syntax highlighting, theme timing,
inspector focus, terminal dimensions, and log aggregation. PTY checks cover
editor terminal handoff/restoration and saved-state restart as well.

Resource relationships beyond workload/service/node-to-pods, dedicated workload
actions, and informer watches remain future work. Resource refresh uses polling.
See [the design](docs/design.md), [verification](docs/verification.md), and
[the pod-shell assessment](docs/pod-shell-assessment.md).
