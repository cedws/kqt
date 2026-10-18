package kube

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Reports contain API observations, not active probes or scheduler simulations.
// Optional reads fail independently so namespace-only RBAC remains useful.
type DiagnosticSection struct {
	Title string
	Lines []string
}
type DiagnosticReport struct {
	Title    string
	Sections []DiagnosticSection
}

func (r *DiagnosticReport) add(title string, lines ...string) {
	r.Sections = append(r.Sections, DiagnosticSection{title, lines})
}
func (r DiagnosticReport) Text() string {
	var s strings.Builder
	s.WriteString(Safe(r.Title) + "\n")
	for _, section := range r.Sections {
		s.WriteString("\n" + Safe(section.Title) + "\n")
		for _, line := range section.Lines {
			s.WriteString("  " + SafeLines(line) + "\n")
		}
	}
	return s.String()
}
func diagnosticResource(group, version, name, kind string, scoped bool) Resource {
	return Resource{GVR: schema.GroupVersionResource{Group: group, Version: version, Resource: name}, Kind: kind, Namespaced: scoped}
}
func diagnosticTarget(ctx context.Context, b Backend, selected unstructured.Unstructured) (unstructured.Unstructured, error) {
	reader, ok := b.(ResourceGetter)
	if !ok {
		return selected, fmt.Errorf("backend cannot read individual resources")
	}
	r, err := ResourceFor(Builtins(), selected.GetAPIVersion(), selected.GetKind())
	if err != nil {
		return selected, err
	}
	fresh, err := reader.Get(ctx, r, selected.GetNamespace(), selected.GetName())
	if err == nil && (selected.GetUID() == "" || fresh.GetUID() != selected.GetUID()) {
		err = fmt.Errorf("resource was replaced; refresh and select it again")
	}
	return fresh, err
}
