package kube

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type Relation struct {
	Resource                Resource
	Namespace, Name, Reason string
	UID                     types.UID
}

func (r Relation) Key() string { return r.Resource.ID() + "/" + r.Namespace + "/" + r.Name }
func (r Relation) Title() string {
	return r.Resource.Kind + " " + strings.TrimPrefix(r.Namespace+"/"+r.Name, "/")
}

func ResourceFor(resources []Resource, apiVersion, kind string) (Resource, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return Resource{}, err
	}
	for _, r := range resources {
		if r.Kind == kind && r.GVR.Group == gv.Group {
			return r, nil
		}
	}
	return Resource{}, fmt.Errorf("API unavailable for %s %s", apiVersion, kind)
}

// Relationships use owners, selectors and explicit references, never guessed
// name prefixes. Failed links remain a visible partial-result error.
func Relationships(ctx context.Context, b Backend, resources []Resource, selected unstructured.Unstructured) ([]Relation, error) {
	reader, ok := b.(ResourceGetter)
	if !ok {
		return nil, fmt.Errorf("individual resource reads are unavailable")
	}
	resource, err := ResourceFor(resources, selected.GetAPIVersion(), selected.GetKind())
	if err != nil {
		return nil, err
	}
	fresh, err := reader.Get(ctx, resource, selected.GetNamespace(), selected.GetName())
	if err != nil {
		return nil, err
	}
	if selected.GetUID() == "" || fresh.GetUID() != selected.GetUID() {
		return nil, fmt.Errorf("resource was replaced; refresh and select it again")
	}
	var result []Relation
	var failures []error
	seen := map[string]bool{}
	add := func(r Resource, o unstructured.Unstructured, reason string) {
		link := Relation{Resource: r, Namespace: o.GetNamespace(), Name: o.GetName(), UID: o.GetUID(), Reason: reason}
		if seen[link.Key()] {
			return
		}
		seen[link.Key()] = true
		if len(result) < 2000 {
			result = append(result, link)
		}
	}
	get := func(version, kind, namespace, name, reason string, uid types.UID) *unstructured.Unstructured {
		r, e := ResourceFor(resources, version, kind)
		if e != nil {
			failures = append(failures, e)
			return nil
		}
		if !r.Namespaced {
			namespace = ""
		}
		o, e := reader.Get(ctx, r, namespace, name)
		if e == nil && uid != "" && o.GetUID() != uid {
			e = fmt.Errorf("referenced %s %s was replaced", kind, name)
		}
		if e != nil {
			failures = append(failures, fmt.Errorf("%s %s: %w", kind, name, e))
			return nil
		}
		add(r, o, reason)
		return &o
	}
	var owners func(unstructured.Unstructured, int)
	ownerSeen := map[types.UID]bool{fresh.GetUID(): true}
	owners = func(o unstructured.Unstructured, depth int) {
		if depth >= 8 {
			return
		}
		for _, ref := range o.GetOwnerReferences() {
			if ref.UID == "" || ownerSeen[ref.UID] {
				continue
			}
			ownerSeen[ref.UID] = true
			if owner := get(ref.APIVersion, ref.Kind, o.GetNamespace(), ref.Name, "owner", ref.UID); owner != nil {
				owners(*owner, depth+1)
			}
		}
	}
	owners(fresh, 0)
	list := func(name, ns, selector, fields, reason string, owner types.UID) {
		r, e := Resolve(resources, name)
		if e != nil {
			failures = append(failures, e)
			return
		}
		values, e := b.List(ctx, Query{Resource: r, Namespace: ns, Labels: selector, Fields: fields})
		if e != nil {
			failures = append(failures, fmt.Errorf("%s: %w", name, e))
			return
		}
		for _, o := range values {
			if owner != "" {
				matched := false
				for _, ref := range o.GetOwnerReferences() {
					if ref.UID == owner {
						matched = true
					}
				}
				if !matched {
					continue
				}
			}
			add(r, o, reason)
		}
	}
	if selector, fields, match := PodSelector(fresh); match {
		ns := fresh.GetNamespace()
		if fresh.GetKind() == "Node" {
			ns = ""
		}
		list("pods", ns, selector, fields, "matching pod", "")
	}
	switch fresh.GetKind() {
	case "Deployment":
		list("replicasets", fresh.GetNamespace(), "", "", "owned ReplicaSet", fresh.GetUID())
	case "CronJob":
		list("jobs", fresh.GetNamespace(), "", "", "owned Job", fresh.GetUID())
	case "Pod":
		if node := Text(fresh, "spec", "nodeName"); node != "" {
			get("v1", "Node", "", node, "scheduled node", "")
		}
		for _, v := range Slice(fresh, "spec", "volumes") {
			volume, _ := v.(map[string]interface{})
			claim, _ := volume["persistentVolumeClaim"].(map[string]interface{})
			if name, _ := claim["claimName"].(string); name != "" {
				get("v1", "PersistentVolumeClaim", fresh.GetNamespace(), name, "mounted claim", "")
			}
		}
	case "PersistentVolumeClaim":
		if name := Text(fresh, "spec", "volumeName"); name != "" {
			get("v1", "PersistentVolume", "", name, "bound volume", "")
		}
	case "PersistentVolume":
		if name := Text(fresh, "spec", "claimRef", "name"); name != "" {
			get("v1", "PersistentVolumeClaim", Text(fresh, "spec", "claimRef", "namespace"), name, "bound claim", types.UID(Text(fresh, "spec", "claimRef", "uid")))
		}
	case "Ingress":
		names := map[string]bool{}
		if name := Text(fresh, "spec", "defaultBackend", "service", "name"); name != "" {
			names[name] = true
		}
		for _, v := range Slice(fresh, "spec", "rules") {
			rule, _ := v.(map[string]interface{})
			http, _ := rule["http"].(map[string]interface{})
			paths, _ := http["paths"].([]interface{})
			for _, path := range paths {
				p, _ := path.(map[string]interface{})
				backend, _ := p["backend"].(map[string]interface{})
				service, _ := backend["service"].(map[string]interface{})
				if name, _ := service["name"].(string); name != "" {
					names[name] = true
				}
			}
		}
		for name := range names {
			get("v1", "Service", fresh.GetNamespace(), name, "ingress backend", "")
		}
	}
	if len(result) == 2000 {
		failures = append(failures, fmt.Errorf("showing at most 2,000 related resources"))
	}
	slices.SortFunc(result, func(a, b Relation) int { return strings.Compare(a.Title(), b.Title()) })
	return result, errors.Join(failures...)
}
