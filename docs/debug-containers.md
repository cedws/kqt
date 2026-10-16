# Ephemeral debug containers

Select a Pod and press **B**, or run `:debug PROFILE`. kqt uses the saved image,
checks permissions/admission automatically and shows a short summary. Press
**Enter** to start on the selected Pod. You do not need to retype the Pod name.
The target container is chosen automatically, honoring the Pod's default-container
annotation when it is running, then the first running regular/init container.
Press **c** to choose a different target, or **d** for full admitted settings.
Escape from the optional container picker returns to the summary.
`<pod network>` uses the pod's network without requesting another container's process namespace. Targeting a container
depends on container-runtime support; a non-root debugger may have limited access
to another container's processes even when targeting works.

The first time you debug without any configured profiles, kqt asks which image
to use. Choose **Use my own image** to paste your approved registry image, or
explicitly choose **Use BusyBox** for Docker Hub's public BusyBox 1.37 image.
Choosing BusyBox saves that explicit choice immediately. For your own image,
paste it once and press Enter to save. Both choices continue to the short start
summary. Escape from the image prompt leaves configuration unchanged.
Later sessions reuse the saved profile; there is no automatic public-registry
fallback. Fully qualified registry references are required; pinning a sha256
digest is recommended for reproducible tools.

The BusyBox option explicitly saves a non-root UID of 1000 so it can use the
restricted security defaults. An organisation's admission policy can still
reject this UID or public image; choose a compatible approved image/profile.
Your own image inherits the normal profile defaults and the Pod's UID settings.

You can also configure profiles manually:

```yaml
debug:
  default_profile: tools
  start_timeout: 90s
  profiles:
    tools:
      image: 123456789012.dkr.ecr.eu-west-2.amazonaws.com/platform/debug-tools:approved
      command: ["/bin/sh"]
      image_pull_policy: IfNotPresent
      security_context:
        runAsUser: 1000
        runAsGroup: 1000
  context_profiles:
    production: tools
  namespace_profiles:
    production:
      payments: tools
```

Replace the example with your registry/repository and a real approved tag or
digest. The image must contain the configured executable and support the chosen
UID. Commands are argument arrays, not locally evaluated shell strings. Profile
names and `attach` complete with Tab in the colon prompt. Changes to the config
file take effect after restarting kqt.

Selection order is explicit `:debug PROFILE`, namespace override, context
override, then `default_profile`. Context keys use actual kubeconfig names,
including when a display alias is active. With profiles but no matching default,
**B** uses the sole configured profile automatically, or opens a picker if
multiple profiles exist. `start_timeout` accepts 1s–10m and bounds creation
and waiting for startup; an interactive session has no such deadline.

## Security and placement

Defaults are `runAsNonRoot: true`, `allowPrivilegeEscalation: false`, capabilities
`drop: [ALL]`, and seccomp `RuntimeDefault`. `security_context` accepts Kubernetes
SecurityContext fields with their usual camelCase names and rejects unknown
fields. Explicit overrides are shown in the preview; cluster admission policy
remains authoritative. No automatic privileged retry is performed.

An ephemeral container joins the existing Pod. It does not create or schedule a
new Pod, so existing node assignment and tolerations apply unchanged. It uses the
Pod's existing service-account and image-pull configuration. kqt does not read
Secret values, retrieve local AWS tokens, inject pull credentials or add pod-level
`imagePullSecrets`. Configure ECR access through your cluster's existing kubelet
credential provider, node/Pod execution role or Pod pull secrets. Network policy,
registry allowlists, image-signature rules and admission webhooks still apply.

kqt checks the selected Pod UID, then performs name- and namespace-scoped access
reviews and a server-side dry-run. Before applying, it checks the Pod again,
repeats admission validation and compares the admitted container to the confirmed
preview. The persistent JSON patch tests both UID and resource version and only
changes `spec.ephemeralContainers` through the `ephemeralcontainers` subresource.
A conflict or ambiguous network failure is reported without retrying creation.
Dry-run checks may retry up to three times after harmless status/version changes,
while retaining the same Pod UID and workload specification.
Startup is observed with GET on that Pod; no node reads or Pod list/watch are
required by the debug operation itself.

## Permissions

The debug operation needs `get` on the selected Pod, `patch` on
`pods/ephemeralcontainers`, and `create` on `pods/attach`. The WebSocket path also
uses `get` on `pods/attach`; if only the create permission is available, kqt uses
SPDY POST directly. An explicit create denial always stops preflight; kqt does not
use a legacy GET path to bypass it. Reattachment does not need ephemeral-container
patch permission. `pods/exec`, Secret access, node access and Pod creation are not
needed. The rest of the explorer requires its usual resource browsing permissions.

For example, administrators can add this namespace-scoped Role to an appropriate
RoleBinding. `resourceNames` can further restrict access to approved Pod names.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kqt-debugger
  namespace: payments
rules:
  - apiGroups: [""]
    resources: [pods]
    verbs: [get]
  - apiGroups: [""]
    resources: [pods/ephemeralcontainers]
    verbs: [patch]
  - apiGroups: [""]
    resources: [pods/attach]
    verbs: [get, create]
```

SelfSubjectAccessReview is normally available to authenticated users. If access
reviews themselves are forbidden/unavailable, the preview explicitly says that
attachment rights could not be verified. Creation still requires a successful
server dry-run and confirmation, and actual API calls enforce authorization.
Other access-review errors stop the operation. A webhook that rejects dry-run
must be addressed by the cluster administrator; kqt does not bypass that check.
`--read-only` disables both creation and interactive reattachment.

## Lifecycle and failures

Use `exit` to end a shell, or **Ctrl+]** to disconnect locally. Disconnecting
does not stop the container. Reconnect with `:debug attach`; it opens the sole
existing ephemeral container directly, or offers a picker when several exist.
`:debug attach NAME` opens a specific one. Reattachment does not ask for a second
confirmation or require an image profile. An existing completed container cannot be restarted. Preview,
cancelled and failed commands are excluded from command history.

The ephemeral entry remains in the Pod spec for that Pod's lifetime, even after
exit. Kubernetes does not support removing it or setting individual resource
requests/limits. It may consume the Pod's available resources. Cancelling a start
or losing a response can leave a container running; refresh and reattach if it
exists. kqt never deletes or restarts the workload to clean up a debugger.

Image pull and container-configuration errors include the kubelet's reason.
Admission denials include the API error. Static/mirror Pods, unscheduled or
finished Pods, terminating Pods and explicitly Windows Pods are rejected.
This phase supports in-place Linux ephemeral debugging; it does not create
copied Pods or node-debug Pods.

## Verification

`just check` covers configuration, scoped access checks, identity-fenced patches,
admission drift, startup failures, cancellation, transport selection, preview
geometry and history. `just smoke` includes a simulated terminal workflow for
creation, disconnect, reattach and exit. The demo never pulls an image or runs a
local shell.

`just smoke-live` creates its own disposable k3d cluster. `TestLiveDebug` creates a
Restricted namespace and a Pod with a toleration, taints the disposable node, and
uses an impersonated service account with only name-scoped get/patch/attach
permissions. It verifies WebSocket and SPDY attachment, disconnect/reattach and
rejected privileged admission. The fixture explicitly uses a public BusyBox image
only inside that disposable cluster; it does not qualify private ECR credentials
or an organisation's admission policy.

References: [Kubernetes ephemeral containers](https://kubernetes.io/docs/concepts/workloads/pods/ephemeral-containers/),
[Pod subresource API](https://kubernetes.io/docs/reference/kubernetes-api/core/pod-v1/#patch-patch-ephemeralcontainers),
[API dry-run](https://kubernetes.io/docs/reference/using-api/api-concepts/#dry-run).
