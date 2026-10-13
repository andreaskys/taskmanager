package tui

import (
	"strings"
)

// suggestFor returns up to 6 entries of list that complete what is being typed.
// With multi, the value is a comma separated list and only the last token is completed.
func suggestFor(value string, list []string, multi bool) []string {
	token := strings.TrimSpace(value)
	var used map[string]bool
	if multi {
		parts := strings.Split(value, ",")
		token = strings.TrimSpace(parts[len(parts)-1])
		used = map[string]bool{}
		for _, p := range parts[:len(parts)-1] {
			used[fold(strings.TrimSpace(p))] = true
		}
	}
	ft := fold(token)
	var prefix, contains []string
	for _, item := range list {
		fi := fold(item)
		if fi == ft || used[fi] {
			continue
		}
		switch {
		case strings.HasPrefix(fi, ft):
			prefix = append(prefix, item)
		case ft != "" && strings.Contains(fi, ft):
			contains = append(contains, item)
		}
	}
	out := append(prefix, contains...)
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

// completeWith returns value with its (last) token replaced by the first suggestion.
// ok is false when there is nothing to complete.
func completeWith(value string, list []string, multi bool) (string, bool) {
	s := suggestFor(value, list, multi)
	if len(s) == 0 {
		return value, false
	}
	if !multi {
		return s[0], true
	}
	i := strings.LastIndex(value, ",")
	head := ""
	if i >= 0 {
		head = strings.TrimRight(value[:i+1], " ") + " "
	}
	return head + s[0] + ", ", true
}

// withSuggestions makes the input window show suggestions and complete them with tab.
func withSuggestions(list []string, multi bool) askOpt {
	return func(m *Model) {
		m.suggestList, m.suggestMulti = list, multi
		m.complete = func(v string) string {
			out, _ := completeWith(v, list, multi)
			return out
		}
	}
}
