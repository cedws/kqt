package kube

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// LogOptions limits the initial history per container. Timestamps are always
// collected for reliable reconnection; the view can hide them independently.
type LogOptions struct {
	SinceTime *time.Time
	TailLines *int64
}

func logRequest(defaultTail int64, limits []LogOptions) *corev1.PodLogOptions {
	options := &corev1.PodLogOptions{Timestamps: true, TailLines: &defaultTail}
	if len(limits) > 0 {
		if limits[0].SinceTime != nil {
			options.SinceTime = &metav1.Time{Time: *limits[0].SinceTime}
		}
		if limits[0].TailLines != nil {
			tail := *limits[0].TailLines
			options.TailLines = &tail
		}
	}
	return options
}

// SafeLogLines removes terminal escape sequences as a whole, so container
// colour codes do not become visible fragments after escaping control bytes.
func SafeLogLines(s string) string {
	return SafeLines(ansi.Strip(s))
}

// LogSnapshot reads bounded snapshots with four concurrent requests. A workload
// selector stays in its namespace; errors remain visible alongside healthy pods.
func LogSnapshot(ctx context.Context, b Backend, target unstructured.Unstructured, container string, previous bool, options ...LogOptions) (string, error) {
	reader, ok := b.(ResourceGetter)
	if !ok {
		return "", fmt.Errorf("individual resource reads are unavailable; cannot verify log target identity")
	}
	var err error
	target, err = checkedLogResource(ctx, reader, target)
	if err != nil {
		return "", err
	}
	if target.GetKind() == "Pod" && container != "*" {
		return b.Logs(ctx, target.GetNamespace(), target.GetName(), container, previous, options...)
	}
	pods := []unstructured.Unstructured{target}
	if target.GetKind() != "Pod" {
		selector, _, ok := PodSelector(target)
		if !ok {
			return "", fmt.Errorf("workload has no usable pod selector")
		}
		pod, _ := Resolve(Builtins(), "pods")
		var err error
		pods, err = b.List(ctx, Query{Resource: pod, Namespace: target.GetNamespace(), Labels: selector})
		if err != nil {
			return "", err
		}
		if len(pods) == 0 {
			return "No pods match this workload's selector.", nil
		}
	}
	slices.SortFunc(pods, func(a, b unstructured.Unstructured) int { return strings.Compare(a.GetName(), b.GetName()) })
	type source struct {
		pod       unstructured.Unstructured
		container string
	}
	var sources []source
	for _, pod := range pods {
		containers := []string{container}
		if container == "*" {
			containers = Containers(pod)
		}
		for _, name := range containers {
			sources = append(sources, source{pod, name})
		}
	}
	if len(sources) == 0 {
		return "No containers found.", nil
	}
	const limit = 4 * 1024 * 1024
	quota := max(128, limit/len(sources)-160)
	results := make([]string, len(sources))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for i := 0; i < min(4, len(sources)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				s := sources[index]
				prefix := fmt.Sprintf("[%s/%s:%s]", s.pod.GetNamespace(), s.pod.GetName(), s.container)
				_, err := checkedLogResource(ctx, reader, s.pod)
				var data string
				if err == nil {
					data, err = b.Logs(ctx, s.pod.GetNamespace(), s.pod.GetName(), s.container, previous, options...)
				}
				if err != nil {
					results[index] = prefix + " Unable to read logs: " + err.Error() + "\n"
					continue
				}
				if data == "" {
					data = "No log lines returned.\n"
				}
				if len(data) > quota {
					data = data[:quota] + "\n… source log truncated\n"
				}
				var out strings.Builder
				for _, line := range strings.Split(strings.TrimSuffix(data, "\n"), "\n") {
					out.WriteString(prefix + " " + line + "\n")
				}
				results[index] = out.String()
			}
		}()
	}
dispatch:
	for i := range sources {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	workers.Wait()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	var out strings.Builder
	for _, data := range results {
		remaining := limit - out.Len()
		if len(data) > remaining {
			out.WriteString(data[:max(0, remaining)])
			out.WriteString("\n… combined log limit reached (4 MiB)\n")
			break
		}
		out.WriteString(data)
	}
	return out.String(), nil
}

// Log subresources are addressed by name. Check the captured UID immediately
// before reading; completed pods remain valid targets for previous snapshots.
func checkedLogResource(ctx context.Context, reader ResourceGetter, selected unstructured.Unstructured) (unstructured.Unstructured, error) {
	if selected.GetUID() == "" {
		return selected, fmt.Errorf("selected resource has no UID; refresh before reading logs")
	}
	r, err := Resolve(Builtins(), strings.ToLower(selected.GetKind()))
	if err != nil {
		return selected, err
	}
	fresh, err := reader.Get(ctx, r, selected.GetNamespace(), selected.GetName())
	if err == nil && fresh.GetUID() != selected.GetUID() {
		err = fmt.Errorf("selected %s was replaced; close logs and select it again", selected.GetKind())
	}
	return fresh, err
}
