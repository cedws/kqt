# Pod shell assessment

Investigated on 2026-10-12. The native client-go approach was implemented and
qualified on 2026-10-13. `s` now opens a selected running container's shell, and
`S` reverses sorting. See [verification](verification.md) for tested behavior
and remaining qualification limits.

For the separately configured, explicitly confirmed **B** action added later,
see [ephemeral debug containers](debug-containers.md). The existing shell action
does not automatically inject a debugger when exec fails.

## What k9s actually does

Inspected the public source at commit
`8f29fba8c97d70008617ed63032f2ef508602c99`.

The pod `s` action checks that the pod is running and resolves the container,
using a default-container annotation, a single container, or a picker. It stops
the resource viewer, starts the shell, and resumes the viewer afterward. Shell
arguments use `kubectl exec -it` with the selected namespace/container and the
active context/kubeconfig. Linux containers get a bash-or-sh fallback; Windows
containers get a different shell selection. See
[the pod shell flow](https://github.com/derailed/k9s/blob/8f29fba8c97d70008617ed63032f2ef508602c99/internal/view/pod.go).

The important simplification is that k9s launches the installed **kubectl**
binary, suspends its TUI, and gives the subprocess the native terminal's input
and output. It does not implement an embedded terminal for this feature. The
wrapper forwards context and relevant authentication flags and displays a target
banner. See
[the execution wrapper](https://github.com/derailed/k9s/blob/8f29fba8c97d70008617ed63032f2ef508602c99/internal/view/exec.go).

## Options for kqt

| Approach | Complexity | Product effect |
| --- | --- | --- |
| Follow k9s: optional `kubectl exec -it` subprocess | Lowest; reuse kqt's tested editor terminal handoff, container picker, argument construction, and refresh pause/resume | Shells require kubectl. Browsing/editing still work without it. Must pin the selected context and configuration rather than rereading an unrelated CLI default. |
| Use client-go `remotecommand` through Bubble Tea `Exec` | More terminal/streaming work, still a contained addition | Keeps kqt standalone and uses the current authenticated session configuration. Need raw mode, resize delivery, cancellation, protocol fallback, and terminal restoration tests. |
| Embed a terminal inside an inspector | Considerably larger | Introduces terminal emulation/rendering/input concerns that this request does not need. |

client-go already supplies TTY-capable shell streams, context cancellation,
WebSocket and SPDY executors, fallback, and a terminal-size queue.
[Official remotecommand API](https://pkg.go.dev/k8s.io/client-go@v0.36.0/tools/remotecommand).
The pinned local v0.36.0 source includes these APIs. kqt already uses Bubble Tea's
external-I/O terminal lifecycle for editing, and x/term already provides raw-mode,
restoration, and terminal dimensions. An embedded terminal is unnecessary.

## Selected implementation

kqt uses **client-go remotecommand with a native terminal handoff**. That preserves
the no-kubectl runtime requirement and the authenticated session/target contract.
The addition consists of an exec backend, terminal/input adapters, and a running
container picker. The current default command is `/bin/sh`, configurable through
`shell` in the user's YAML settings. Distroless/missing shells fail normally.

If shortest implementation time takes priority, the k9s-style optional kubectl
path is also reasonable. It should be explicitly optional and preserve the
session's resolved configuration, not merely the mutable kubeconfig default.
Keep credential snapshots private if that approach needs a temporary kubeconfig;
do not pass bearer tokens as visible process arguments.

For the native implementation:

- `s` acts only on the selected running pod/container. Offer a container picker
  for multi-container pods; do not silently choose another pod or context.
- Show the exact context, namespace, pod, and container before terminal handoff.
  Pause list refreshes and bind streaming to the captured session context.
- Request a TTY through the pod exec subresource and stream directly to the
  native terminal, using a WebSocket executor with an appropriate SPDY fallback.
  Use a dedicated streaming configuration rather than ordinary list timeouts.
- Restore terminal attributes on every exit/error/cancellation. Forward terminal
  size changes, then resume the resource browser and refresh the selected pod.
- Respect Kubernetes pod-exec authorization. A shell is an explicit operator
  action and can change container state; do not turn a denied/missing shell into
  privileged execution, node access, or injected debug containers.
- Support a configurable shell command with a small bash/sh default. Report a
  missing shell normally; distroless images do not necessarily contain one.
- Reverse sorting uses `S`; `:sort` remains available.

Normal exit, Ctrl+C, Ctrl+] disconnect, resize, missing shell recovery, RBAC
denial, and both transport paths passed disposable-cluster checks. Unit tests
cover stale UID rejection, session cancellation, canceled handshakes, and input
worker shutdown. Provider-specific authentication refresh and exhaustive network
failure scenarios remain outside the verified coverage.
[Kubernetes shell task](https://kubernetes.io/docs/tasks/debug/debug-application/get-shell-running-container/)
provides the baseline pod/container exec workflow.
