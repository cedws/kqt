package kube

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// SafeLogLines removes terminal escape sequences as a whole, so container
// colour codes do not become visible fragments after escaping control bytes.
func SafeLogLines(s string) string {
	return SafeLines(ansi.Strip(s))
}

// LogSnapshot reads bounded snapshots with four concurrent requests. A workload
// selector stays in its namespace; errors remain visible alongside healthy pods.
func LogSnapshot(ctx context.Context, b Backend, target unstructured.Unstructured, container string, previous bool) (string, error) {
	if target.GetKind() == "Pod" && container != "*" {
		return b.Logs(ctx, target.GetNamespace(), target.GetName(), container, previous)
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
	type source struct{ ns, pod, container string }
	var sources []source
	for _, pod := range pods {
		containers := []string{container}
		if container == "*" {
			containers = Containers(pod)
		}
		for _, name := range containers {
			sources = append(sources, source{pod.GetNamespace(), pod.GetName(), name})
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
				prefix := fmt.Sprintf("[%s/%s:%s]", s.ns, s.pod, s.container)
				data, err := b.Logs(ctx, s.ns, s.pod, s.container, previous)
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
