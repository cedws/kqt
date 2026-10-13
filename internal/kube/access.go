package kube

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type AccessRequest struct {
	Resource                           Resource
	Namespace, Name, Verb, Subresource string
}
type AccessDecision struct {
	Allowed bool
	Reason  string
}
type UserIdentity struct {
	Username string
	Groups   []string
}
type AccessReviewer interface {
	ReviewAccess(context.Context, AccessRequest) (AccessDecision, error)
	Identity(context.Context) (UserIdentity, error)
}

func (c *Client) ReviewAccess(ctx context.Context, q AccessRequest) (AccessDecision, error) {
	object := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "authorization.k8s.io/v1", "kind": "SelfSubjectAccessReview", "spec": map[string]interface{}{"resourceAttributes": map[string]interface{}{"namespace": q.Namespace, "name": q.Name, "verb": q.Verb, "group": q.Resource.GVR.Group, "version": q.Resource.GVR.Version, "resource": q.Resource.GVR.Resource, "subresource": q.Subresource}}}}
	r := schema.GroupVersionResource{Group: "authorization.k8s.io", Version: "v1", Resource: "selfsubjectaccessreviews"}
	response, err := c.dynamic.Resource(r).Create(ctx, &object, metav1.CreateOptions{})
	if err != nil {
		return AccessDecision{}, err
	}
	allowed, present, err := unstructured.NestedBool(response.Object, "status", "allowed")
	if err != nil || !present {
		return AccessDecision{}, fmt.Errorf("access review did not report a decision")
	}
	denied, _, _ := unstructured.NestedBool(response.Object, "status", "denied")
	if allowed && denied {
		return AccessDecision{}, fmt.Errorf("access review returned conflicting decisions")
	}
	reason := Text(*response, "status", "reason")
	if evaluation := Text(*response, "status", "evaluationError"); evaluation != "" {
		reason += " " + evaluation
	}
	return AccessDecision{allowed, reason}, nil
}
func (c *Client) Identity(ctx context.Context) (UserIdentity, error) {
	object := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "authentication.k8s.io/v1", "kind": "SelfSubjectReview"}}
	r := schema.GroupVersionResource{Group: "authentication.k8s.io", Version: "v1", Resource: "selfsubjectreviews"}
	response, err := c.dynamic.Resource(r).Create(ctx, &object, metav1.CreateOptions{})
	if err != nil {
		return UserIdentity{}, err
	}
	groups, _, _ := unstructured.NestedStringSlice(response.Object, "status", "userInfo", "groups")
	name := Text(*response, "status", "userInfo", "username")
	if name == "" {
		return UserIdentity{}, fmt.Errorf("identity review did not return a username")
	}
	return UserIdentity{name, groups}, nil
}
func (*Demo) ReviewAccess(ctx context.Context, q AccessRequest) (AccessDecision, error) {
	if ctx.Err() != nil {
		return AccessDecision{}, ctx.Err()
	}
	return AccessDecision{true, "offline demo permissions"}, nil
}
func (*Demo) Identity(ctx context.Context) (UserIdentity, error) {
	if ctx.Err() != nil {
		return UserIdentity{}, ctx.Err()
	}
	return UserIdentity{"demo-user", []string{"demo"}}, nil
}
