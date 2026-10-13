package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

func liveEvent(id string, second, count int64) unstructured.Unstructured {
	o := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "v1", "kind": "Event", "metadata": map[string]interface{}{"name": id, "namespace": "default", "uid": id}, "type": "Warning", "reason": "BackOff", "message": "retry web", "count": count, "lastTimestamp": time.Unix(second, 0).UTC().Format(time.RFC3339Nano), "involvedObject": map[string]interface{}{"kind": "Pod", "name": "web", "uid": "pod-uid"}}}
	o.SetResourceVersion(fmt.Sprintf("%d/%d", second, count))
	return o
}

func TestEventBufferUpsertDeletionAndBoundedRetention(t *testing.T) {
	b := newEventBuffer()
	o := liveEvent("retry", 100, 1)
	before := o.DeepCopy()
	if !b.put(o, false) || b.put(o, false) {
		t.Fatal("resource version deduplication failed")
	}
	updated := liveEvent("retry", 110, 9)
	b.put(updated, false)
	b.put(updated, true)
	if len(b.items) != 1 || !b.items["retry"].deleted || kube.EventCount(b.objects()[0]) != 9 || !reflect.DeepEqual(o.Object, before.Object) {
		t.Fatal("updates duplicated rows or changed source objects")
	}
	for i := 0; i < eventBufferRecords+100; i++ {
		// Name order disagrees with chronological order.
		b.put(liveEvent(fmt.Sprintf("event-%04d", eventBufferRecords+100-i), int64(200+i), 1), false)
		if i%17 == 0 {
			b.objects() // Reading must not corrupt heap indices used by later updates.
		}
	}
	if len(b.items) != eventBufferRecords || b.dropped != 101 || !kube.EventTime(b.objects()[0]).Equal(time.Unix(300, 0)) {
		t.Fatal("buffer did not retain the newest occurrences")
	}
	b.put(liveEvent("event-0001", 9999, 2), false)
	for i, entry := range b.order {
		if entry.index != i {
			t.Fatal("sorted reads corrupted heap indices")
		}
	}
	if kube.EventTime(b.objects()[len(b.items)-1]).Unix() != 9999 {
		t.Fatal("updated occurrence did not move to the end")
	}
	large := newEventBuffer()
	for i := 0; i < 6; i++ {
		o := liveEvent(fmt.Sprint(i), int64(i), 1)
		o.Object["message"] = strings.Repeat("x", 1024*1024)
		large.put(o, false)
	}
	if large.size > eventBufferBytes || len(large.items) != 3 {
		t.Fatal("byte retention limit failed")
	}
	o.Object["message"] = strings.Repeat("x", eventBufferBytes+1)
	if large.put(o, false) || large.size > eventBufferBytes {
		t.Fatal("oversized record retained")
	}
}

func TestEventFeedCoalescesBurstsAndRejectsAfterFinish(t *testing.T) {
	f := newEventFeed()
	for i := 1; i <= 10000; i++ {
		f.append(kube.EventUpdate{Type: watch.Modified, Object: liveEvent("same", int64(i), int64(i))})
	}
	f.append(kube.EventUpdate{State: "reconnecting", Message: "retry\x1b[2J", Synced: true, Gap: true})
	f.append(kube.EventUpdate{Type: watch.Deleted, Object: liveEvent("same", 10000, 10000)})
	f.finish(errors.New("ended"))
	f.append(kube.EventUpdate{State: "live", Object: liveEvent("late", 10001, 1), Type: watch.Added})
	b := f.drain()
	if len(b.changes) != 1 || b.changes[0].Type != watch.Deleted || kube.EventCount(b.changes[0].Object) != 10000 || !b.done || b.err == nil || !b.synced || b.gaps != 1 || b.state != "reconnecting" || strings.Contains(b.message, "\x1b") {
		t.Fatalf("burst/final state lost: %#v", b)
	}
	if len(f.notify) != 1 || len(f.pending.items) != 0 || f.drain().gaps != 0 {
		t.Fatal("notification queue or drained state grew")
	}
}

type testEventSession struct {
	ctx   context.Context
	query kube.Query
}
type eventBackend struct {
	*kube.Demo
	sessions chan testEventSession
}

func (b *eventBackend) FollowEvents(ctx context.Context, query kube.Query, emit func(kube.EventUpdate)) error {
	b.sessions <- testEventSession{ctx, query}
	<-ctx.Done()
	return nil
}
func eventModel(t *testing.T, global bool) (*model, testEventSession) {
	t.Helper()
	m := demoModel(t)
	b := &eventBackend{m.backend.(*kube.Demo), make(chan testEventSession, 8)}
	m.backend = b
	if global {
		m.command("events")
	} else {
		m.Update(m.openEvents()())
	}
	select {
	case session := <-b.sessions:
		return m, session
	case <-time.After(time.Second):
		t.Fatal("event watch did not start")
		return nil, testEventSession{}
	}
}
func pulseEvent(m *model, object unstructured.Unstructured, kind watch.EventType) {
	m.eventFeed.append(kube.EventUpdate{Object: object, Type: kind, State: "live", Synced: true})
	m.Update(eventPulseMsg{m.generation, m.eventToken, m.eventFeed})
}

func TestGlobalEventsPauseFollowScopeAndModalCleanup(t *testing.T) {
	m, session := eventModel(t, true)
	if !m.eventGlobal || m.sortColumn != "lastseen" || session.query.Namespace != "default" {
		t.Fatal("events did not open as scoped live table")
	}
	pulseEvent(m, liveEvent("old", 100, 1), watch.Added)
	pulseEvent(m, liveEvent("new", 110, 1), watch.Added)
	if o, _ := m.current(); o.GetName() != "new" {
		t.Fatal("table did not follow newest event")
	}
	key(m, " ")
	before := m.eventExport()
	pulseEvent(m, liveEvent("new", 120, 7), watch.Modified)
	if m.eventExport() != before || kube.EventCount(m.eventStore.items["new"].object) != 7 {
		t.Fatal("pause did not freeze display while collecting")
	}
	key(m, " ")
	if !strings.Contains(m.eventExport(), "×7") || len(m.rows) != 2 {
		t.Fatal("unpause did not merge count in place")
	}
	key(m, "k")
	pulseEvent(m, liveEvent("new", 130, 8), watch.Modified)
	if o, _ := m.current(); o.GetName() != "old" || m.eventTail {
		t.Fatal("new events moved a browsing selection")
	}
	key(m, "G")
	if o, _ := m.current(); o.GetName() != "new" || !m.eventTail {
		t.Fatal("G did not resume following")
	}
	key(m, "J")
	key(m, "esc")
	if session.ctx.Err() != nil || !m.eventGlobal {
		t.Fatal("closing event JSON stopped global watch")
	}
	m.selectLayout("inspector")
	m.Update(tea.WindowSizeMsg{Width: 180, Height: 48})
	key(m, "tab")
	key(m, "G")
	if !m.inspectorFocused || m.eventTail {
		t.Fatal("inspector scrolling resumed table following")
	}
	key(m, "tab")
	key(m, "G")
	m.Update(eventSavedMsg{m.generation, m.eventToken, "events.log", nil})
	if !strings.Contains(ansi.Strip(m.View().Content), "Events saved to events.log") {
		t.Fatal("live event footer hid the save confirmation")
	}
	m.command("ns platform")
	if session.ctx.Err() == nil {
		t.Fatal("namespace change left old watch running")
	}
	select {
	case next := <-m.backend.(*eventBackend).sessions:
		if next.query.Namespace != "platform" {
			t.Fatal("new watch has old namespace")
		}
	case <-time.After(time.Second):
		t.Fatal("new namespace did not start watch")
	}
	feed, token := m.eventFeed, m.eventToken
	m.command("pods")
	m.Update(eventPulseMsg{m.generation, token, feed})
	m.Update(eventSavedMsg{m.generation, token, "old-events.log", nil})
	if m.eventFeed != nil || m.eventGlobal {
		t.Fatal("late event pulse leaked after navigation")
	}
	if strings.Contains(m.notice, "old-events") {
		t.Fatal("late export changed the new view")
	}
}

func TestSelectedLiveEventsPauseSearchExportAndReplacement(t *testing.T) {
	m, session := eventModel(t, false)
	if session.query.Fields != "involvedObject.uid="+string(m.eventObject.GetUID()) || session.query.Resource.Kind != "Event" {
		t.Fatal("selected events were not bound to UID")
	}
	pulseEvent(m, liveEvent("retry", 100, 1), watch.Added)
	key(m, " ")
	before := m.panel.GetContent()
	pulseEvent(m, liveEvent("retry", 110, 7), watch.Modified)
	if m.panel.GetContent() != before {
		t.Fatal("paused status panel moved")
	}
	key(m, " ")
	if !strings.Contains(m.panel.GetContent(), "×7") {
		t.Fatal("unpause missed repeats")
	}
	m.eventsInput("event-search")
	m.input.SetValue("no match")
	m.eventsInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(m.panel.GetContent(), "No events match") || !strings.Contains(m.panel.GetContent(), "Containers") {
		t.Fatal("search lost status or did not filter events")
	}
	m.eventsInput("event-search")
	m.input.SetValue("retry")
	m.eventsInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	pulseEvent(m, liveEvent("retry", 110, 7), watch.Deleted)
	if !strings.Contains(m.panel.GetContent(), "removed") {
		t.Fatal("API removal lost observed history")
	}
	file := filepath.Join(t.TempDir(), "events.log")
	m.eventsInput("event-save")
	m.input.SetValue(file)
	m.Update(m.eventsInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})())
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 || strings.Contains(string(data), "\x1b") || !strings.Contains(string(data), "×7") {
		t.Fatal("private export lost visible text")
	}
	m.eventsInput("event-save")
	m.input.SetValue(file)
	m.Update(m.eventsInputKey(tea.KeyPressMsg{Code: tea.KeyEnter})())
	if !strings.Contains(m.notice, "Unable to save events") {
		t.Fatal("export overwrote existing file")
	}
	old := m.eventObject.DeepCopy()
	m.Update(eventStatusMsg{generation: m.generation, token: m.eventToken, err: errors.New("get denied")})
	if !strings.Contains(m.panel.GetContent(), "STALE RESOURCE STATUS") || session.ctx.Err() != nil {
		t.Fatal("GET failure hid status or stopped event collection")
	}
	changed := *old.DeepCopy()
	changed.SetUID(types.UID("replacement"))
	m.Update(eventStatusMsg{m.generation, m.eventToken, changed, errors.New("resource replaced"), true})
	m.eventFeed.append(kube.EventUpdate{State: "live", Synced: true})
	m.Update(eventPulseMsg{m.generation, m.eventToken, m.eventFeed})
	if session.ctx.Err() == nil || m.eventState != "ended" || m.eventObject.GetUID() != old.GetUID() {
		t.Fatal("replacement reused old resource watch or accepted late live state")
	}
	key(m, "esc")
	if m.eventFeed != nil || m.mode != "" {
		t.Fatal("closing selected status leaked the watch")
	}
}

func TestLiveEventsDimensionsHistoryGapAndCancelledWait(t *testing.T) {
	m, session := eventModel(t, false)
	pulseEvent(m, liveEvent("retry", 100, 1), watch.Added)
	m.eventFeed.append(kube.EventUpdate{State: "reconnecting", Message: "history expired", Gap: true})
	m.Update(eventPulseMsg{m.generation, m.eventToken, m.eventFeed})
	if !strings.Contains(m.panel.GetContent(), "some events may have been missed") {
		t.Fatal("history gap was hidden")
	}
	for _, size := range [][2]int{{40, 12}, {80, 24}, {180, 48}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.View().Content)
		if len(strings.Split(view, "\n")) != size[1] {
			t.Fatal("event view height exceeds terminal")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("event view width exceeds terminal")
			}
		}
	}
	feed := m.eventFeed
	m.closeEvents()
	if session.ctx.Err() == nil || waitEvent(session.ctx, m.generation, m.eventToken, feed)() != nil {
		t.Fatal("cancelled event waiter remained active")
	}
}

type eventStatusBackend struct {
	*eventBackend
	fresh                   unstructured.Unstructured
	readNamespace, readName string
}

func (b *eventStatusBackend) Get(_ context.Context, _ kube.Resource, namespace, name string) (unstructured.Unstructured, error) {
	b.readNamespace, b.readName = namespace, name
	return *b.fresh.DeepCopy(), nil
}

func TestSelectedEventStatusRefreshReadsFreshIdentity(t *testing.T) {
	m, session := eventModel(t, false)
	b := &eventStatusBackend{eventBackend: m.backend.(*eventBackend), fresh: *m.eventObject.DeepCopy()}
	m.backend = b
	b.fresh.Object["status"].(map[string]interface{})["phase"] = "Failed"
	msg := m.eventStatusTick(eventStatusTickMsg{m.generation, m.eventToken})().(eventStatusMsg)
	m.Update(msg)
	if b.readNamespace != m.eventObject.GetNamespace() || b.readName != m.eventObject.GetName() || kube.Text(m.eventObject, "status", "phase") != "Failed" {
		t.Fatal("status refresh did not fetch the selected resource")
	}
	b.fresh.SetUID(types.UID("new-pod"))
	msg = m.eventStatusTick(eventStatusTickMsg{m.generation, m.eventToken})().(eventStatusMsg)
	if !msg.replaced || msg.err == nil {
		t.Fatal("refresh accepted a replaced resource")
	}
	m.Update(msg)
	if session.ctx.Err() == nil || m.eventStatusTick(eventStatusTickMsg{m.generation, m.eventToken}) != nil {
		t.Fatal("replacement left status polling running")
	}
}
