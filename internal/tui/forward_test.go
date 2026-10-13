package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type cancellableForwardBackend struct{ *kube.Demo }

func (b *cancellableForwardBackend) Forward(ctx context.Context, pod unstructured.Unstructured, ports string, ready func(uint16)) error {
	ready(18080)
	<-ctx.Done()
	return fmt.Errorf("forward transport: %w", ctx.Err())
}

func TestForwardXCancellationFromResourceListAndManagementPanels(t *testing.T) {
	for _, view := range []string{"resources", "picker", "details", "manifest"} {
		t.Run(view, func(t *testing.T) {
			m := demoModel(t)
			m.backend = &cancellableForwardBackend{kube.NewDemo()}
			cmd := m.forwardCommand("0:8080")
			if cmd == nil {
				t.Fatal("forward did not start")
			}
			wait := m.forwardUpdate(cmd().(forwardMsg))
			if view == "picker" {
				m.openPicker("forwards")
			} else if view == "details" {
				m.showForward(1)
			} else if view == "manifest" {
				m.openDetails(true, false)
			}
			key(m, "x")
			if m.forwards[1].state != "stopping" {
				t.Fatalf("x did not cancel selected forward: %s", m.forwards[1].state)
			}
			result := make(chan forwardMsg, 1)
			go func() { result <- wait().(forwardMsg) }()
			select {
			case message := <-result:
				m.forwardUpdate(message)
			case <-time.After(time.Second):
				t.Fatal("cancel did not reach forwarding backend")
			}
			if m.forwards[1].state != "stopped" || strings.Contains(m.notice, "failed:") {
				t.Fatalf("cancellation reported a failure: %s / %s", m.forwards[1].state, m.notice)
			}
		})
	}
}

func TestForwardCancellationUsesResourceUIDAndIncludesResolvedServicePod(t *testing.T) {
	m := demoModel(t)
	pod, _ := m.current()
	other := m.visible[1]
	service := unstructured.Unstructured{}
	service.SetKind("Service")
	service.SetName("http")
	service.SetNamespace(pod.GetNamespace())
	service.SetUID("service-original")
	cancels := map[uint64]context.Context{}
	m.forwards = map[uint64]*forwardSession{}
	for id, keys := range map[uint64][2]string{
		1: {kube.Identity(pod), kube.Identity(pod)},
		2: {kube.Identity(other), kube.Identity(other)},
		3: {kube.Identity(service), kube.Identity(pod)},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		cancels[id] = ctx
		m.forwards[id] = &forwardSession{id: id, state: "active", sourceKey: keys[0], podKey: keys[1], cancel: cancel}
	}
	// A recreated service with the same name must not stop the original's forward.
	m.visible = []unstructured.Unstructured{*service.DeepCopy()}
	m.selected = 0
	m.visible[0].SetUID("service-replacement")
	key(m, "x")
	if cancels[3].Err() != nil {
		t.Fatal("replacement resource stopped original forward")
	}
	m.visible = []unstructured.Unstructured{pod}
	key(m, "x")
	if cancels[1].Err() == nil || cancels[3].Err() == nil || cancels[2].Err() != nil {
		t.Fatal("x failed to stop only the selected pod's direct and service forwards")
	}
	if !strings.Contains(m.notice, "Stopping 2 port forwards") {
		t.Fatal("multiple-forward cancellation was not explained")
	}
	// The source service remains associated after readiness resolves its target to a pod.
	m.forwards[3].state = "active"
	ctx, cancel := context.WithCancel(context.Background())
	m.forwards[3].cancel = cancel
	m.visible = []unstructured.Unstructured{service}
	key(m, "x")
	if ctx.Err() == nil {
		t.Fatal("service could not stop its resolved pod forward")
	}
}

func TestForwardStoppingIgnoresLateReadinessAndTransportShutdownErrors(t *testing.T) {
	m := demoModel(t)
	ctx, cancel := context.WithCancel(context.Background())
	m.forwards = map[uint64]*forwardSession{1: {id: 1, state: "starting", cancel: cancel}}
	m.stopForward(1)
	m.forwardUpdate(forwardMsg{generation: m.generation, id: 1, local: 8080, pod: "apps/web", remote: 80})
	if ctx.Err() == nil || m.forwards[1].state != "stopping" || !strings.HasPrefix(m.notice, "Stopping") {
		t.Fatal("late readiness revived a cancelled forward")
	}
	m.forwardUpdate(forwardMsg{generation: m.generation, id: 1, done: true, err: fmt.Errorf("connection closed during shutdown")})
	if m.forwards[1].state != "stopped" {
		t.Fatal("shutdown transport error turned cancellation into failure")
	}
	before := m.notice
	m.stopForward(1)
	if m.notice != before {
		t.Fatal("x on stopped forward restarted stopping state")
	}
	_, cancel = context.WithCancel(context.Background())
	m.forwards[2] = &forwardSession{id: 2, state: "active", cancel: cancel}
	m.forwardUpdate(forwardMsg{generation: m.generation, id: 2, done: true, err: fmt.Errorf("connection lost")})
	if !strings.Contains(m.forwards[2].state, "failed: connection lost") {
		t.Fatal("genuine transport failure was hidden")
	}
}

func TestForwardPickerKeepsSelectedIDWhenFilteredResultsChange(t *testing.T) {
	m := demoModel(t)
	m.forwards = map[uint64]*forwardSession{}
	for id := uint64(1); id <= 3; id++ {
		_, cancel := context.WithCancel(context.Background())
		m.forwards[id] = &forwardSession{id: id, state: "active", cancel: cancel}
	}
	m.openPicker("forwards")
	m.input.SetValue("active")
	m.filterChoices()
	m.pickerIndex = 1
	m.forwardUpdate(forwardMsg{generation: m.generation, id: 1, done: true})
	if m.matches[m.pickerIndex].name != "2" {
		t.Fatal("async forward completion moved selection to another forward")
	}
	key(m, "x")
	if m.forwards[2].state != "stopping" || m.forwards[3].state != "active" {
		t.Fatal("x stopped a different forward after picker refresh")
	}
}
