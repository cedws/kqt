package kube

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/remotecommand"
)

func (d *Demo) PatchEphemeral(ctx context.Context, pod unstructured.Unstructured, container corev1.EphemeralContainer, dryRun bool) (unstructured.Unstructured, error) {
	if err := ctx.Err(); err != nil {
		return unstructured.Unstructured{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.objects {
		current := d.objects[i]
		if current.GetKind() != "Pod" || current.GetNamespace() != pod.GetNamespace() || current.GetName() != pod.GetName() {
			continue
		}
		if current.GetUID() != pod.GetUID() || current.GetResourceVersion() != pod.GetResourceVersion() {
			return current, fmt.Errorf("pod changed; refresh and try again")
		}
		copy := *current.DeepCopy()
		entry, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&container)
		if err != nil {
			return copy, err
		}
		_ = unstructured.SetNestedSlice(copy.Object, append(Slice(copy, "spec", "ephemeralContainers"), entry), "spec", "ephemeralContainers")
		if !dryRun {
			rv, _ := strconv.Atoi(copy.GetResourceVersion())
			copy.SetResourceVersion(strconv.Itoa(rv + 1))
			_ = unstructured.SetNestedSlice(copy.Object, append(Slice(copy, "status", "ephemeralContainerStatuses"), map[string]interface{}{"name": container.Name, "state": map[string]interface{}{"running": map[string]interface{}{"startedAt": time.Now().UTC().Format(time.RFC3339)}}}), "status", "ephemeralContainerStatuses")
			d.objects[i] = copy
			d.notifyResourcesLocked()
		}
		return *copy.DeepCopy(), nil
	}
	return unstructured.Unstructured{}, fmt.Errorf("pod not found")
}

func (d *Demo) Attach(ctx context.Context, selected unstructured.Unstructured, container string, _ bool, options remotecommand.StreamOptions) error {
	pod, err := debugPod(ctx, d, selected)
	if err != nil {
		return err
	}
	if !sameRunningDebugger(pod, container) {
		return fmt.Errorf("ephemeral container %q is not running", container)
	}
	fmt.Fprint(options.Stdout, "Demo debug terminal (simulated; no commands run on your computer).\r\nType exit to stop, or Ctrl+] to disconnect and reattach.\r\n$ ")
	scanner := bufio.NewScanner(demoDebugInput{options.Stdin})
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(scanner.Text())
		fmt.Fprint(options.Stdout, "\r\n")
		if line == "exit" {
			d.mu.Lock()
			for i := range d.objects {
				if d.objects[i].GetUID() != selected.GetUID() {
					continue
				}
				statuses := Slice(d.objects[i], "status", "ephemeralContainerStatuses")
				for _, value := range statuses {
					if status, ok := value.(map[string]interface{}); ok && status["name"] == container {
						status["state"] = map[string]interface{}{"terminated": map[string]interface{}{"reason": "Completed", "exitCode": int64(0)}}
					}
				}
				_ = unstructured.SetNestedSlice(d.objects[i].Object, statuses, "status", "ephemeralContainerStatuses")
			}
			d.notifyResourcesLocked()
			d.mu.Unlock()
			return nil
		}
		fmt.Fprintf(options.Stdout, "demo: %s\r\n$ ", Safe(line))
	}
	return scanner.Err()
}

type demoDebugInput struct{ io.Reader }

func (r demoDebugInput) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	for i := range p[:n] {
		if p[i] == '\r' {
			p[i] = '\n'
		}
	}
	return n, err
}

func sameRunningDebugger(pod unstructured.Unstructured, name string) bool {
	for _, container := range DebugContainers(pod) {
		if container.Name == name {
			for _, running := range RunningContainers(pod) {
				if running == name {
					return true
				}
			}
		}
	}
	return false
}
