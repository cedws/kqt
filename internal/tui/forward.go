package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"ket/internal/kube"
)

type forwardSession struct {
	id     uint64
	target string
	local  uint16
	remote int
	state  string
	cancel context.CancelFunc
	events chan forwardMsg
}
type forwardMsg struct {
	generation, id uint64
	local          uint16
	pod            string
	remote         int
	done           bool
	err            error
}

func parsePorts(value string) (int, int, error) {
	pair := strings.Split(value, ":")
	if len(pair) != 2 {
		return 0, 0, fmt.Errorf("use :forward LOCAL:REMOTE (LOCAL 0 chooses a free port)")
	}
	local, err := strconv.Atoi(pair[0])
	if err != nil || local < 0 || local > 65535 {
		return 0, 0, fmt.Errorf("local port must be 0 to 65535")
	}
	remote, err := strconv.Atoi(pair[1])
	if err != nil || remote < 1 || remote > 65535 {
		return 0, 0, fmt.Errorf("remote port must be 1 to 65535")
	}
	return local, remote, nil
}
func (m *model) forwardCommand(value string) tea.Cmd {
	if strings.HasPrefix(value, "stop ") {
		name := strings.TrimSpace(strings.TrimPrefix(value, "stop "))
		if name == "all" {
			m.stopForwards()
			m.notice = "Stopping all port forwards"
			return nil
		}
		id, err := strconv.ParseUint(name, 10, 64)
		if err != nil || m.forwards[id] == nil {
			m.notice = "Unknown port forward ID"
			return nil
		}
		m.stopForward(id)
		return nil
	}
	local, remote, err := parsePorts(value)
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	target, ok := m.current()
	if !ok {
		return nil
	}
	backend, ok := m.backend.(kube.PodForwarder)
	if !ok {
		m.notice = "Port forwarding requires a live Kubernetes connection"
		return nil
	}
	if target.GetKind() != "Pod" && target.GetKind() != "Service" {
		m.notice = "Select a pod or service to forward"
		return nil
	}
	if m.forwards == nil {
		m.forwards = map[uint64]*forwardSession{}
	}
	active := 0
	for _, f := range m.forwards {
		if f.state == "starting" || f.state == "active" || f.state == "stopping" {
			active++
		}
	}
	if active >= 16 {
		m.notice = "Stop an existing forward before starting another (limit 16)"
		return nil
	}
	ctx, cancel := context.WithCancel(m.session.ctx)
	m.forwardID++
	f := &forwardSession{id: m.forwardID, target: target.GetNamespace() + "/" + target.GetName(), local: uint16(local), remote: remote, state: "starting", cancel: cancel, events: make(chan forwardMsg, 2)}
	if len(m.forwards) >= 128 {
		ids := make([]uint64, 0, len(m.forwards))
		for id := range m.forwards {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			previous := m.forwards[id]
			if previous.state == "stopped" || strings.HasPrefix(previous.state, "failed:") {
				delete(m.forwards, id)
				if len(m.forwards) < 128 {
					break
				}
			}
		}
	}
	m.forwards[f.id] = f
	g, b, id := m.generation, m.backend, f.id
	m.notice = fmt.Sprintf("Starting port forward #%d · %s · :forwards to manage", id, f.target)
	go func() {
		request, done := context.WithTimeout(ctx, 15*time.Second)
		pod, port, err := kube.ForwardTarget(request, b, target, remote)
		done()
		if err == nil {
			err = backend.Forward(ctx, pod, fmt.Sprintf("%d:%d", local, port), func(actual uint16) {
				f.events <- forwardMsg{generation: g, id: id, local: actual, pod: pod.GetNamespace() + "/" + pod.GetName(), remote: port}
			})
		}
		f.events <- forwardMsg{generation: g, id: id, done: true, err: err}
	}()
	return waitForward(m.session.ctx, f)
}
func waitForward(ctx context.Context, f *forwardSession) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case event := <-f.events:
			return event
		}
	}
}
func (m *model) forwardUpdate(v forwardMsg) tea.Cmd {
	f := m.forwards[v.id]
	if v.generation != m.generation || f == nil {
		return nil
	}
	if v.done {
		f.cancel()
		f.state = "stopped"
		if v.err != nil && v.err != context.Canceled {
			f.state = "failed: " + kube.Safe(v.err.Error())
			m.notice = fmt.Sprintf("Port forward #%d · %s", f.id, f.state)
		}
	} else {
		f.local, f.remote, f.target = v.local, v.remote, v.pod
		if f.state != "stopping" {
			f.state = "active"
		}
		m.notice = fmt.Sprintf("Forward #%d · 127.0.0.1:%d → %s:%d · :forwards", f.id, f.local, f.target, f.remote)
	}
	if m.mode == "picker" && m.pickerKind == "forwards" {
		m.refreshForwardChoices()
	}
	if m.mode == "forward-details" && m.forwardDetail == v.id {
		m.showForward(v.id)
	}
	if !v.done {
		return waitForward(m.session.ctx, f)
	}
	return nil
}
func (m *model) stopForward(id uint64) {
	if f := m.forwards[id]; f != nil {
		f.cancel()
		if f.state == "starting" || f.state == "active" {
			f.state = "stopping"
		}
		m.notice = fmt.Sprintf("Stopping port forward #%d", id)
	}
}
func (m *model) stopForwards() {
	for id := range m.forwards {
		m.stopForward(id)
	}
}
func (m *model) refreshForwardChoices() {
	m.choices = nil
	ids := make([]uint64, 0, len(m.forwards))
	for id := range m.forwards {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if f := m.forwards[id]; f != nil {
			m.choices = append(m.choices, choice{strconv.FormatUint(id, 10), fmt.Sprintf("127.0.0.1:%d → %s:%d · %s", f.local, f.target, f.remote, f.state)})
		}
	}
	m.filterChoices()
}
func (m *model) promptForward() tea.Cmd {
	o, ok := m.current()
	if !ok {
		return nil
	}
	for _, value := range kube.Slice(o, "spec", "ports") {
		p, _ := value.(map[string]interface{})
		if p["protocol"] == nil || p["protocol"] == "TCP" {
			return m.startInput("command", "forward 0:"+fmt.Sprint(p["port"]))
		}
	}
	for _, value := range kube.Slice(o, "spec", "containers") {
		c, _ := value.(map[string]interface{})
		ports, _ := c["ports"].([]interface{})
		for _, value := range ports {
			p, _ := value.(map[string]interface{})
			if p["protocol"] == nil || p["protocol"] == "TCP" {
				return m.startInput("command", "forward 0:"+fmt.Sprint(p["containerPort"]))
			}
		}
	}
	return m.startInput("command", "forward 0:")
}
func (m *model) showForward(id uint64) {
	if f := m.forwards[id]; f != nil {
		m.forwardDetail = id
		m.mode = "forward-details"
		m.panelTitle = fmt.Sprintf("Port forward #%d", id)
		m.panel.SoftWrap = true
		m.panel.SetContent(fmt.Sprintf("Context: %s\nLocal: 127.0.0.1:%d\nPod: %s\nRemote port: %d\nState: %s\n\nx stops this forward; Escape returns to resources.", kube.Safe(m.contextName), f.local, kube.Safe(f.target), f.remote, kube.Safe(f.state)))
	}
}
