package kube

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// Bundles deliberately omit Secret resources, even when the inspector has
// revealed one. Optional logs are opt-in because log messages can contain secrets.
type BundleOptions struct {
	IncludeLogs bool
	Context     string
}
type DiagnosticBundle struct {
	Files    map[string][]byte
	Warnings []string
}
type bundleBackend struct {
	Backend
	reader    ResourceGetter
	limitPods bool
}

func (b bundleBackend) Get(ctx context.Context, r Resource, ns, name string) (unstructured.Unstructured, error) {
	if r.Kind == "Secret" || r.GVR.Resource == "secrets" {
		return unstructured.Unstructured{}, fmt.Errorf("Secret resources are excluded from diagnostic bundles")
	}
	return b.reader.Get(ctx, r, ns, name)
}
func (b bundleBackend) List(ctx context.Context, q Query) ([]unstructured.Unstructured, error) {
	if q.Resource.Kind == "Secret" || q.Resource.GVR.Resource == "secrets" {
		return nil, fmt.Errorf("Secret resources are excluded from diagnostic bundles")
	}
	objects, err := b.Backend.List(ctx, q)
	if b.limitPods && q.Resource.Kind == "Pod" && len(objects) > 20 {
		objects = objects[:20]
	}
	return objects, err
}
func CollectBundle(ctx context.Context, b Backend, resources []Resource, selected unstructured.Unstructured, options BundleOptions) (DiagnosticBundle, error) {
	bundle := DiagnosticBundle{Files: map[string][]byte{}}
	if selected.GetKind() == "Secret" {
		return bundle, fmt.Errorf("Secret resources are excluded from diagnostic bundles; select another resource")
	}
	reader, ok := b.(ResourceGetter)
	if !ok {
		return bundle, fmt.Errorf("individual resource reads are unavailable")
	}
	safeBackend := bundleBackend{Backend: b, reader: reader}
	r, err := ResourceFor(resources, selected.GetAPIVersion(), selected.GetKind())
	if err != nil {
		return bundle, err
	}
	o, err := reader.Get(ctx, r, selected.GetNamespace(), selected.GetName())
	if err != nil {
		return bundle, err
	}
	if selected.GetUID() == "" || o.GetUID() != selected.GetUID() {
		return bundle, fmt.Errorf("resource was replaced; refresh and select it again")
	}
	put := func(name string, value any) {
		data, e := yaml.Marshal(value)
		if e != nil {
			bundle.Warnings = append(bundle.Warnings, name+": "+e.Error())
			return
		}
		if len(data) > 4*1024*1024 {
			bundle.Warnings = append(bundle.Warnings, name+": omitted (larger than 4 MiB)")
			return
		}
		bundle.Files[name] = data
	}
	redacted := bundleResource(o)
	put("resource.yaml", redacted.Object)
	put("status.yaml", redacted.Object["status"])
	events, e := EventsForResource(ctx, safeBackend, o)
	if e != nil {
		bundle.Warnings = append(bundle.Warnings, "Events unavailable: "+e.Error())
	} else {
		var entries []map[string]any
		if len(events) > 2000 {
			events = events[:2000]
			bundle.Warnings = append(bundle.Warnings, "Only the latest 2,000 events are included.")
		}
		for _, event := range events {
			entries = append(entries, bundleResource(event).Object)
		}
		put("events.yaml", entries)
	}
	links, e := Relationships(ctx, safeBackend, resources, o)
	if e != nil {
		bundle.Warnings = append(bundle.Warnings, "Related resource summary may be incomplete: "+e.Error())
	}
	var related strings.Builder
	for i, link := range links {
		if i >= 200 {
			bundle.Warnings = append(bundle.Warnings, "Only the first 200 related resource summaries are included.")
			break
		}
		if link.Resource.Kind == "Secret" {
			continue
		}
		fmt.Fprintf(&related, "%s · %s · UID %s\n", Safe(link.Title()), Safe(link.Reason), Safe(string(link.UID)))
	}
	if related.Len() == 0 {
		related.WriteString("No related resources available.\n")
	}
	bundle.Files["related.txt"] = []byte(related.String())
	var report DiagnosticReport
	e = nil
	switch o.GetKind() {
	case "Service":
		report, e = ServiceDiagnostics(ctx, safeBackend, o)
	case "Pod":
		report, e = SchedulingDiagnostics(ctx, safeBackend, o)
	}
	if e != nil {
		bundle.Warnings = append(bundle.Warnings, "Diagnostics unavailable: "+e.Error())
	}
	if report.Title != "" {
		bundle.Files["diagnostics.txt"] = []byte(report.Text())
	}
	if options.IncludeLogs {
		switch o.GetKind() {
		case "Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job":
			safeBackend.limitPods = true
			tail := int64(200)
			logs, e := LogSnapshot(ctx, safeBackend, o, "*", false, LogOptions{TailLines: &tail})
			if e != nil {
				bundle.Warnings = append(bundle.Warnings, "Logs unavailable: "+e.Error())
			} else {
				if len(logs) > 4*1024*1024 {
					logs = logs[:4*1024*1024] + "\n[kqt] Logs truncated at 4 MiB.\n"
				}
				bundle.Files["logs.txt"] = []byte(SafeLogLines(logs))
			}
			bundle.Warnings = append(bundle.Warnings, "Logs are limited to 20 pods, 200 lines per container and 4 MiB total; log text is not redacted.")
		default:
			bundle.Warnings = append(bundle.Warnings, "This resource does not support log snapshots.")
		}
	}
	// Reject a capture spanning replacement of the selected object. A bundle is a
	// best-effort collection, not an atomic cluster-wide snapshot.
	fresh, e := reader.Get(ctx, r, o.GetNamespace(), o.GetName())
	if e != nil {
		return bundle, e
	}
	if fresh.GetUID() != o.GetUID() {
		return bundle, fmt.Errorf("resource was replaced during capture; no bundle was saved")
	}
	if err := ctx.Err(); err != nil {
		return bundle, err
	}
	readme := fmt.Sprintf("kqt diagnostic bundle\nCaptured: %s\nResource: %s %s/%s\nUID: %s\n\nBest-effort observations, not an atomic cluster snapshot.\nSecret resources are excluded. Secret values, literal environment values,\nConfigMap data and last-applied annotations are omitted/redacted.\nOther annotations, arguments, status, events and optional logs may still contain\nsensitive application data. Review the archive before sharing it.\nRelated resources are summaries only; their manifests are not exported.\nLogs included: %t (opt-in)\n", time.Now().UTC().Format(time.RFC3339), Safe(o.GetKind()), Safe(o.GetNamespace()), Safe(o.GetName()), Safe(string(o.GetUID())), options.IncludeLogs)
	readme += "Context: " + Safe(options.Context) + "\n"
	if len(bundle.Warnings) > 0 {
		readme += "\nCapture notes / unavailable data:\n"
		for _, warning := range bundle.Warnings {
			readme += "- " + SafeLines(warning) + "\n"
		}
	}
	bundle.Files["README.txt"] = []byte(readme)
	return bundle, nil
}
func bundleResource(o unstructured.Unstructured) unstructured.Unstructured {
	o = Redacted(o)
	annotations := o.GetAnnotations()
	delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
	o.SetAnnotations(annotations)
	if o.GetKind() == "ConfigMap" {
		for _, field := range []string{"data", "binaryData"} {
			values, ok, _ := unstructured.NestedMap(o.Object, field)
			if ok {
				for key := range values {
					values[key] = "<redacted>"
				}
				o.Object[field] = values
			}
		}
	}
	var redactEnv func(any)
	redactEnv = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				if key == "env" {
					if entries, ok := child.([]any); ok {
						for _, entry := range entries {
							if env, ok := entry.(map[string]any); ok {
								if _, present := env["value"]; present {
									env["value"] = "<redacted>"
								}
							}
						}
					}
				} else {
					redactEnv(child)
				}
			}
		case []any:
			for _, child := range v {
				redactEnv(child)
			}
		}
	}
	redactEnv(o.Object)
	return o
}
func (b DiagnosticBundle) Archive() ([]byte, error) {
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(b.Files))
	for name := range b.Files {
		names = append(names, name)
	}
	slices.Sort(names)
	total := 0
	for _, name := range names {
		if name == "" || strings.ContainsAny(name, "/\\") || name == ".." {
			return nil, fmt.Errorf("invalid archive entry")
		}
		data := b.Files[name]
		total += len(data)
		if total > 16*1024*1024 {
			return nil, fmt.Errorf("diagnostic bundle exceeds 16 MiB")
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
