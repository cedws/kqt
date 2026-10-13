# ket: The Kubernetes Exploration Tool

ket carries Maestro's visual language into a Kubernetes explorer: compact
context header, framed tables, contextual footer, command prompt, fuzzy pickers,
independently scrollable inspector, and the same semantic palettes.

## Foundation

Use Maestro's pinned Charm versions: Bubble Tea v2.0.10, Bubbles v2.2.1,
Lip Gloss v2.0.6, and x/ansi v0.11.8. Palette values and notices came from
`maestro/internal/tui`. Official client-go v0.36.0 supplies kubeconfig loading,
authentication, discovery, dynamic resource reads/patches, and the typed core
log client. There is no runtime dependency on Maestro or kubectl.

References: [k9s](https://k9scli.io/),
[client-go](https://github.com/kubernetes/client-go),
[Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Sessions and presentation

- Contexts come from explicit flags or kubeconfig current-context; saved UI
  state never silently changes the Kubernetes target. Context switches are
  explicit, cancel requests, clear data, and restore that context's view/scope.
- Async results carry session/request tokens. Obsolete list, log, and editor
  results are discarded. Only one list request runs for a route at a time.
- Bounded polling defaults to two seconds with error backoff. The connection
  label reflects health, not every polling request's transient busy flag.
  Failed refreshes keep the last good snapshot and visibly mark it stale.
- Discovery includes listable preferred resources and CRDs, with built-in
  definitions retained after partial failures. Ambiguous names require a
  qualified API resource name.
- Namespace, workload selectors, and node field selectors are sent to the API.
  Selection is preserved by UID; relationship history restores scope and row.
- Fuzzy search matches each field independently and ranks subsequence matches,
  avoiding matches assembled from unrelated pod/restart/node digits. Printable
  navigation keys remain text in inputs. Tab completes only; Enter executes.
- YAML tokenization maps source tokens to existing theme colours without
  reserializing. If source text cannot be preserved, display plain text.
  JSON matches entire strings before numbers/punctuation. Secret redaction and
  control sanitation happen before highlighting. Both formats work in the
  inspector and panels; theme previews preserve inspector scrolling.
- The selected theme is announced for ten seconds, with versioned timer
  messages so an earlier timer cannot hide a later selection.

## Preferences and layouts

`~/.config/ket/config.yaml` (or XDG/explicit path) stores current view/namespace,
per-context state, theme, inspector format, editor, columns, and named layouts.
Explicit launch flags take precedence. Default TOML settings are migrated on
first save while leaving their source unchanged; custom palettes remain TOML.

Built-in and inherited custom profiles choose inspector arrangement/percentage,
per-resource columns/sort, and wide mode. `:layouts` selects a profile; `:write`
saves the current configuration. Cycles, invalid names, and invalid fields fail
validation. Changes patch the latest YAML tree, retaining unrelated values and
comments, then use a private temporary file and atomic rename. Failed writes
leave the current UI usable and display the error.

## Logs

Pod logs support live following and bounded snapshots. Deployment/statefulset
logs list actual matching pods in the object's namespace. Snapshots read the
selected container or all containers with four workers and a 4 MiB total bound.
Live logs reconcile membership every two seconds, retain timestamp checkpoints
across reconnects, and cap concurrent streams at 64. A serialized callback feeds
a bounded queue; the UI retains at most 4 MiB / 10,000 lines. Pausing freezes
rendering while collection continues. Search is a case-insensitive text filter;
exports write the displayed buffer to a new private file. Individual source
failures remain visible alongside healthy output. Single-pod streams stop on
UID replacement; context changes/closing the pane cancel all sources.

## Events, metrics, and connections

Selected-resource events use a UID field selector, fresh identity checks, and
newest-first ordering. Conditions accompany events. Metrics try the stable API
then the older beta API when absent. Metrics requests have a three-second bound;
failure never turns a successful resource read into a failed read. Usage remains
separate from unstructured resources, preserving YAML/JSON exactly. Sorting uses
numeric quantities; missing samples/limits remain unavailable.

Shells use client-go remotecommand through Bubble Tea Exec, handing the native
terminal over without an embedded emulator or kubectl dependency. A cancelable
input reader and bounded queue consume Ctrl+] even during connection setup.
Polling terminal size supplies the remote size queue, and all terminal input
workers end before returning to Bubble Tea. The selected running container is
revalidated against the pod UID immediately before exec.

Port forwards use client-go, expose only 127.0.0.1, and bind to one reported pod.
Service ports resolve numeric/named target ports against ready matching pods.
Up to 16 connections run concurrently, with bounded recent history. Context
changes and quitting cancel pending and active forwards. Both connection types
retain session TLS/authentication, prefer WebSocket, and use SPDY fallback for
compatible upgrade/proxy failures. A small upgrade wrapper closes the TCP socket
on context cancellation during a stalled handshake; the SDK owns established
streams. Handshakes are bounded without imposing that timeout on the session.

## Editor lifecycle

Editing is an explicit `e` / `:edit` action. Fetch fresh data and validate the
selected UID before preparing a private file. Full Secret data is used for
editing, while all UI views stay redacted. Pause polling and use Bubble Tea's
`ExecProcess` to release/reclaim the terminal around a directly executed editor
command, parsed with quote/escape support and no shell expansion.

On successful editor exit, strictly parse one YAML document, preserving numeric
precision. Resource identity and version cannot change; status and managedFields
are excluded. Compare with the editable source and perform no write if unchanged.
Otherwise send a minimal merge patch with original UID/resourceVersion
preconditions. Kubernetes supplies schema validation and authorization. Keep the
file on failure/conflict; delete it on success or an unchanged edit. Resume the
resource table and polling after return.

Editing remains the resource mutation action, and an explicit shell can change
container state. Informer watches, deeper resource relationships, and dedicated
workload actions remain future work. Qualification uses backend contract tests,
model tests, and PTY checks against demo and a disposable real Kubernetes cluster.
