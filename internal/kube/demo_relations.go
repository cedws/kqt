package kube

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func demoRelationships(d *Demo) {
	for _, ns := range []string{"default", "platform"} {
		name := "web"
		if ns == "platform" {
			name = "gateway"
		}
		rs := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "apps/v1", "kind": "ReplicaSet", "metadata": map[string]interface{}{"name": name + "-7c9f6d8b5", "namespace": ns, "uid": ns + "/" + name + "-7c9f6d8b5"}, "spec": map[string]interface{}{"replicas": int64(12), "selector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": name}}}, "status": map[string]interface{}{"readyReplicas": int64(12)}}}
		rs.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: name, UID: types.UID(ns + "/" + name)}})
		for i := range d.objects {
			o := &d.objects[i]
			if o.GetKind() == "Pod" && o.GetNamespace() == ns && o.GetLabels()["app"] == name {
				o.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: rs.GetName(), UID: rs.GetUID()}})
			}
		}
		d.objects = append(d.objects, rs)
	}
	claim := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]interface{}{"name": "web-data", "namespace": "default", "uid": "default/web-data"}, "spec": map[string]interface{}{"volumeName": "web-data"}, "status": map[string]interface{}{"phase": "Bound"}}}
	volume := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "v1", "kind": "PersistentVolume", "metadata": map[string]interface{}{"name": "web-data", "uid": "pv/web-data"}, "spec": map[string]interface{}{"claimRef": map[string]interface{}{"name": "web-data", "namespace": "default", "uid": "default/web-data"}}, "status": map[string]interface{}{"phase": "Bound"}}}
	ingress := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "networking.k8s.io/v1", "kind": "Ingress", "metadata": map[string]interface{}{"name": "web", "namespace": "default", "uid": "ingress/web"}, "spec": map[string]interface{}{"defaultBackend": map[string]interface{}{"service": map[string]interface{}{"name": "web", "port": map[string]interface{}{"number": int64(80)}}}}}}
	d.objects = append(d.objects, claim, volume, ingress)
}
