package tui

import (
	"slices"
	"strings"
)

type completionState struct {
	value   string
	matches []string
	index   int
}

var commandNames = []string{"overview", "access", "can-i", "permissions", "actions", "api", "delete", "restart", "resume", "run", "scale", "suspend", "columns", "copy", "context", "contexts", "ctx", "edit", "events", "export", "forward", "forwards", "help", "inspector", "label", "labels", "layout", "layouts", "logs", "metrics", "namespace", "ns", "q", "quit", "refresh", "related", "relations", "resources", "shell", "sort", "status", "theme", "themes", "write"}

func (m *model) completeCommand(reverse bool) {
	value := m.input.Value()
	if m.input.Position() != len([]rune(value)) {
		m.completion = completionState{}
		return
	}
	c := &m.completion
	if len(c.matches) == 0 || c.value != value {
		*c = completionState{}
		var candidates []string
		parts := strings.SplitN(value, " ", 2)
		if len(parts) == 1 {
			candidates = append(candidates, commandNames...)
			for _, r := range m.resources {
				candidates = append(candidates, r.ID(), r.GVR.Resource, strings.ToLower(r.Kind))
				candidates = append(candidates, r.Aliases...)
			}
		} else {
			prefix := parts[0] + " "
			var names []string
			switch parts[0] {
			case "theme", "themes":
				names = themeNames(m.settings.Themes)
			case "layout", "layouts":
				for name := range m.settings.Layouts {
					names = append(names, name)
				}
			case "ctx", "context", "contexts":
				if m.options.Demo {
					names = []string{"demo"}
				} else {
					for _, ctx := range m.loader.Contexts() {
						names = append(names, ctx.Name)
					}
				}
			case "ns", "namespace":
				names = []string{"-A", "all"}
				if m.route.namespace != "" {
					names = append(names, m.route.namespace)
				}
				for _, row := range m.rows {
					if row.GetKind() == "Namespace" {
						names = append(names, row.GetName())
					} else if row.GetNamespace() != "" {
						names = append(names, row.GetNamespace())
					}
				}
			case "can-i":
				names = []string{"get", "list", "watch", "patch", "delete", "create", "get log", "get exec", "create exec", "get portforward", "create portforward"}
			case "copy", "export":
				names = []string{"yaml", "json", "clean", "yaml clean", "json clean"}
			case "inspector":
				names = []string{"yaml", "json", "right", "bottom", "none"}
			case "sort":
				columns := append(m.columns(), "name", "age", "status", "restarts")
				for _, column := range columns {
					names = append(names, column, column+" asc", column+" desc")
				}
			case "logs":
				names = []string{"previous"}
			case "events":
				names = []string{"selected"}
			case "metrics":
				names = []string{"on", "off"}
			case "forward":
				names = []string{"stop all"}
			}
			for _, name := range names {
				candidates = append(candidates, prefix+name)
			}
		}
		slices.Sort(candidates)
		candidates = slices.Compact(candidates)
		for _, candidate := range candidates {
			if strings.HasPrefix(candidate, value) {
				c.matches = append(c.matches, candidate)
			}
		}
		if len(c.matches) == 0 {
			return
		}
		if reverse {
			c.index = len(c.matches) - 1
		}
	} else {
		direction := 1
		if reverse {
			direction = -1
		}
		c.index = (c.index + direction + len(c.matches)) % len(c.matches)
	}
	c.value = c.matches[c.index]
	m.input.SetValue(c.value)
	m.input.CursorEnd()
}
