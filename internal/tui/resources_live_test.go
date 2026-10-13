package tui

import (
	"context"
	"fmt"
	"testing"

	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

func testResourceFeed(m *model) *resourceFeed {
	m.stopResourceWatch()
	m.resourceFeed = &resourceFeed{changes: make(chan kube.EventUpdate, 64)}
	m.resourceCache = map[string]unstructured.Unstructured{}
	for _, o := range m.rows {
		m.resourceCache[kube.Identity(o)] = o
	}
	m.resourceContext = m.session.ctx
	return m.resourceFeed
}
func resourceChanges(m *model, f *resourceFeed, changes ...kube.EventUpdate) {
	m.resourceBatch(resourceBatchMsg{generation: m.generation, token: m.resourceToken, feed: f, changes: changes})
}
func TestResourceWatchChangesResyncSelectionAndCancellation(t *testing.T) {
	m := demoModel(t)
	f := testResourceFeed(m)
	m.selected = 3
	selected := m.visible[3]
	changed := *selected.DeepCopy()
	changed.SetResourceVersion("2")
	unstructured.SetNestedField(changed.Object, "Failed", "status", "phase")
	resourceChanges(m, f, kube.EventUpdate{Type: watch.Modified, Object: changed})
	if current, _ := m.current(); current.GetUID() != selected.GetUID() || kube.Text(current, "status", "phase") != "Failed" {
		t.Fatal("watch update lost selection or status")
	}
	replacement := *changed.DeepCopy()
	replacement.SetUID("replacement")
	replacement.SetName("replacement")
	resourceChanges(m, f, kube.EventUpdate{Reset: true, State: "listing"}, kube.EventUpdate{Type: watch.Added, Object: replacement})
	if len(m.rows) != 12 {
		t.Fatal("partial relist replaced stable snapshot")
	}
	resourceChanges(m, f, kube.EventUpdate{Synced: true, State: "live"})
	if len(m.rows) != 1 || m.rows[0].GetUID() != replacement.GetUID() {
		t.Fatal("resync retained deleted or missing objects")
	}
	resourceChanges(m, f, kube.EventUpdate{Type: watch.Deleted, Object: replacement})
	if len(m.rows) != 0 {
		t.Fatal("watch deletion retained row")
	}
	token := m.resourceToken
	m.navigate(m.route, false)
	m.resourceBatch(resourceBatchMsg{generation: m.generation, token: token, feed: f, changes: []kube.EventUpdate{{Type: watch.Added, Object: changed}}})
	if len(m.rows) != 0 {
		t.Fatal("old-route watch updated new view")
	}
}
func TestResourceWatchDeniedFallsBackToPollingAndSecretPayloadIsHidden(t *testing.T) {
	m := demoModel(t)
	f := testResourceFeed(m)
	m.resourceBatch(resourceBatchMsg{generation: m.generation, token: m.resourceToken, feed: f, done: true, err: fmt.Errorf("forbidden")})
	if m.resourceFeed != nil || m.resourceWatchError != "forbidden" || len(m.rows) != 12 {
		t.Fatal("denied watch lost data or polling fallback")
	}
	cmd := m.fetch()
	m.Update(cmd())
	if m.resourceFeed != nil {
		t.Fatal("polling retried denied watch automatically")
	}
	m.Update(m.changeResource("secrets", false)())
	m.Update(m.toggleSecret()())
	f = testResourceFeed(m)
	o := *m.visible[0].DeepCopy()
	o.SetResourceVersion("changed")
	resourceChanges(m, f, kube.EventUpdate{Type: watch.Modified, Object: o})
	if m.secretObject != nil || m.secretTarget != "" {
		t.Fatal("changed Secret retained stale revealed values")
	}
	m.close()
	if m.resourceContext != nil {
		t.Fatal("closed view retained watch context")
	}
}
func TestResourceFeedBackpressureCancelsWithoutDroppingPendingChanges(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &resourceFeed{changes: make(chan kube.EventUpdate, 1)}
	f.changes <- kube.EventUpdate{State: "live"}
	cancel()
	if waitResources(ctx, 1, 1, f)() != nil {
		t.Fatal("cancelled queue waiter survived")
	}
	if len(f.changes) != 1 {
		t.Fatal("cancellation consumed another view's pending change")
	}
}
