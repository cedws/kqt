package kube

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// This test only runs against the disposable cluster owned by live-smoke.sh.
// It proves Restricted admission, tainted-node placement and scoped RBAC using
// real kubelet attachment, without granting the debug user exec or pod creation.
func TestLiveDebug(t *testing.T) {
	path := os.Getenv("KQT_LIVE_KUBECONFIG")
	if path == "" {
		t.Skip("disposable Kubernetes fixture only")
	}
	loader, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(loader.Current(), "k3d-kqt-smoke-") {
		t.Fatal("live debug qualification requires a disposable kqt-smoke cluster")
	}
	backend, _, _, err := loader.Open(loader.Current())
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	admin := backend.(*Client)
	clientset, err := kubernetes.NewForConfig(admin.config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ns := "kqt-debug-test"
	_, err = clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{"pod-security.kubernetes.io/enforce": "restricted", "pod-security.kubernetes.io/enforce-version": "latest"}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientset.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{}) })
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil || len(nodes.Items) == 0 {
		t.Fatalf("nodes: %v", err)
	}
	for _, node := range nodes.Items {
		node.Spec.Taints = append(node.Spec.Taints, corev1.Taint{Key: "kqt.dev/debug-test", Value: "restricted", Effect: corev1.TaintEffectNoSchedule})
		if _, err = clientset.CoreV1().Nodes().Update(ctx, &node, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		for _, node := range nodes.Items {
			current, err := clientset.CoreV1().Nodes().Get(cleanupCtx, node.Name, metav1.GetOptions{})
			if err != nil {
				t.Error(err)
				continue
			}
			var retained []corev1.Taint
			for _, taint := range current.Spec.Taints {
				if taint.Key != "kqt.dev/debug-test" {
					retained = append(retained, taint)
				}
			}
			current.Spec.Taints = retained
			if _, err = clientset.CoreV1().Nodes().Update(cleanupCtx, current, metav1.UpdateOptions{}); err != nil {
				t.Error(err)
			}
		}
	})
	_, err = clientset.CoreV1().ServiceAccounts(ns).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "debugger"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = clientset.RbacV1().Roles(ns).Create(ctx, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "debugger"}, Rules: []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"pods"}, ResourceNames: []string{"debug-target"}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"pods/ephemeralcontainers"}, ResourceNames: []string{"debug-target"}, Verbs: []string{"patch"}},
		{APIGroups: []string{""}, Resources: []string{"pods/attach"}, ResourceNames: []string{"debug-target"}, Verbs: []string{"get", "create"}},
	}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = clientset.RbacV1().RoleBindings(ns).Create(ctx, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "debugger"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "debugger", Namespace: ns}}, RoleRef: rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: "debugger"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	no := false
	uid := int64(1000)
	profile := debugTestProfile()
	profile.Image = "docker.io/library/busybox:1.37"
	profile.SecurityContext.RunAsUser = &uid
	_, err = clientset.CoreV1().Pods(ns).Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "debug-target"}, Spec: corev1.PodSpec{AutomountServiceAccountToken: &no, RestartPolicy: corev1.RestartPolicyNever,
		Tolerations: []corev1.Toleration{{Key: "kqt.dev/debug-test", Operator: corev1.TolerationOpEqual, Value: "restricted", Effect: corev1.TaintEffectNoSchedule}},
		Containers:  []corev1.Container{{Name: "app", Image: profile.Image, Command: []string{"sleep", "3600"}, SecurityContext: profile.SecurityContext.DeepCopy()}},
	}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	resource, _ := Resolve(Builtins(), "pods")
	waitLive(t, 60*time.Second, func() bool {
		pod, err := admin.Get(ctx, resource, ns, "debug-target")
		return err == nil && Text(pod, "status", "phase") == "Running"
	})
	selected, err := admin.Get(ctx, resource, ns, "debug-target")
	if err != nil {
		t.Fatal(err)
	}
	cfg := rest.CopyConfig(admin.config)
	cfg.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:" + ns + ":debugger", Groups: []string{"system:serviceaccounts", "system:serviceaccounts:" + ns, "system:authenticated"}}
	limited, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer limited.Close()
	for _, q := range []AccessRequest{{Resource: resource, Namespace: ns, Name: "debug-target", Verb: "create", Subresource: "exec"}, {Resource: resource, Namespace: ns, Verb: "create"}, {Resource: Resource{GVR: corev1.SchemeGroupVersion.WithResource("secrets")}, Namespace: ns, Verb: "get"}, {Resource: Resource{GVR: corev1.SchemeGroupVersion.WithResource("nodes")}, Verb: "list"}} {
		decision, err := limited.ReviewAccess(ctx, q)
		if err != nil || decision.Allowed {
			t.Fatalf("debug user has unnecessary rights: %#v, %v", q, err)
		}
	}
	for _, spdyOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("spdy-only=%t", spdyOnly), func(t *testing.T) {
			selected, err = admin.Get(ctx, resource, ns, selected.GetName())
			if err != nil {
				t.Fatal(err)
			}
			plan, err := PrepareDebug(ctx, limited, selected, profile, "app", "")
			if err != nil {
				t.Fatal(err)
			}
			before, err := admin.Get(ctx, resource, ns, selected.GetName())
			if err != nil || len(DebugContainers(before)) != len(DebugContainers(selected)) {
				t.Fatal("dry-run created a container")
			}
			created, err := ApplyDebug(ctx, limited, plan)
			if err != nil {
				t.Fatal(err)
			}
			running, err := WaitDebug(ctx, limited, created, plan.Container.Name)
			if err != nil {
				t.Fatal(err)
			}
			if Text(running, "spec", "nodeName") != Text(selected, "spec", "nodeName") {
				t.Fatal("ephemeral container moved to a different node")
			}
			attachCtx, disconnect := context.WithCancel(ctx)
			defer disconnect()
			input, writer := io.Pipe()
			var output errorBuffer
			defer input.Close()
			defer writer.Close()
			done := make(chan error, 1)
			go func() {
				done <- limited.Attach(attachCtx, running, plan.Container.Name, spdyOnly, remotecommand.StreamOptions{Stdin: input, Stdout: &output, Tty: true})
			}()
			go func() { _, _ = io.WriteString(writer, "printf '%s%s\\n' kqt- debug-connected\n") }()
			waitLive(t, 10*time.Second, func() bool { return strings.Contains(output.String(), "kqt-debug-connected") })
			disconnect()
			_ = writer.Close()
			_ = input.Close()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("attach did not cancel")
			}
			reattach, err := PrepareDebug(ctx, limited, running, DebugProfile{}, "", plan.Container.Name)
			if err != nil || !reattach.AttachOnly {
				t.Fatalf("reattach preflight: %v", err)
			}
			var final errorBuffer
			reattachInput, reattachWriter := io.Pipe()
			defer reattachInput.Close()
			defer reattachWriter.Close()
			reattachDone := make(chan error, 1)
			go func() {
				reattachDone <- limited.Attach(ctx, running, plan.Container.Name, spdyOnly, remotecommand.StreamOptions{Stdin: reattachInput, Stdout: &final, Tty: true})
			}()
			go func() { _, _ = io.WriteString(reattachWriter, "printf '%s%s\\n' kqt- debug-reattached\nexit\n") }()
			select {
			case err = <-reattachDone:
			case <-time.After(10 * time.Second):
				t.Fatal("reattach did not exit")
			}
			if err != nil || !strings.Contains(final.String(), "kqt-debug-reattached") {
				observed, _ := admin.Get(ctx, resource, ns, selected.GetName())
				t.Fatalf("reattach failed: output %q, error %v, status %#v", final.String(), err, observed.Object["status"])
			}
			selected, err = admin.Get(ctx, resource, ns, selected.GetName())
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	// Admission rejects a locally approved profile that violates namespace policy.
	bad := profile
	bad.SecurityContext = profile.SecurityContext.DeepCopy()
	yes := true
	bad.SecurityContext.Privileged = &yes
	if _, err = PrepareDebug(ctx, limited, selected, bad, "", ""); err == nil {
		t.Fatal("Restricted admission accepted a privileged debugger")
	}
}
