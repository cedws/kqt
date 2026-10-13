package kube

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestDecodedSecretManifestIsDisplayOnly(t *testing.T) {
	encode := func(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }
	secret := unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]interface{}{"name": "example", "annotations": map[string]interface{}{"kubectl.kubernetes.io/last-applied-configuration": "duplicate-secret", "other": "keep"}, "managedFields": []interface{}{map[string]interface{}{"manager": "fixture"}}},
		"data": map[string]interface{}{
			"password": encode("demo-only"), "multiline": encode("first line\n東京\nlast line\n"),
			"quoted": encode(`literal "quotes": true, 18446744073709551615`), "empty": "",
			"binary": encode(string([]byte{255, 254, 0})), "invalid": "not base64!",
			"control": encode("\x1b]52;c;hidden\a"),
		},
		"stringData": map[string]interface{}{"plain": "readable"},
	}}
	before := secret.DeepCopy()
	for _, asJSON := range []bool{false, true} {
		text := DecodedSecretManifest(secret, asJSON)
		if !asJSON {
			data, err := yaml.YAMLToJSON([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			text = string(data)
		}
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(text), &result); err != nil {
			t.Fatal(err)
		}
		values := result["data"].(map[string]interface{})
		for key, want := range map[string]string{"password": "demo-only", "multiline": "first line\n東京\nlast line\n", "quoted": `literal "quotes": true, 18446744073709551615`, "empty": "", "binary": "[binary: 3 bytes; base64: //4A]", "invalid": "[invalid base64: not base64!]"} {
			if values[key] != want {
				t.Fatalf("JSON=%v %s: want %q, got %q", asJSON, key, want, values[key])
			}
		}
		if !strings.HasPrefix(values["control"].(string), "[binary:") || strings.ContainsAny(text, "\x1b\a") {
			t.Fatal("decoded control bytes entered the terminal")
		}
		if result["stringData"].(map[string]interface{})["plain"] != "readable" || strings.Contains(text, "duplicate-secret") || strings.Contains(text, "managedFields") {
			t.Fatal("stringData or inspection metadata was handled incorrectly")
		}
		if strings.Contains(Manifest(secret, asJSON), "demo-only") {
			t.Fatal("default manifest stopped redacting")
		}
	}
	if !reflect.DeepEqual(secret.Object, before.Object) {
		t.Fatal("decoding changed the source API object")
	}
}
