package kube

import (
	"encoding/json"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestManifestExportRoundTripCleaningAndOriginalPreservation(t *testing.T) {
	o := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]interface{}{"name": "settings", "namespace": "apps", "uid": "uid", "resourceVersion": "123", "generation": int64(2), "creationTimestamp": "2026-10-13T12:00:00Z", "annotations": map[string]interface{}{"keep": "yes", "kubectl.kubernetes.io/last-applied-configuration": "large generated value"}}, "data": map[string]interface{}{"multiline": "one\ntwo\t界\x1b[31m"}, "status": map[string]interface{}{"phase": "Ready"}}}
	before := o.DeepCopy()
	for _, format := range []string{"yaml", "json"} {
		for _, clean := range []bool{false, true} {
			content, e := ExportManifest(o, format, clean)
			if e != nil {
				t.Fatal(e)
			}
			var values map[string]interface{}
			if format == "yaml" {
				e = yaml.Unmarshal([]byte(content), &values)
			} else {
				e = json.Unmarshal([]byte(content), &values)
			}
			if e != nil {
				t.Fatal(e)
			}
			if values["data"].(map[string]interface{})["multiline"] != o.Object["data"].(map[string]interface{})["multiline"] {
				t.Fatal("export altered API data")
			}
			metadata := values["metadata"].(map[string]interface{})
			_, uid := metadata["uid"]
			_, status := values["status"]
			if uid == clean || status == clean {
				t.Fatal("generated field cleaning was incorrect")
			}
			if metadata["name"] != "settings" || metadata["namespace"] != "apps" {
				t.Fatal("identity names lost")
			}
		}
	}
	if !reflect.DeepEqual(o.Object, before.Object) {
		t.Fatal("export mutated source")
	}
}
