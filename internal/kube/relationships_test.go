package kube

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRelationshipsOwnersSelectorsIngressAndStorage(t *testing.T) {
	d := NewDemo()
	resources, _ := d.Resources(context.Background())
	for _, tc := range []struct {
		kind, ns, name string
		wants          []string
	}{
		{"Pod", "default", "web-7c9f6d8b5-0003", []string{"ReplicaSet default/web-7c9f6d8b5", "Deployment default/web", "Node worker-1"}},
		{"Service", "default", "web", []string{"Pod default/web-7c9f6d8b5-0003"}},
		{"Ingress", "default", "web", []string{"Service default/web"}},
		{"PersistentVolumeClaim", "default", "web-data", []string{"PersistentVolume web-data"}},
		{"PersistentVolume", "", "web-data", []string{"PersistentVolumeClaim default/web-data"}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			r, e := ResourceFor(resources, "v1", tc.kind)
			if tc.kind == "Ingress" {
				r, e = ResourceFor(resources, "networking.k8s.io/v1", tc.kind)
			}
			if e != nil {
				t.Fatal(e)
			}
			o, e := d.Get(context.Background(), r, tc.ns, tc.name)
			if e != nil {
				t.Fatal(e)
			}
			links, e := Relationships(context.Background(), d, resources, o)
			if e != nil {
				t.Fatal(e)
			}
			for _, want := range tc.wants {
				found := false
				for _, link := range links {
					if link.Title() == want {
						found = true
					}
					if link.Namespace == "platform" {
						t.Fatal("relationship crossed namespace")
					}
				}
				if !found {
					t.Fatalf("missing %s: %+v", want, links)
				}
			}
		})
	}
}
func TestRelationshipsRejectReplacedOwnerAndSelectedResource(t *testing.T) {
	d := NewDemo()
	resources, _ := d.Resources(context.Background())
	r, _ := Resolve(resources, "pods")
	pod, _ := d.Get(context.Background(), r, "default", "web-7c9f6d8b5-0003")
	pod.SetUID("replaced")
	if _, e := Relationships(context.Background(), d, resources, pod); e == nil || !strings.Contains(e.Error(), "replaced") {
		t.Fatal("stale selection accepted")
	}
	pod, _ = d.Get(context.Background(), r, "default", "web-7c9f6d8b5-0003")
	for i := range d.objects {
		if d.objects[i].GetKind() == "ReplicaSet" && d.objects[i].GetNamespace() == "default" {
			d.objects[i].SetUID("new-owner")
		}
	}
	links, e := Relationships(context.Background(), d, resources, pod)
	if e == nil || !strings.Contains(e.Error(), "replaced") {
		t.Fatal("stale owner hidden")
	}
	for _, link := range links {
		if link.Resource.Kind == "ReplicaSet" || link.Resource.Kind == "Deployment" {
			t.Fatal("followed replacement owner")
		}
	}
	if len(links) != 1 || links[0].Resource.Kind != "Node" {
		t.Fatal("partial results discarded")
	}
}
func TestRelationshipsOwnerCyclesAndIngressDuplicateServices(t *testing.T) {
	d := NewDemo()
	resources, _ := d.Resources(context.Background())
	r, _ := Resolve(resources, "deployments")
	deployment, _ := d.Get(context.Background(), r, "default", "web")
	for i := range d.objects {
		if d.objects[i].GetKind() == "Deployment" && d.objects[i].GetName() == "web" {
			d.objects[i].SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "web", UID: deployment.GetUID()}})
		}
	}
	links, e := Relationships(context.Background(), d, resources, deployment)
	if e != nil {
		t.Fatal(e)
	}
	if len(links) != 14 {
		t.Fatalf("owner cycle or duplicate resources: %d", len(links))
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Relationships(cancelled, d, resources, unstructured.Unstructured{}); e == nil {
		t.Fatal("cancellation ignored")
	}
}
