package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	apihttp "k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog/v2"
	streamhttp "k8s.io/streaming/pkg/httpstream"
)

// Debugging deliberately has its own subresource API. It cannot create pods,
// change scheduling, read pull secrets, or fall back to exec.
type EphemeralDebugger interface {
	ResourceGetter
	ReviewAccess(context.Context, AccessRequest) (AccessDecision, error)
	PatchEphemeral(context.Context, unstructured.Unstructured, corev1.EphemeralContainer, bool) (unstructured.Unstructured, error)
	Attach(context.Context, unstructured.Unstructured, string, bool, remotecommand.StreamOptions) error
}

type DebugProfile struct {
	Image           string
	Command         []string
	PullPolicy      corev1.PullPolicy
	SecurityContext *corev1.SecurityContext
}

type DebugPlan struct {
	Pod                  unstructured.Unstructured
	Container            corev1.EphemeralContainer
	AttachOnly, SPDYOnly bool
	Warnings             []string
}

func DebugTargets(pod unstructured.Unstructured) []string {
	var result []string
	for _, field := range []string{"containers", "initContainers"} {
		for _, entry := range Slice(pod, "spec", field) {
			if value, ok := entry.(map[string]interface{}); ok {
				if name, ok := value["name"].(string); ok {
					result = append(result, name)
				}
			}
		}
	}
	return result
}

func DebugContainers(pod unstructured.Unstructured) []corev1.EphemeralContainer {
	var result []corev1.EphemeralContainer
	for _, entry := range Slice(pod, "spec", "ephemeralContainers") {
		if value, ok := entry.(map[string]interface{}); ok {
			var container corev1.EphemeralContainer
			if runtime.DefaultUnstructuredConverter.FromUnstructured(value, &container) == nil {
				result = append(result, container)
			}
		}
	}
	return result
}

func debugPod(ctx context.Context, backend ResourceGetter, selected unstructured.Unstructured) (unstructured.Unstructured, error) {
	r, _ := Resolve(Builtins(), "pods")
	pod, err := backend.Get(ctx, r, selected.GetNamespace(), selected.GetName())
	if err != nil {
		return pod, err
	}
	if selected.GetKind() != "Pod" || selected.GetUID() == "" || pod.GetUID() != selected.GetUID() {
		return pod, fmt.Errorf("selected pod was replaced; refresh and select it again")
	}
	if pod.GetDeletionTimestamp() != nil || pod.GetResourceVersion() == "" {
		return pod, fmt.Errorf("pod is terminating or has no resource version")
	}
	if _, mirror := pod.GetAnnotations()["kubernetes.io/config.mirror"]; mirror {
		return pod, fmt.Errorf("static mirror pods do not support ephemeral containers")
	}
	if Text(pod, "spec", "nodeName") == "" {
		return pod, fmt.Errorf("pod is not scheduled; ephemeral containers require an existing node assignment")
	}
	if phase := Text(pod, "status", "phase"); phase == "Succeeded" || phase == "Failed" {
		return pod, fmt.Errorf("pod has finished (%s)", phase)
	}
	if Text(pod, "spec", "os", "name") == "windows" {
		return pod, fmt.Errorf("debug profiles currently support Linux pods")
	}
	return pod, nil
}

func debugAccess(ctx context.Context, backend EphemeralDebugger, plan *DebugPlan) error {
	r, _ := Resolve(Builtins(), "pods")
	checks := []struct{ verb, sub string }{{"get", ""}, {"create", "attach"}}
	if !plan.AttachOnly {
		checks = append(checks, struct{ verb, sub string }{"patch", "ephemeralcontainers"})
	}
	checks = append(checks, struct{ verb, sub string }{"get", "attach"})
	for _, check := range checks {
		decision, err := backend.ReviewAccess(ctx, AccessRequest{Resource: r, Namespace: plan.Pod.GetNamespace(), Name: plan.Pod.GetName(), Verb: check.verb, Subresource: check.sub})
		if err != nil {
			if apierrors.IsForbidden(err) || apierrors.IsNotFound(err) {
				plan.Warnings = append(plan.Warnings, "Access reviews unavailable; the server will enforce permissions. Attachment permissions could not be verified.")
				plan.SPDYOnly = true
				return nil
			}
			return fmt.Errorf("check %s pods/%s: %w", check.verb, check.sub, err)
		}
		if decision.Allowed {
			continue
		}
		if check.verb == "get" && check.sub == "attach" {
			plan.SPDYOnly = true
			continue
		}
		resource := "pods"
		if check.sub != "" {
			resource += "/" + check.sub
		}
		return fmt.Errorf("permission denied: %s %s in namespace %s for %s: %s", check.verb, resource, plan.Pod.GetNamespace(), plan.Pod.GetName(), decision.Reason)
	}
	return nil
}

func PrepareDebug(ctx context.Context, backend EphemeralDebugger, selected unstructured.Unstructured, profile DebugProfile, target, attach string) (*DebugPlan, error) {
	pod, err := debugPod(ctx, backend, selected)
	if err != nil {
		return nil, err
	}
	plan := &DebugPlan{Pod: pod, AttachOnly: attach != ""}
	if err = debugAccess(ctx, backend, plan); err != nil {
		return nil, err
	}
	if attach != "" {
		for _, container := range DebugContainers(pod) {
			if container.Name == attach {
				plan.Container = container
				return plan, nil
			}
		}
		return nil, fmt.Errorf("ephemeral container %q no longer exists", attach)
	}
	if profile.Image == "" || len(profile.Command) == 0 || profile.SecurityContext == nil {
		return nil, fmt.Errorf("debug profile must specify an image, command and security context")
	}
	if target != "" && !slices.Contains(DebugTargets(pod), target) {
		return nil, fmt.Errorf("target container %q no longer exists", target)
	}
	container := corev1.EphemeralContainer{EphemeralContainerCommon: corev1.EphemeralContainerCommon{
		Name: "kqt-debug-" + rand.String(8), Image: profile.Image, Command: slices.Clone(profile.Command),
		ImagePullPolicy: profile.PullPolicy, SecurityContext: profile.SecurityContext.DeepCopy(), Stdin: true, TTY: true,
		TerminationMessagePath: "/dev/termination-log", TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}, TargetContainerName: target}
	base, preview, err := previewDebug(ctx, backend, pod, container)
	if err != nil {
		return nil, fmt.Errorf("debug dry-run failed (nothing created): %w", err)
	}
	for _, admitted := range DebugContainers(preview) {
		if admitted.Name == container.Name {
			plan.Pod = base
			plan.Container = admitted
			return plan, nil
		}
	}
	return nil, fmt.Errorf("debug dry-run did not return the proposed container")
}

func previewDebug(ctx context.Context, backend EphemeralDebugger, pod unstructured.Unstructured, container corev1.EphemeralContainer) (unstructured.Unstructured, unstructured.Unstructured, error) {
	base := pod
	for attempt := 0; ; attempt++ {
		preview, err := backend.PatchEphemeral(ctx, base, container, true)
		if err == nil || attempt == 2 || ctx.Err() != nil {
			return base, preview, err
		}
		var status apierrors.APIStatus
		if !errors.As(err, &status) || (status.Status().Code != 409 && status.Status().Code != 422) {
			return base, preview, err
		}
		fresh, getErr := debugPod(ctx, backend, pod)
		if getErr != nil {
			return base, preview, getErr
		}
		// JSON-patch identity tests can lose a race to a kubelet status update.
		// Only dry-runs may retry, only with a changed version and the same spec.
		if fresh.GetResourceVersion() == base.GetResourceVersion() {
			return base, preview, err
		}
		if !reflect.DeepEqual(debugSpec(fresh), debugSpec(pod)) {
			return base, preview, fmt.Errorf("pod specification changed during preflight; open debug again")
		}
		for _, existing := range DebugContainers(fresh) {
			if existing.Name == container.Name {
				return base, preview, fmt.Errorf("container %s already exists; use :debug attach %s", existing.Name, existing.Name)
			}
		}
		base = fresh
	}
}

func debugSpec(pod unstructured.Unstructured) map[string]interface{} {
	copy := pod.DeepCopy()
	unstructured.RemoveNestedField(copy.Object, "spec", "ephemeralContainers")
	spec, _, _ := unstructured.NestedMap(copy.Object, "spec")
	return spec
}

// ApplyDebug revalidates the preview, then performs exactly one persistent patch.
// A timeout may leave an ephemeral container behind; callers must offer reattach,
// never blindly retry creation or try to delete the workload to clean it up.
func ApplyDebug(ctx context.Context, backend EphemeralDebugger, plan *DebugPlan) (unstructured.Unstructured, error) {
	pod, err := debugPod(ctx, backend, plan.Pod)
	if err != nil {
		return pod, err
	}
	if plan.AttachOnly {
		return pod, nil
	}
	if !reflect.DeepEqual(debugSpec(pod), debugSpec(plan.Pod)) {
		return pod, fmt.Errorf("pod specification changed after preview; open debug again")
	}
	for _, existing := range DebugContainers(pod) {
		if existing.Name == plan.Container.Name {
			return pod, fmt.Errorf("container %s already exists; use :debug attach %s", existing.Name, existing.Name)
		}
	}
	check := *plan
	check.Pod = pod
	if err = debugAccess(ctx, backend, &check); err != nil {
		return pod, err
	}
	plan.SPDYOnly = check.SPDYOnly
	base, preview, err := previewDebug(ctx, backend, pod, plan.Container)
	if err != nil {
		return pod, fmt.Errorf("debug dry-run failed: %w", err)
	}
	if !sameDebugContainer(preview, plan.Container) {
		return pod, fmt.Errorf("admission changed the confirmed container; open debug again to review it")
	}
	created, err := backend.PatchEphemeral(ctx, base, plan.Container, false)
	if err != nil {
		return pod, fmt.Errorf("creating %s: %w; the request may have reached the server. Refresh and use :debug attach %s if present", plan.Container.Name, err, plan.Container.Name)
	}
	if !sameDebugContainer(created, plan.Container) {
		return created, fmt.Errorf("container %s was created with unexpected admission changes; inspect it before attaching", plan.Container.Name)
	}
	return created, nil
}

func sameDebugContainer(pod unstructured.Unstructured, wanted corev1.EphemeralContainer) bool {
	for _, container := range DebugContainers(pod) {
		if container.Name == wanted.Name {
			return reflect.DeepEqual(container, wanted)
		}
	}
	return false
}

func WaitDebug(ctx context.Context, backend ResourceGetter, selected unstructured.Unstructured, name string) (unstructured.Unstructured, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	last := "waiting for the kubelet"
	for {
		pod, err := debugPod(ctx, backend, selected)
		if err != nil {
			return pod, err
		}
		for _, value := range Slice(pod, "status", "ephemeralContainerStatuses") {
			entry, ok := value.(map[string]interface{})
			if !ok || entry["name"] != name {
				continue
			}
			state, _, _ := unstructured.NestedMap(entry, "state")
			if state["running"] != nil {
				return pod, nil
			}
			if terminated, ok := state["terminated"].(map[string]interface{}); ok {
				return pod, fmt.Errorf("debug container %s terminated: %v (exit %v)", name, terminated["reason"], terminated["exitCode"])
			}
			if waiting, ok := state["waiting"].(map[string]interface{}); ok {
				reason, _ := waiting["reason"].(string)
				message, _ := waiting["message"].(string)
				last = strings.TrimSpace(reason + ": " + message)
				if slices.Contains([]string{"ErrImagePull", "ImagePullBackOff", "InvalidImageName", "CreateContainerConfigError", "CreateContainerError", "RunContainerError"}, reason) {
					return pod, fmt.Errorf("debug container %s: %s; check the approved image, existing pod pull credentials and security policy", name, last)
				}
			}
		}
		select {
		case <-ctx.Done():
			return pod, fmt.Errorf("debug container %s: %s: %w; it may still start; use :debug attach %s", name, last, ctx.Err(), name)
		case <-ticker.C:
		}
	}
}

func (c *Client) PatchEphemeral(ctx context.Context, pod unstructured.Unstructured, container corev1.EphemeralContainer, dryRun bool) (unstructured.Unstructured, error) {
	if pod.GetUID() == "" || pod.GetResourceVersion() == "" {
		return unstructured.Unstructured{}, fmt.Errorf("pod identity and resource version are required")
	}
	entry, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&container)
	if err != nil {
		return unstructured.Unstructured{}, err
	}
	entries := append(Slice(*pod.DeepCopy(), "spec", "ephemeralContainers"), entry)
	patch, err := json.Marshal([]map[string]interface{}{
		{"op": "test", "path": "/metadata/uid", "value": string(pod.GetUID())},
		{"op": "test", "path": "/metadata/resourceVersion", "value": pod.GetResourceVersion()},
		{"op": "add", "path": "/spec/ephemeralContainers", "value": entries},
	})
	if err != nil {
		return unstructured.Unstructured{}, err
	}
	options := metav1.PatchOptions{FieldManager: "kqt-debug", FieldValidation: "Strict"}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	r, _ := Resolve(Builtins(), "pods")
	result, err := c.dynamic.Resource(r.GVR).Namespace(pod.GetNamespace()).Patch(ctx, pod.GetName(), types.JSONPatchType, patch, options, "ephemeralcontainers")
	if err != nil {
		return unstructured.Unstructured{}, err
	}
	return *result, nil
}

func (c *Client) Attach(ctx context.Context, selected unstructured.Unstructured, container string, spdyOnly bool, options remotecommand.StreamOptions) error {
	// Connection errors are returned to the UI. Late client-go close/heartbeat
	// diagnostics must not write over the restored resource browser.
	ctx = klog.NewContext(ctx, klog.Logger{})
	pod, err := debugPod(ctx, c, selected)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(DebugContainers(pod), func(e corev1.EphemeralContainer) bool { return e.Name == container }) || !slices.Contains(RunningContainers(pod), container) {
		return fmt.Errorf("ephemeral container %q is not running", container)
	}
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	request := c.streamCore.RESTClient().Post().Resource("pods").Namespace(pod.GetNamespace()).Name(pod.GetName()).SubResource("attach").VersionedParams(&corev1.PodAttachOptions{Container: container, Stdin: options.Stdin != nil, Stdout: options.Stdout != nil, Stderr: !options.Tty && options.Stderr != nil, TTY: options.Tty}, runtime.NewParameterCodec(scheme))
	cfg := c.streamingConfig(ctx)
	secondary, err := remotecommand.NewSPDYExecutor(cfg, http.MethodPost, request.URL())
	if err != nil {
		return err
	}
	if spdyOnly {
		return secondary.StreamWithContext(ctx, options)
	}
	primary, err := remotecommand.NewWebSocketExecutor(cfg, http.MethodGet, request.URL().String())
	if err != nil {
		return err
	}
	executor, err := remotecommand.NewFallbackExecutor(primary, secondary, debugStreamFallback)
	if err != nil {
		return err
	}
	return executor.StreamWithContext(ctx, options)
}

func debugStreamFallback(err error) bool {
	// client-go wraps even 401/403 responses as upgrade failures, without an
	// Unwrap method. Inspect the cause so an authorization denial is never used
	// as a reason to try another verb/transport.
	cause := err
	var apiUpgrade *apihttp.UpgradeFailureError
	var streamUpgrade *streamhttp.UpgradeFailureError
	if errors.As(err, &apiUpgrade) {
		cause = apiUpgrade.Cause
	}
	if errors.As(err, &streamUpgrade) {
		cause = streamUpgrade.Cause
	}
	if cause != nil && (apierrors.IsForbidden(cause) || apierrors.IsUnauthorized(cause) || strings.Contains(cause.Error(), "(403 Forbidden)") || strings.Contains(cause.Error(), "(401 Unauthorized)")) {
		return false
	}
	return streamFallback(err)
}
