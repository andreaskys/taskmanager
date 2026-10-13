package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Theme is the full color palette of the UI. Add a theme by adding an entry to themes.
type Theme struct {
	Name     string
	BG, FG   color.Color // terminal background/foreground; nil keeps the terminal's own
	Accent   color.Color // titles, focus borders, keys
	Dim      color.Color // secondary text
	Rule     color.Color // borders and separators
	Err      color.Color
	Warn     color.Color
	Ok       color.Color
	Info     color.Color
	Purple   color.Color
	Bright   color.Color // selected text
	SelBg    color.Color // selected row background
	ChipBg   color.Color
	Prio     [4]color.Color // none, low, medium, high
	Projects []color.Color  // palette for project labels
}

func c(s string) color.Color { return lipgloss.Color(s) }

var themes = []Theme{
	{
		Name:   "default",
		Accent: c("39"), Dim: c("244"), Rule: c("240"), Err: c("196"), Warn: c("214"), Ok: c("42"),
		Info: c("81"), Purple: c("141"), Bright: c("255"), SelBg: c("236"), ChipBg: c("238"),
		Prio:     [4]color.Color{c("240"), c("34"), c("214"), c("196")},
		Projects: []color.Color{c("39"), c("141"), c("214"), c("42"), c("204"), c("81"), c("178")},
	},
	{
		Name: "monokai",
		BG:   c("#272822"), FG: c("#f8f8f2"),
		Accent: c("#66d9ef"), Dim: c("#75715e"), Rule: c("#49483e"), Err: c("#f92672"), Warn: c("#fd971f"), Ok: c("#a6e22e"),
		Info: c("#66d9ef"), Purple: c("#ae81ff"), Bright: c("#f8f8f2"), SelBg: c("#3e3d32"), ChipBg: c("#3e3d32"),
		Prio:     [4]color.Color{c("#49483e"), c("#a6e22e"), c("#e6db74"), c("#f92672")},
		Projects: []color.Color{c("#66d9ef"), c("#ae81ff"), c("#fd971f"), c("#a6e22e"), c("#f92672"), c("#e6db74")},
	},
	{
		Name: "gruvbox",
		BG:   c("#282828"), FG: c("#ebdbb2"),
		Accent: c("#83a598"), Dim: c("#928374"), Rule: c("#504945"), Err: c("#fb4934"), Warn: c("#fe8019"), Ok: c("#b8bb26"),
		Info: c("#8ec07c"), Purple: c("#d3869b"), Bright: c("#fbf1c7"), SelBg: c("#3c3836"), ChipBg: c("#3c3836"),
		Prio:     [4]color.Color{c("#504945"), c("#b8bb26"), c("#fabd2f"), c("#fb4934")},
		Projects: []color.Color{c("#83a598"), c("#d3869b"), c("#fe8019"), c("#b8bb26"), c("#fb4934"), c("#fabd2f"), c("#8ec07c")},
	},
	{
		Name: "nord",
		BG:   c("#2e3440"), FG: c("#d8dee9"),
		Accent: c("#88c0d0"), Dim: c("#616e88"), Rule: c("#434c5e"), Err: c("#bf616a"), Warn: c("#d08770"), Ok: c("#a3be8c"),
		Info: c("#81a1c1"), Purple: c("#b48ead"), Bright: c("#eceff4"), SelBg: c("#3b4252"), ChipBg: c("#3b4252"),
		Prio:     [4]color.Color{c("#434c5e"), c("#a3be8c"), c("#ebcb8b"), c("#bf616a")},
		Projects: []color.Color{c("#88c0d0"), c("#b48ead"), c("#d08770"), c("#a3be8c"), c("#bf616a"), c("#ebcb8b"), c("#81a1c1")},
	},
	{
		Name: "tokyonight",
		BG:   c("#1a1b26"), FG: c("#c0caf5"),
		Accent: c("#7aa2f7"), Dim: c("#565f89"), Rule: c("#3b4261"), Err: c("#f7768e"), Warn: c("#ff9e64"), Ok: c("#9ece6a"),
		Info: c("#7dcfff"), Purple: c("#bb9af7"), Bright: c("#e0e6ff"), SelBg: c("#292e42"), ChipBg: c("#292e42"),
		Prio:     [4]color.Color{c("#3b4261"), c("#9ece6a"), c("#e0af68"), c("#f7768e")},
		Projects: []color.Color{c("#7aa2f7"), c("#bb9af7"), c("#ff9e64"), c("#9ece6a"), c("#f7768e"), c("#e0af68"), c("#7dcfff")},
	},
	{
		// for terminals with a light background: dark ink, soft grays
		Name:   "light",
		Accent: c("25"), Dim: c("243"), Rule: c("250"), Err: c("160"), Warn: c("130"), Ok: c("28"),
		Info: c("31"), Purple: c("91"), Bright: c("16"), SelBg: c("254"), ChipBg: c("253"),
		Prio:     [4]color.Color{c("250"), c("28"), c("130"), c("160")},
		Projects: []color.Color{c("25"), c("91"), c("130"), c("28"), c("161"), c("31"), c("94")},
	},
	{
		Name: "catppuccin",
		BG:   c("#1e1e2e"), FG: c("#cdd6f4"),
		Accent: c("#89b4fa"), Dim: c("#7f849c"), Rule: c("#45475a"), Err: c("#f38ba8"), Warn: c("#fab387"), Ok: c("#a6e3a1"),
		Info: c("#89dceb"), Purple: c("#cba6f7"), Bright: c("#f5f5ff"), SelBg: c("#313244"), ChipBg: c("#313244"),
		Prio:     [4]color.Color{c("#45475a"), c("#a6e3a1"), c("#f9e2af"), c("#f38ba8")},
		Projects: []color.Color{c("#89b4fa"), c("#cba6f7"), c("#fab387"), c("#a6e3a1"), c("#f5c2e7"), c("#f9e2af"), c("#94e2d5")},
	},
	{
		// accessibility: the terminal's 16 bright colors, strong contrast, a solid selection bar
		Name:   "contrast",
		Accent: c("14"), Dim: c("7"), Rule: c("7"), Err: c("9"), Warn: c("11"), Ok: c("10"),
		Info: c("14"), Purple: c("13"), Bright: c("15"), SelBg: c("4"), ChipBg: c("0"),
		Prio:     [4]color.Color{c("7"), c("10"), c("11"), c("9")},
		Projects: []color.Color{c("14"), c("13"), c("11"), c("10"), c("9"), c("12")},
	},
	{
		Name: "dracula",
		BG:   c("#282a36"), FG: c("#f8f8f2"),
		Accent: c("#8be9fd"), Dim: c("#6272a4"), Rule: c("#44475a"), Err: c("#ff5555"), Warn: c("#ffb86c"), Ok: c("#50fa7b"),
		Info: c("#8be9fd"), Purple: c("#bd93f9"), Bright: c("#f8f8f2"), SelBg: c("#44475a"), ChipBg: c("#44475a"),
		Prio:     [4]color.Color{c("#44475a"), c("#50fa7b"), c("#f1fa8c"), c("#ff5555")},
		Projects: []color.Color{c("#8be9fd"), c("#bd93f9"), c("#ffb86c"), c("#50fa7b"), c("#ff79c6"), c("#f1fa8c")},
	},
}

var th = themes[0]

func init() { applyTheme("default") }

// themeNames lists the available theme names.
func themeNames() []string {
	var out []string
	for _, t := range themes {
		out = append(out, t.Name)
	}
	return out
}

// applyTheme switches the active theme (unknown names fall back to default) and rebuilds every style.
func applyTheme(name string) string {
	th = themes[0]
	for _, t := range themes {
		if strings.EqualFold(t.Name, name) {
			th = t
		}
	}
	st := lipgloss.NewStyle
	titleStyle = st().Bold(true).Foreground(th.Accent)
	dimStyle = st().Foreground(th.Dim)
	errStyle = st().Foreground(th.Err)
	selStyle = st().Bold(true).Foreground(th.Bright).Background(th.SelBg)
	colStyle = st().Border(lipgloss.RoundedBorder()).BorderForeground(th.Rule).Padding(0, 1)
	colSelStyle = colStyle.BorderForeground(th.Accent)
	colDropSty = colStyle.BorderForeground(th.Warn)
	panelStyle = st().Border(lipgloss.RoundedBorder()).BorderForeground(th.Accent).Padding(0, 1)
	keyStyle = st().Bold(true).Foreground(th.Accent)
	ruleStyle = st().Foreground(th.Rule)
	barStyle = st().Bold(true).Foreground(th.Accent)
	chipStyle = st().Foreground(th.Bright).Background(th.ChipBg).Padding(0, 1)
	noticeStyle = st().Foreground(th.Warn)
	return th.Name
}

// nextTheme cycles to the following theme and returns its name.
func nextTheme() string {
	for i, t := range themes {
		if t.Name == th.Name {
			return applyTheme(themes[(i+1)%len(themes)].Name)
		}
	}
	return applyTheme(themes[0].Name)
}
