package kube

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

func demoActions(d *Demo) {
	d.objects = append(d.objects, unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "batch/v1", "kind": "CronJob", "metadata": map[string]interface{}{"name": "nightly-report", "namespace": "default", "uid": "default/nightly-report", "resourceVersion": "1"},
		"spec": map[string]interface{}{"schedule": "0 0 * * *", "suspend": false, "jobTemplate": map[string]interface{}{"spec": map[string]interface{}{"template": map[string]interface{}{"spec": map[string]interface{}{"restartPolicy": "Never", "containers": []interface{}{map[string]interface{}{"name": "report", "image": "busybox:1.37", "command": []interface{}{"sh", "-c", "echo report"}}}}}}}}}})
	for i := range d.objects {
		if d.objects[i].GetResourceVersion() == "" {
			d.objects[i].SetResourceVersion("1")
		}
	}
}
