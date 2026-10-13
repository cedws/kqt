package kube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"
	streamhttp "k8s.io/streaming/pkg/httpstream"
)

type PodExecutor interface {
	Exec(context.Context, unstructured.Unstructured, string, []string, remotecommand.StreamOptions) error
}
type PodForwarder interface {
	Forward(context.Context, unstructured.Unstructured, string, func(uint16)) error
}

func RunningContainers(pod unstructured.Unstructured) []string {
	var names []string
	for _, field := range []string{"containerStatuses", "initContainerStatuses", "ephemeralContainerStatuses"} {
		for _, value := range Slice(pod, "status", field) {
			state, ok := value.(map[string]interface{})
			if !ok {
				continue
			}
			current, _ := state["state"].(map[string]interface{})
			if _, running := current["running"]; running {
				if name, ok := state["name"].(string); ok {
					names = append(names, name)
				}
			}
		}
	}
	return names
}

// Kubernetes exec/portforward paths use pod names. Reject a stale selection
// immediately before opening the connection; never silently follow a replacement.
func (c *Client) checkedPod(ctx context.Context, selected unstructured.Unstructured) (unstructured.Unstructured, error) {
	r, _ := Resolve(Builtins(), "pods")
	pod, err := c.Get(ctx, r, selected.GetNamespace(), selected.GetName())
	if err != nil {
		return pod, err
	}
	if selected.GetKind() != "Pod" || selected.GetUID() == "" || pod.GetUID() != selected.GetUID() {
		return pod, fmt.Errorf("selected pod was replaced; refresh and select it again")
	}
	if Text(pod, "status", "phase") != "Running" || pod.GetDeletionTimestamp() != nil {
		return pod, fmt.Errorf("selected pod is not running")
	}
	return pod, nil
}

func streamFallback(err error) bool {
	return httpstream.IsUpgradeFailure(err) || streamhttp.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err) || streamhttp.IsHTTPSProxyError(err)
}

func (c *Client) Exec(ctx context.Context, selected unstructured.Unstructured, container string, command []string, options remotecommand.StreamOptions) error {
	pod, err := c.checkedPod(ctx, selected)
	if err != nil {
		return err
	}
	if !slices.Contains(RunningContainers(pod), container) {
		return fmt.Errorf("container %q is not running", container)
	}
	if len(command) == 0 {
		return fmt.Errorf("shell command is empty")
	}
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	request := c.streamCore.RESTClient().Post().Resource("pods").Namespace(pod.GetNamespace()).Name(pod.GetName()).SubResource("exec").VersionedParams(&corev1.PodExecOptions{Container: container, Command: command, Stdin: options.Stdin != nil, Stdout: options.Stdout != nil, Stderr: !options.Tty && options.Stderr != nil, TTY: options.Tty}, runtime.NewParameterCodec(scheme))
	cfg := c.streamingConfig(ctx)
	primary, err := remotecommand.NewWebSocketExecutor(cfg, http.MethodGet, request.URL().String())
	if err != nil {
		return err
	}
	secondary, err := remotecommand.NewSPDYExecutor(cfg, http.MethodPost, request.URL())
	if err != nil {
		return err
	}
	executor, err := remotecommand.NewFallbackExecutor(primary, secondary, streamFallback)
	if err != nil {
		return err
	}
	return executor.StreamWithContext(ctx, options)
}

type errorBuffer struct {
	sync.Mutex
	text strings.Builder
}

func (b *errorBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	if b.text.Len() < 8192 {
		b.text.Write(p[:min(len(p), 8192-b.text.Len())])
	}
	return len(p), nil
}
func (b *errorBuffer) String() string { b.Lock(); defer b.Unlock(); return b.text.String() }

// Only expose a loopback listener. All transports retain the current session's
// authentication and TLS configuration, rather than loading CLI defaults again.
func (c *Client) Forward(ctx context.Context, selected unstructured.Unstructured, ports string, ready func(uint16)) error {
	pod, err := c.checkedPod(ctx, selected)
	if err != nil {
		return err
	}
	cfg := c.streamingConfig(ctx)
	url := c.streamCore.RESTClient().Post().Resource("pods").Namespace(pod.GetNamespace()).Name(pod.GetName()).SubResource("portforward").URL()
	transport, upgrade, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return err
	}
	secondary := spdy.NewDialer(upgrade, &http.Client{Transport: transport}, http.MethodPost, url)
	primary, err := portforward.NewSPDYOverWebsocketDialer(url, cfg)
	if err != nil {
		return err
	}
	dialer := portforward.NewFallbackDialer(primary, secondary, streamFallback)
	stop, started := make(chan struct{}), make(chan struct{})
	var errors errorBuffer
	forward, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{ports}, stop, started, io.Discard, &errors)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- forward.ForwardPorts() }()
	select {
	case <-ctx.Done():
		close(stop)
		<-done
		return ctx.Err()
	case err = <-done:
		if err != nil {
			return fmt.Errorf("%w: %s", err, errors.String())
		}
		return fmt.Errorf("port forward ended before becoming ready: %s", errors.String())
	case <-started:
		pairs, getErr := forward.GetPorts()
		if getErr != nil || len(pairs) != 1 {
			close(stop)
			<-done
			return fmt.Errorf("unable to determine local forwarding port: %v", getErr)
		}
		ready(pairs[0].Local)
	}
	select {
	case <-ctx.Done():
		close(stop)
		<-done
		return ctx.Err()
	case err = <-done:
		if err != nil {
			return fmt.Errorf("%w: %s", err, errors.String())
		}
		return nil
	}
}

// Resolve a service's port (including named targetPort) against one ready pod.
// The returned pod is shown in the UI and remains fixed for that forward.
func ForwardTarget(ctx context.Context, b Backend, target unstructured.Unstructured, port int) (unstructured.Unstructured, int, error) {
	if target.GetKind() == "Pod" {
		return target, port, nil
	}
	if target.GetKind() != "Service" {
		return target, 0, fmt.Errorf("select a pod or service to forward")
	}
	if reader, ok := b.(Editable); ok {
		resource, _ := Resolve(Builtins(), "services")
		fresh, err := reader.Get(ctx, resource, target.GetNamespace(), target.GetName())
		if err != nil {
			return target, 0, err
		}
		if fresh.GetUID() != target.GetUID() {
			return target, 0, fmt.Errorf("service was replaced; refresh and select it again")
		}
		target = fresh
	}
	var targetPort interface{}
	for _, value := range Slice(target, "spec", "ports") {
		p, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		if fmt.Sprint(p["port"]) == strconv.Itoa(port) {
			if protocol, _ := p["protocol"].(string); protocol != "" && protocol != "TCP" {
				return target, 0, fmt.Errorf("only TCP ports can be forwarded")
			}
			targetPort = p["targetPort"]
			if targetPort == nil {
				targetPort = p["port"]
			}
			break
		}
	}
	if targetPort == nil {
		return target, 0, fmt.Errorf("service does not expose port %d", port)
	}
	selector, _, ok := PodSelector(target)
	if !ok {
		return target, 0, fmt.Errorf("service has no pod selector")
	}
	resource, _ := Resolve(Builtins(), "pods")
	pods, err := b.List(ctx, Query{Resource: resource, Namespace: target.GetNamespace(), Labels: selector})
	if err != nil {
		return target, 0, err
	}
	slices.SortFunc(pods, func(a, b unstructured.Unstructured) int { return strings.Compare(a.GetName(), b.GetName()) })
	for _, pod := range pods {
		if Text(pod, "status", "phase") != "Running" || pod.GetDeletionTimestamp() != nil || !podReady(pod) {
			continue
		}
		if numeric, err := strconv.Atoi(fmt.Sprint(targetPort)); err == nil && numeric > 0 && numeric <= 65535 {
			return pod, numeric, nil
		}
		for _, value := range Slice(pod, "spec", "containers") {
			container, _ := value.(map[string]interface{})
			ports, _ := container["ports"].([]interface{})
			for _, value := range ports {
				p, _ := value.(map[string]interface{})
				if p["name"] == targetPort && (p["protocol"] == nil || p["protocol"] == "TCP") {
					numeric, err := strconv.Atoi(fmt.Sprint(p["containerPort"]))
					if err == nil && numeric > 0 && numeric <= 65535 {
						return pod, numeric, nil
					}
				}
			}
		}
	}
	return target, 0, fmt.Errorf("no ready pod exposes this service's target port")
}
func podReady(pod unstructured.Unstructured) bool {
	for _, value := range Slice(pod, "status", "conditions") {
		c, _ := value.(map[string]interface{})
		if c["type"] == "Ready" && c["status"] == "True" {
			return true
		}
	}
	return false
}
