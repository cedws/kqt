package kube

import (
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type Resource struct {
	GVR        schema.GroupVersionResource
	Kind       string
	Namespaced bool
	Aliases    []string
}

func (r Resource) ID() string {
	if r.GVR.Group == "" {
		return r.GVR.Resource
	}
	return r.GVR.Resource + "." + r.GVR.Group
}

func Builtins() []Resource {
	type entry struct {
		group, version, name, kind string
		scoped                     bool
		aliases                    []string
	}
	entries := []entry{
		{"", "v1", "pods", "Pod", true, []string{"po", "pod"}},
		{"", "v1", "services", "Service", true, []string{"svc", "service"}},
		{"", "v1", "nodes", "Node", false, []string{"no", "node"}},
		{"", "v1", "namespaces", "Namespace", false, []string{"ns", "namespace"}},
		{"", "v1", "events", "Event", true, []string{"ev", "event"}},
		{"", "v1", "configmaps", "ConfigMap", true, []string{"cm", "configmap"}},
		{"", "v1", "secrets", "Secret", true, []string{"secret"}},
		{"", "v1", "persistentvolumeclaims", "PersistentVolumeClaim", true, []string{"pvc"}},
		{"", "v1", "persistentvolumes", "PersistentVolume", false, []string{"pv"}},
		{"", "v1", "serviceaccounts", "ServiceAccount", true, []string{"sa"}},
		{"apps", "v1", "deployments", "Deployment", true, []string{"deploy", "deployment"}},
		{"apps", "v1", "statefulsets", "StatefulSet", true, []string{"sts", "statefulset"}},
		{"apps", "v1", "daemonsets", "DaemonSet", true, []string{"ds", "daemonset"}},
		{"apps", "v1", "replicasets", "ReplicaSet", true, []string{"rs", "replicaset"}},
		{"batch", "v1", "jobs", "Job", true, []string{"job"}},
		{"batch", "v1", "cronjobs", "CronJob", true, []string{"cj", "cronjob"}},
		{"networking.k8s.io", "v1", "ingresses", "Ingress", true, []string{"ing", "ingress"}},
		{"networking.k8s.io", "v1", "networkpolicies", "NetworkPolicy", true, []string{"netpol"}},
		{"rbac.authorization.k8s.io", "v1", "roles", "Role", true, []string{"role"}},
		{"rbac.authorization.k8s.io", "v1", "rolebindings", "RoleBinding", true, []string{"rolebinding"}},
		{"rbac.authorization.k8s.io", "v1", "clusterroles", "ClusterRole", false, []string{"clusterrole"}},
		{"rbac.authorization.k8s.io", "v1", "clusterrolebindings", "ClusterRoleBinding", false, []string{"clusterrolebinding"}},
		{"apiextensions.k8s.io", "v1", "customresourcedefinitions", "CustomResourceDefinition", false, []string{"crd", "crds"}},
	}
	result := make([]Resource, 0, len(entries))
	for _, e := range entries {
		result = append(result, Resource{schema.GroupVersionResource{Group: e.group, Version: e.version, Resource: e.name}, e.kind, e.scoped, e.aliases})
	}
	return result
}

func Resolve(resources []Resource, name string) (Resource, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	// Fully qualified IDs always win over aliases.
	for _, r := range resources {
		if strings.ToLower(r.ID()) == name {
			return r, nil
		}
	}
	var matches []Resource
	for _, r := range resources {
		if strings.ToLower(r.GVR.Resource) == name || strings.ToLower(r.Kind) == name || slices.Contains(r.Aliases, name) {
			matches = append(matches, r)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		ids := make([]string, len(matches))
		for i, r := range matches {
			ids[i] = r.ID()
		}
		return Resource{}, fmt.Errorf("ambiguous resource %q; use %s", name, strings.Join(ids, " or "))
	}
	return Resource{}, fmt.Errorf("unknown resource %q; use :resources to browse APIs", name)
}

// Merge discovery over built-ins while retaining familiar command aliases.
func Merge(discovered []Resource) []Resource {
	result := Builtins()
	for _, r := range discovered {
		found := false
		for i, old := range result {
			if old.ID() == r.ID() {
				r.Aliases = append(r.Aliases, old.Aliases...)
				result[i] = r
				found = true
				break
			}
		}
		if !found {
			result = append(result, r)
		}
	}
	slices.SortFunc(result, func(a, b Resource) int { return strings.Compare(a.ID(), b.ID()) })
	return result
}
