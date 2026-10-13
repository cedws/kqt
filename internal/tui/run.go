package tui

import (
	"context"
	"os"

	tea "charm.land/bubbletea/v2"
	"ket/internal/kube"
)

func Run(ctx context.Context, s *Settings, o Options, l *kube.Loader, in, out *os.File) error {
	m, err := newModel(ctx, s, o, l)
	if err != nil {
		return err
	}
	defer m.close()
	_, err = tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out)).Run()
	return err
}

func (s *Settings) SelectTheme(name string) bool {
	_, ok := s.Themes[name]
	if ok {
		s.Config.Theme = name
	}
	return ok
}
func (s *Settings) ThemeNames() []string { return themeNames(s.Themes) }
func (s *Settings) SelectLayout(name string) bool {
	if _, ok := s.Layouts[name]; ok {
		s.Config.Layout = name
		return true
	}
	return false
}
