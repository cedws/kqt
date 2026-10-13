package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/rest"
)

func TestAccessReviewsScopeDecisionsIdentityAndErrors(t *testing.T) {
	for _, mode := range []string{"allowed", "denied", "unavailable", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/apis/authentication.k8s.io/v1/selfsubjectreviews" {
					fmt.Fprint(w, `{"apiVersion":"authentication.k8s.io/v1","kind":"SelfSubjectReview","status":{"userInfo":{"username":"fixture-user","groups":["devs"]}}}`)
					return
				}
				if r.URL.Path != "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews" || r.Method != http.MethodPost {
					t.Errorf("wrong access endpoint: %s", r.URL)
				}
				var body map[string]interface{}
				json.NewDecoder(r.Body).Decode(&body)
				attrs := body["spec"].(map[string]interface{})["resourceAttributes"].(map[string]interface{})
				if attrs["namespace"] != "apps" || attrs["resource"] != "pods" || attrs["name"] != "web" || attrs["verb"] != "get" || attrs["subresource"] != "log" {
					t.Errorf("incorrect access scope: %+v", attrs)
				}
				if mode == "unavailable" {
					w.WriteHeader(403)
					fmt.Fprint(w, `{"apiVersion":"v1","kind":"Status","reason":"Forbidden","status":"Failure","code":403}`)
					return
				}
				status := map[string]interface{}{"allowed": mode == "allowed", "reason": "fixture policy"}
				if mode == "malformed" {
					status = map[string]interface{}{}
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"apiVersion": "authorization.k8s.io/v1", "kind": "SelfSubjectAccessReview", "status": status})
			}))
			defer server.Close()
			c, e := NewClient(&rest.Config{Host: server.URL})
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			r, _ := Resolve(Builtins(), "pods")
			decision, e := c.ReviewAccess(context.Background(), AccessRequest{Resource: r, Namespace: "apps", Name: "web", Verb: "get", Subresource: "log"})
			if mode == "unavailable" || mode == "malformed" {
				if e == nil {
					t.Fatal("unavailable review became an authorization decision")
				}
			} else if e != nil || decision.Allowed != (mode == "allowed") {
				t.Fatalf("wrong decision %+v %v", decision, e)
			}
			identity, e := c.Identity(context.Background())
			if e != nil || identity.Username != "fixture-user" || len(identity.Groups) != 1 {
				t.Fatal("identity review failed", e)
			}
		})
	}
}
