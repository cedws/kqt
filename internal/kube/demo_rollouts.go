package kube

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"time"
)

func demoRollouts(d *Demo) {
	var older []unstructured.Unstructured
	for i := range d.objects {
		rs := &d.objects[i]
		if rs.GetKind() != "ReplicaSet" {
			continue
		}
		for _, deployment := range d.objects {
			if deployment.GetKind() != "Deployment" || deployment.GetNamespace() != rs.GetNamespace() || deployment.GetName() != rs.GetOwnerReferences()[0].Name {
				continue
			}
			template, _ := deploymentTemplate(deployment)
			_ = unstructured.SetNestedMap(rs.Object, template, "spec", "template")
			rs.SetLabels(map[string]string{"app": deployment.GetName()})
			controller := true
			refs := rs.GetOwnerReferences()
			refs[0].Controller = &controller
			rs.SetOwnerReferences(refs)
			rs.SetAnnotations(map[string]string{revisionAnnotation: "2", "kubernetes.io/change-cause": "Upgrade nginx to 1.28"})
			rs.SetCreationTimestamp(metav1.NewTime(time.Now().Add(-time.Hour)))
			previous := *rs.DeepCopy()
			previous.SetName(deployment.GetName() + "-previous")
			previous.SetUID(types.UID(deployment.GetNamespace() + "/" + previous.GetName()))
			previous.SetAnnotations(map[string]string{revisionAnnotation: "1", "kubernetes.io/change-cause": "Initial deployment"})
			previous.SetCreationTimestamp(metav1.NewTime(time.Now().Add(-27 * time.Hour)))
			containers := Slice(previous, "spec", "template", "spec", "containers")
			containers[0].(map[string]interface{})["image"] = "nginx:1.27"
			_ = unstructured.SetNestedSlice(previous.Object, containers, "spec", "template", "spec", "containers")
			_ = unstructured.SetNestedField(previous.Object, int64(0), "spec", "replicas")
			_ = unstructured.SetNestedField(previous.Object, int64(0), "status", "readyReplicas")
			older = append(older, previous)
			break
		}
	}
	d.objects = append(d.objects, older...)
}
