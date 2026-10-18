package kube

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

// EndpointSlices make Service troubleshooting useful without cluster access.
func demoDiagnostics(d *Demo) {
	var slices []unstructured.Unstructured
	for _, service := range d.objects {
		if service.GetKind() != "Service" {
			continue
		}
		selector, _, _ := unstructured.NestedStringMap(service.Object, "spec", "selector")
		if len(selector) == 0 {
			continue
		}
		var endpoints []any
		for _, pod := range d.objects {
			if pod.GetKind() != "Pod" || pod.GetNamespace() != service.GetNamespace() {
				continue
			}
			matches := true
			for key, value := range selector {
				if pod.GetLabels()[key] != value {
					matches = false
				}
			}
			if !matches {
				continue
			}
			ip := Text(pod, "status", "podIP")
			if ip == "" {
				continue
			}
			ready := false
			for _, condition := range Slice(pod, "status", "conditions") {
				c, _ := condition.(map[string]any)
				if c["type"] == "Ready" && c["status"] == "True" {
					ready = true
				}
			}
			endpoints = append(endpoints, map[string]any{"addresses": []any{ip}, "conditions": map[string]any{"ready": ready}, "targetRef": map[string]any{"kind": "Pod", "namespace": pod.GetNamespace(), "name": pod.GetName(), "uid": string(pod.GetUID())}})
		}
		slices = append(slices, unstructured.Unstructured{Object: map[string]any{"apiVersion": "discovery.k8s.io/v1", "kind": "EndpointSlice", "metadata": map[string]any{"name": service.GetName() + "-demo", "namespace": service.GetNamespace(), "uid": string(service.GetUID()) + "/endpoints", "labels": map[string]any{"kubernetes.io/service-name": service.GetName()}}, "addressType": "IPv4", "ports": []any{map[string]any{"name": "", "port": int64(80), "protocol": "TCP"}}, "endpoints": endpoints}})
	}
	d.objects = append(d.objects, slices...)
}
