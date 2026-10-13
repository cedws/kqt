package kube

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// DecodedSecretManifest is a display-only copy. Editing always uses the original
// API object, so decoded values can never replace base64 data in a patch.
func DecodedSecretManifest(o unstructured.Unstructured, asJSON bool) string {
	if o.GetKind() != "Secret" {
		return Manifest(o, asJSON)
	}
	o = *o.DeepCopy()
	o.SetManagedFields(nil)
	annotations := o.GetAnnotations()
	delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
	o.SetAnnotations(annotations)
	if values, ok := o.Object["data"].(map[string]interface{}); ok {
		for key, value := range values {
			encoded, ok := value.(string)
			if !ok {
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				values[key] = "[invalid base64: " + encoded + "]"
			} else if !utf8.Valid(decoded) || strings.IndexFunc(string(decoded), func(r rune) bool {
				return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t'
			}) >= 0 {
				values[key] = fmt.Sprintf("[binary: %d bytes; base64: %s]", len(decoded), encoded)
			} else {
				values[key] = string(decoded)
			}
		}
	}
	return manifestText(o, asJSON)
}
