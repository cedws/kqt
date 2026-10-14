package tui

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

type theme struct {
	Name   string
	Colors map[string]string
}
type themeFile struct {
	Version int               `toml:"version"`
	Name    string            `toml:"name"`
	Extends string            `toml:"extends"`
	Colors  map[string]string `toml:"colors"`
}

func palette(name, bg, surface, fg, muted, border, accent, selected, success, warning, failure string) theme {
	return theme{name, map[string]string{"background": bg, "surface": surface, "foreground": fg, "muted": muted, "border": border, "accent": accent, "focus": accent, "selection_background": selected, "selection_foreground": fg, "success": success, "warning": warning, "error": failure}}
}

func builtThemes() map[string]theme {
	values := []theme{
		palette("terminal", "default", "default", "default", "7", "8", "6", "4", "2", "3", "1"),
		palette("nord", "#2e3440", "#3b4252", "#eceff4", "#d8dee9", "#4c566a", "#88c0d0", "#434c5e", "#a3be8c", "#ebcb8b", "#bf616a"),
		palette("rose-pine", "#191724", "#1f1d2e", "#e0def4", "#908caa", "#524f67", "#c4a7e7", "#403d52", "#9ccfd8", "#f6c177", "#eb6f92"),
		palette("rose-pine-moon", "#232136", "#2a273f", "#e0def4", "#908caa", "#56526e", "#c4a7e7", "#44415a", "#9ccfd8", "#f6c177", "#eb6f92"),
		palette("rose-pine-dawn", "#faf4ed", "#fffaf3", "#575279", "#797593", "#9893a5", "#907aa9", "#dfdad9", "#286983", "#996600", "#b4637a"),
		palette("catppuccin-mocha", "#1e1e2e", "#181825", "#cdd6f4", "#a6adc8", "#585b70", "#cba6f7", "#313244", "#a6e3a1", "#f9e2af", "#f38ba8"),
		palette("catppuccin-macchiato", "#24273a", "#1e2030", "#cad3f5", "#a5adcb", "#5b6078", "#c6a0f6", "#363a4f", "#a6da95", "#eed49f", "#ed8796"),
		palette("catppuccin-frappe", "#303446", "#292c3c", "#c6d0f5", "#a5adce", "#626880", "#ca9ee6", "#414559", "#a6d189", "#e5c890", "#e78284"),
		palette("catppuccin-latte", "#eff1f5", "#e6e9ef", "#4c4f69", "#5c5f77", "#9ca0b0", "#8839ef", "#ccd0da", "#287d19", "#8f5d08", "#d20f39"),
		palette("dracula", "#282a36", "#21222c", "#f8f8f2", "#b6b8c8", "#6272a4", "#bd93f9", "#44475a", "#50fa7b", "#f1fa8c", "#ff5555"),
		palette("gruvbox-dark", "#282828", "#32302f", "#ebdbb2", "#bdae93", "#7c6f64", "#fabd2f", "#504945", "#b8bb26", "#fabd2f", "#fb4934"),
		palette("gruvbox-light", "#fbf1c7", "#f2e5bc", "#3c3836", "#665c54", "#928374", "#076678", "#d5c4a1", "#79740e", "#8f3f00", "#9d0006"),
		palette("tokyo-night", "#1a1b26", "#16161e", "#c0caf5", "#a9b1d6", "#565f89", "#7aa2f7", "#292e42", "#9ece6a", "#e0af68", "#f7768e"),
		palette("tokyo-night-storm", "#24283b", "#1f2335", "#c0caf5", "#a9b1d6", "#565f89", "#7aa2f7", "#364a82", "#9ece6a", "#e0af68", "#f7768e"),
		palette("tokyo-night-light", "#d5d6db", "#e9e9ec", "#343b58", "#565f89", "#9699a3", "#34548a", "#b4b5b9", "#33635c", "#8f5e15", "#8c4351"),
		palette("one-dark", "#282c34", "#21252b", "#abb2bf", "#9da5b4", "#5c6370", "#61afef", "#3e4451", "#98c379", "#e5c07b", "#e06c75"),
		palette("solarized-dark", "#002b36", "#073642", "#93a1a1", "#839496", "#586e75", "#2aa198", "#073642", "#859900", "#b58900", "#dc322f"),
		palette("solarized-light", "#fdf6e3", "#eee8d5", "#586e75", "#657b83", "#93a1a1", "#006c99", "#eee8d5", "#586e00", "#856300", "#c12d2a"),
		palette("ayu-dark", "#10141c", "#0d1017", "#bfbdb6", "#8a91a1", "#414b5e", "#e6b450", "#193155", "#aad94c", "#ffb454", "#d95757"),
		palette("ayu-mirage", "#242936", "#1f2430", "#cccac2", "#949cab", "#49566e", "#ffcc66", "#2b4768", "#d5ff80", "#ffcd66", "#ff6666"),
		palette("ayu-light", "#fcfcfc", "#f8f9fa", "#5c6166", "#677485", "#abb4be", "#a15b00", "#d7e4f6", "#587700", "#986800", "#c83f3f"),
		palette("everforest-dark-hard", "#272e33", "#1e2326", "#d3c6aa", "#9da9a0", "#7a8478", "#a7c080", "#374145", "#83c092", "#dbbc7f", "#e67e80"),
		palette("everforest-dark-medium", "#2d353b", "#232a2e", "#d3c6aa", "#9da9a0", "#7a8478", "#a7c080", "#3d484d", "#83c092", "#dbbc7f", "#e67e80"),
		palette("everforest-dark-soft", "#333c43", "#293136", "#d3c6aa", "#9da9a0", "#7a8478", "#a7c080", "#434f55", "#83c092", "#dbbc7f", "#ed8e8f"),
		palette("everforest-light-hard", "#fffbef", "#f2efdf", "#536169", "#5e6b5c", "#939f91", "#596900", "#f2efdf", "#216b4f", "#7f5b00", "#b73735"),
		palette("everforest-light-medium", "#fdf6e3", "#efebd4", "#536169", "#5e6b5c", "#939f91", "#596900", "#efebd4", "#216b4f", "#7f5b00", "#b73735"),
		palette("everforest-light-soft", "#f3ead3", "#e5dfc5", "#536169", "#576354", "#939f91", "#596900", "#e5dfc5", "#216b4f", "#7f5b00", "#a92f2d"),
	}
	result := map[string]theme{}
	for _, t := range values {
		result[t.Name] = t
	}
	return result
}
func themeNames(themes map[string]theme) []string {
	result := make([]string, 0, len(themes))
	for name := range themes {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}
func (t theme) color(token string) color.Color {
	if t.Colors[token] == "default" {
		return lipgloss.NoColor{}
	}
	return lipgloss.Color(t.Colors[token])
}
func (t theme) style(token string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.color(token))
}
func loadThemes(directory string) (map[string]theme, error) {
	result := builtThemes()
	files, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	custom := map[string]themeFile{}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".toml") {
			continue
		}
		var v themeFile
		_, err := decodeFile(filepath.Join(directory, f.Name()), &v)
		if err != nil {
			return nil, err
		}
		if v.Version != 1 || !validName(v.Name) || f.Name() != v.Name+".toml" {
			return nil, fmt.Errorf("theme %s requires version = 1 and a name matching its filename", f.Name())
		}
		if _, ok := result[v.Name]; ok {
			return nil, fmt.Errorf("built-in theme %s cannot be replaced", v.Name)
		}
		custom[v.Name] = v
	}
	visiting := map[string]bool{}
	var resolve func(string, int) (theme, error)
	resolve = func(name string, depth int) (theme, error) {
		if t, ok := result[name]; ok {
			return t, nil
		}
		v, ok := custom[name]
		if !ok {
			return theme{}, fmt.Errorf("unknown theme %q", name)
		}
		if visiting[name] || depth > 16 {
			return theme{}, fmt.Errorf("theme inheritance cycle or depth exceeded: %s", name)
		}
		visiting[name] = true
		defer delete(visiting, name)
		parent := v.Extends
		if parent == "" {
			parent = "terminal"
		}
		base, err := resolve(parent, depth+1)
		if err != nil {
			return theme{}, err
		}
		t := theme{Name: name, Colors: map[string]string{}}
		for k, v := range base.Colors {
			t.Colors[k] = v
		}
		for key, value := range v.Colors {
			if _, ok := t.Colors[key]; !ok {
				return theme{}, fmt.Errorf("theme %s: unknown color token %s", name, key)
			}
			if !validColor(value) || value == "default" && key != "background" && key != "foreground" && key != "surface" && key != "selection_foreground" {
				return theme{}, fmt.Errorf("theme %s: invalid %s color %q", name, key, value)
			}
			t.Colors[key] = value
		}
		result[name] = t
		return t, nil
	}
	for name := range custom {
		if _, err := resolve(name, 0); err != nil {
			return nil, err
		}
	}
	return result, nil
}
func validColor(value string) bool {
	if value == "default" {
		return true
	}
	if len(value) == 7 && value[0] == '#' {
		for _, r := range value[1:] {
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
		return true
	}
	for i := 0; i < 256; i++ {
		if fmt.Sprint(i) == value {
			return true
		}
	}
	return false
}
