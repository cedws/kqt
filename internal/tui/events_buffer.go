package tui

import (
	"container/heap"
	"encoding/json"
	"sort"
	"sync"

	"github.com/z0mbix/kqt/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

const eventBufferBytes = 4 * 1024 * 1024
const eventBufferRecords = 2000

type eventEntry struct {
	object      unstructured.Unstructured
	deleted     bool
	size, index int
	sequence    uint64
}
type eventHeap []*eventEntry

func (h eventHeap) Len() int { return len(h) }
func (h eventHeap) Less(i, j int) bool {
	a, b := kube.EventTime(h[i].object), kube.EventTime(h[j].object)
	if !a.Equal(b) {
		return a.Before(b)
	}
	return h[i].sequence < h[j].sequence
}
func (h eventHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *eventHeap) Push(value any) {
	entry := value.(*eventEntry)
	entry.index = len(*h)
	*h = append(*h, entry)
}
func (h *eventHeap) Pop() any {
	old := *h
	entry := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	return entry
}

// Keep the latest version of each UID, evicting the oldest occurrence first.
// API removals retain the observed record until buffer eviction or view closure.
type eventBuffer struct {
	items         map[string]*eventEntry
	order         eventHeap
	size, dropped int
	sequence      uint64
}

func newEventBuffer() *eventBuffer { return &eventBuffer{items: make(map[string]*eventEntry)} }
func (b *eventBuffer) put(object unstructured.Unstructured, deleted bool) bool {
	key := kube.Identity(object)
	old := b.items[key]
	if old != nil && object.GetResourceVersion() != "" && old.object.GetResourceVersion() == object.GetResourceVersion() && old.deleted == deleted {
		return false
	}
	object = kube.Redacted(object)
	data, err := json.Marshal(object.Object)
	if err != nil || len(data) > eventBufferBytes {
		b.dropped++
		return false
	}
	entry := old
	if entry == nil {
		entry = &eventEntry{}
		b.items[key] = entry
	}
	if old == nil || !kube.EventTime(old.object).Equal(kube.EventTime(object)) || kube.EventCount(old.object) != kube.EventCount(object) || kube.EventMessage(old.object) != kube.EventMessage(object) {
		b.sequence++
		entry.sequence = b.sequence
	}
	b.size -= entry.size
	entry.object, entry.deleted, entry.size = object, deleted, len(data)
	b.size += entry.size
	if old == nil {
		heap.Push(&b.order, entry)
	} else {
		heap.Fix(&b.order, entry.index)
	}
	for len(b.items) > eventBufferRecords || b.size > eventBufferBytes {
		removed := heap.Pop(&b.order).(*eventEntry)
		delete(b.items, kube.Identity(removed.object))
		b.size -= removed.size
		b.dropped++
	}
	return true
}
func (b *eventBuffer) entries() []*eventEntry {
	rows := append(eventHeap(nil), b.order...)
	sort.Slice(rows, func(i, j int) bool { return rows.Less(i, j) })
	return rows
}
func (b *eventBuffer) objects() []unstructured.Unstructured {
	rows := make([]unstructured.Unstructured, 0, len(b.items))
	for _, entry := range b.entries() {
		rows = append(rows, entry.object)
	}
	return rows
}

type eventFeed struct {
	sync.Mutex
	pending        *eventBuffer
	state, message string
	synced, done   bool
	gaps           int
	err            error
	notify         chan struct{}
}
type eventBatch struct {
	changes        []kube.EventUpdate
	state, message string
	synced, done   bool
	dropped, gaps  int
	err            error
}

func newEventFeed() *eventFeed {
	return &eventFeed{pending: newEventBuffer(), notify: make(chan struct{}, 1)}
}
func (f *eventFeed) signal() {
	select {
	case f.notify <- struct{}{}:
	default:
	}
}
func (f *eventFeed) append(update kube.EventUpdate) {
	f.Lock()
	if f.done {
		f.Unlock()
		return
	}
	if update.Type == watch.Added || update.Type == watch.Modified || update.Type == watch.Deleted {
		f.pending.put(update.Object, update.Type == watch.Deleted)
	}
	if update.State != "" {
		f.state, f.message = update.State, kube.Safe(update.Message)
	}
	f.synced = f.synced || update.Synced
	if update.Gap {
		f.gaps++
	}
	f.Unlock()
	f.signal()
}
func (f *eventFeed) finish(err error) { f.Lock(); f.done, f.err = true, err; f.Unlock(); f.signal() }
func (f *eventFeed) drain() eventBatch {
	f.Lock()
	defer f.Unlock()
	batch := eventBatch{state: f.state, message: f.message, synced: f.synced, done: f.done, err: f.err, dropped: f.pending.dropped, gaps: f.gaps}
	for _, entry := range f.pending.entries() {
		kind := watch.Modified
		if entry.deleted {
			kind = watch.Deleted
		}
		batch.changes = append(batch.changes, kube.EventUpdate{Type: kind, Object: entry.object})
	}
	f.pending = newEventBuffer()
	f.state, f.message = "", ""
	f.gaps = 0
	return batch
}
