package kube

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// ExportManifest encodes API values without terminal sanitization or mutation.
// The caller chooses redaction before passing the object. Clean removes only
// generated metadata and status, preserving user-specified resource fields.
func ExportManifest(object unstructured.Unstructured, format string, clean bool) (string, error) {
	o := object.DeepCopy()
	o.SetManagedFields(nil)
	if clean {
		delete(o.Object, "status")
		for _, key := range []string{"uid", "resourceVersion", "generation", "creationTimestamp", "deletionTimestamp", "deletionGracePeriodSeconds", "selfLink", "ownerReferences"} {
			unstructured.RemoveNestedField(o.Object, "metadata", key)
		}
		annotations := o.GetAnnotations()
		delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
		o.SetAnnotations(annotations)
	}
	var data []byte
	var err error
	switch format {
	case "yaml":
		data, err = yaml.Marshal(o.Object)
	case "json":
		data, err = json.MarshalIndent(o.Object, "", "  ")
	default:
		return "", fmt.Errorf("choose yaml or json")
	}
	if err != nil {
		return "", err
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	return string(data), nil
}
