package tui

import (
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// Card text is free-form. A line is a checkbox when it is "[ ] text" or "[x] text", optionally
// indented with spaces: indentation makes nested sub-tasks (2 spaces per level).

type lineKind int

const (
	linePlain lineKind = iota
	lineOpen
	lineDone

	indentUnit = 2
	maxIndent  = 6 * indentUnit
)

// parseLine splits a line into its indentation, kind and text (without the "[ ] " marker).
func parseLine(l string) (indent int, kind lineKind, text string) {
	t := strings.TrimLeft(l, " ")
	indent = len(l) - len(t)
	switch {
	case t == "[ ]":
		return indent, lineOpen, ""
	case strings.HasPrefix(t, "[ ] "):
		return indent, lineOpen, t[4:]
	case t == "[x]" || t == "[X]":
		return indent, lineDone, ""
	case strings.HasPrefix(t, "[x] "), strings.HasPrefix(t, "[X] "):
		return indent, lineDone, t[4:]
	}
	return indent, linePlain, t
}

func formatLine(indent int, kind lineKind, text string) string {
	p := strings.Repeat(" ", indent)
	switch kind {
	case lineOpen:
		return p + "[ ] " + text
	case lineDone:
		return p + "[x] " + text
	}
	return p + text
}

// checkProgress counts checkbox lines (nested ones included): (done, total).
func checkProgress(text string) (int, int) {
	done, total := 0, 0
	for _, l := range strings.Split(text, "\n") {
		switch _, k, _ := parseLine(l); k {
		case lineDone:
			done++
			total++
		case lineOpen:
			total++
		}
	}
	return done, total
}

// toggleLines flips the checkbox of lines[i]. Toggling a parent applies the same state to all its
// sub-tasks (the lines below it that are indented deeper). It returns false for plain text lines.
func toggleLines(lines []string, i int) bool {
	if i < 0 || i >= len(lines) {
		return false
	}
	ind, kind, text := parseLine(lines[i])
	if kind == linePlain {
		return false
	}
	to := lineDone
	if kind == lineDone {
		to = lineOpen
	}
	lines[i] = formatLine(ind, to, text)
	for j := i + 1; j < len(lines); j++ {
		cind, ck, ctext := parseLine(lines[j])
		if strings.TrimSpace(lines[j]) != "" && cind <= ind {
			break // left the sub-tree
		}
		if ck != linePlain {
			lines[j] = formatLine(cind, to, ctext)
		}
	}
	return true
}

// --- editing helpers for the textareas (details, notes, new-card notes) ---

func lineAt(a *textarea.Model, row int) string {
	lines := strings.Split(a.Value(), "\n")
	if row < 0 || row >= len(lines) {
		return ""
	}
	return lines[row]
}

// replaceLine rewrites the current logical line and puts the cursor at column col.
func replaceLine(a *textarea.Model, newLine string, col int) {
	a.CursorStart()
	*a, _ = a.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}) // delete to the end of the line
	a.InsertString(newLine)
	a.SetCursorColumn(max(0, min(col, utf8.RuneCountInString(newLine))))
}

// insertCheckbox adds a "[ ] " item below the current line (same indentation), or turns an empty line into one.
func insertCheckbox(a *textarea.Model) {
	l := lineAt(a, a.Line())
	ind, _, _ := parseLine(l)
	if strings.TrimSpace(l) == "" {
		replaceLine(a, strings.Repeat(" ", ind)+"[ ] ", ind+4)
		return
	}
	a.CursorEnd()
	a.InsertString("\n" + strings.Repeat(" ", ind) + "[ ] ")
}

// indentLine moves the current line one level deeper (dir +1) or shallower (dir -1).
// It reports whether anything changed.
func indentLine(a *textarea.Model, dir int) bool {
	l := lineAt(a, a.Line())
	ind, _, _ := parseLine(l)
	n := max(0, min(ind+dir*indentUnit, maxIndent))
	if n == ind {
		return false
	}
	replaceLine(a, strings.Repeat(" ", n)+strings.TrimLeft(l, " "), a.Column()+(n-ind))
	return true
}

// toggleCheckboxAtCursor flips the checkbox on the current line; a plain line becomes a checkbox.
func toggleCheckboxAtCursor(a *textarea.Model) {
	l := lineAt(a, a.Line())
	ind, kind, text := parseLine(l)
	col := a.Column()
	switch kind {
	case lineOpen:
		replaceLine(a, formatLine(ind, lineDone, text), col)
	case lineDone:
		replaceLine(a, formatLine(ind, lineOpen, text), col)
	default:
		replaceLine(a, formatLine(ind, lineOpen, text), col+4)
	}
}

// newLineKeepingIndent inserts a line break that keeps the indentation of the current line.
// On an empty checkbox ("[ ]" with no text) it ends the list instead: the marker is removed.
func newLineKeepingIndent(a *textarea.Model) {
	l := lineAt(a, a.Line())
	ind, kind, text := parseLine(l)
	if kind != linePlain && strings.TrimSpace(text) == "" {
		replaceLine(a, strings.Repeat(" ", ind), ind)
		return
	}
	a.CursorEnd()
	a.InsertString("\n" + strings.Repeat(" ", ind))
}

// handleEditorKey applies the shared list-editing keys to a textarea. It reports whether the key was used.
//
//	shift+enter / alt+enter  new checkbox       alt+x  toggle checkbox
//	tab / shift+tab          indent / outdent   enter  new line keeping the indentation
func handleEditorKey(a *textarea.Model, key string, tabs bool) bool {
	switch key {
	case "shift+enter", "alt+enter":
		insertCheckbox(a)
	case "alt+x":
		toggleCheckboxAtCursor(a)
	case "enter":
		newLineKeepingIndent(a)
	case "tab":
		if !tabs {
			return false
		}
		indentLine(a, 1)
	case "shift+tab":
		if !tabs {
			return false
		}
		indentLine(a, -1)
	default:
		return false
	}
	return true
}
