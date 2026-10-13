# ket

**The Kubernetes Exploration Tool** — a keyboard-first Kubernetes browser with
Maestro's terminal layout, themes, and Go UI libraries.

Inspired by [k9s](https://k9scli.io/), ket uses Bubble Tea v2, Bubbles v2, Lip Gloss
v2, and the official Kubernetes client-go. It talks directly to Kubernetes;
kubectl is not required at runtime.

## Run

Requires Go 1.27.1 or later; mise pins Go 1.27.1 for this repository.

```sh
go build -trimpath -o bin/ket ./cmd/ket
./bin/ket --demo                 # Offline sample cluster
./bin/ket                        # Current context and saved view / namespace
./bin/ket -A                     # Override saved scope: all namespaces
./bin/ket --context staging -n apps --theme nord
./bin/ket --view deploy --layout classic -l app=web
```

With mise and just, use `just build`, `just demo`, and `just check`.

## Binary builds

The **Build binaries** GitHub workflow runs on pushes (including tags), pull
requests, and manual dispatch. Download the matching artifact from the workflow
run's **Artifacts** section:

| Platform | Artifact |
| --- | --- |
| macOS Intel | `ket-darwin-amd64` |
| macOS Apple Silicon | `ket-darwin-arm64` |
| Linux x86-64 | `ket-linux-amd64` |
| Linux ARM64 | `ket-linux-arm64` |

Each artifact contains a `.tar.gz` archive and its `.sha256` checksum file. The
archive includes the executable, README, and theme license notices. Extract it
and run the `ket` executable inside, or copy it onto your PATH. Artifacts are kept
for 30 days. These are workflow downloads; releases are not published automatically.
Tag builds report the tag as their version; other builds report `dev-<commit>`.
All four targets are cross-compiled on Linux with CGO disabled.

## Kubernetes connection

`--kubeconfig` selects one kubeconfig file. Otherwise Kubernetes's normal
`KUBECONFIG` merging or `~/.kube/config` loading applies. `--context` overrides
current-context for this session; ket never rewrites kubeconfig. Exec credential
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
| `L`, `P` | Logs / previous logs from a pod, deployment, or statefulset |
| `e`, `:edit` | Open the selected resource in your editor |
| `t`, `:themes` | Fuzzy theme picker with reversible previews |
| `:layouts`, `:layout NAME` | Choose a saved layout |
| `:write NAME` | Save current arrangement, columns, sort, and wide state |
| `i`, `w`, `s` | Cycle inspector positions, wide fields, reverse sort |
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

Select a pod, deployment, or statefulset and press `L`. For a workload, ket reads
all pods matching its selector in its namespace. Multi-container resources offer
a container picker, including an all-containers option. Within logs, `c` selects
a container, `p` toggles previous logs, and `Ctrl+R` reloads.

Logs are bounded snapshots, not a continuous stream: up to 500 lines / 1 MiB per
source and 4 MiB combined. Workload output identifies each pod/container, reads
at most four sources concurrently, and shows individual source errors alongside
successful logs. Truncation is labelled. Logs wrap by default; `w` toggles wrapping, and `h/l` scrolls unwrapped lines.

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

Settings live in `$XDG_CONFIG_HOME/ket/config.yaml`, or
`~/.config/ket/config.yaml`. Missing files use defaults and are created when
preferences are saved. View and namespace are saved automatically, per context.
Theme selection, inspector format, and chosen layout are also saved. Launch flags
override saved values. A theme preview is not saved until Enter; Escape cancels it.
The theme name appears on the connection line for ten seconds after selection.

```sh
./bin/ket config                 # Print a YAML example
./bin/ket config --validate
./bin/ket config --validate --config ./my-config.yaml
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
`s` change sorting; `:write NAME` includes that sort in the saved profile.
JSON Pointer columns such as `/spec/replicas` and
`/metadata/labels/app.kubernetes.io~1name` are supported. Numeric fields sort
numerically; age uses creation time.

All 18 Maestro palettes are bundled. `./bin/ket themes` lists them. `--theme`
overrides `KET_THEME`, which overrides the saved theme. Custom themes retain the
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
stored in ket's UI config.

## Verification and remaining scope

```sh
go test -race ./...
go vet ./...
python3 scripts/tui-smoke.py --binary bin/ket
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

Pod shells, port forwarding, metrics, continuous log following, and informer
watches are not implemented. Resource refresh uses polling.
See [the design](docs/design.md), [verification](docs/verification.md), and
[the pod-shell assessment](docs/pod-shell-assessment.md).
