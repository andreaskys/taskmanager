package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Links: cards, notes and the diary can point at other cards with "#12" (the id shown in the card's
// details), at web pages (http://…) and at attached files or URLs. "o" lists everything that can be
// opened from the current item; a single target opens right away.

var (
	cardRef = regexp.MustCompile(`(?:^|[\s(\[])#(\d+)\b`)
	urlRef  = regexp.MustCompile(`https?://[^\s)>\]]+`)
)

type linkItem struct {
	Label  string
	Kind   string // "card", "file" or "url"
	Target string
	AttID  int64 // set for attachments (they can be removed with x)
}

// opener starts the desktop handler of a file or URL; tests replace it.
var opener = func(target string) error {
	cmd := exec.Command("xdg-open", target)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// cardRefs returns the card ids mentioned as #id in text, in order and without repeats.
func cardRefs(text string) []int64 {
	var out []int64
	seen := map[int64]bool{}
	for _, mt := range cardRef.FindAllStringSubmatch(text, -1) {
		if id, err := strconv.ParseInt(mt[1], 10, 64); err == nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// collectLinks lists what can be opened from an item: its attachments, the cards it mentions and the
// URLs in its text. owner (kind "card"/"note" + id) selects the attachments; kind "" means none.
func (m *Model) collectLinks(text, kind string, id int64) []linkItem {
	var out []linkItem
	seen := map[string]bool{}
	if kind != "" {
		atts, err := m.st.Attachments(kind, id)
		m.fail(err)
		for _, a := range atts {
			k := "file"
			if strings.Contains(a.Target, "://") {
				k = "url"
			}
			seen[a.Target] = true
			out = append(out, linkItem{Label: "📎 " + a.Label, Kind: k, Target: a.Target, AttID: a.ID})
		}
	}
	for _, rid := range cardRefs(text) {
		if kind == "card" && rid == id {
			continue
		}
		if d, ok := m.st.CardByID(rid); ok {
			out = append(out, linkItem{Label: fmt.Sprintf("#%d  %s", d.ID, d.Title), Kind: "card", Target: strconv.FormatInt(rid, 10)})
		}
	}
	for _, u := range urlRef.FindAllString(text, -1) {
		u = strings.TrimRight(u, ".,;:!?")
		if !seen[u] {
			seen[u] = true
			out = append(out, linkItem{Label: "🔗 " + u, Kind: "url", Target: u})
		}
	}
	return out
}

// openLinks opens the only target of an item, or shows the list when there are several.
func (m *Model) openLinks(text, kind string, id int64) {
	items := m.collectLinks(text, kind, id)
	switch len(items) {
	case 0:
		m.notice = "no links here: write #id, a URL, or attach a file (a)"
	case 1:
		m.openLink(items[0])
	default:
		m.links, m.linkIdx = items, 0
		m.linkKind, m.linkOwner, m.linkText = kind, id, text
		m.prevMode = m.mode
		m.mode = modeLinks
	}
}

func (m *Model) openLink(it linkItem) {
	switch it.Kind {
	case "card":
		id, _ := strconv.ParseInt(it.Target, 10, 64)
		d, ok := m.st.CardByID(id)
		if !ok {
			m.err = "card not found"
			return
		}
		if d.ArchivedAt != "" {
			m.fail(m.unarchive(d.ID))
			m.notice = "restored from the archive"
		}
		m.jumpTo(d)
		if cur := m.current(); cur != nil && cur.ID == d.ID {
			m.openDetail()
		}
	default:
		target := it.Target
		if it.Kind == "file" {
			if strings.HasPrefix(target, "~/") {
				if home, err := os.UserHomeDir(); err == nil {
					target = filepath.Join(home, target[2:])
				}
			}
			if _, err := os.Stat(target); err != nil {
				m.err = "file not found: " + it.Target
				return
			}
		}
		if err := opener(target); err != nil {
			m.err = "cannot open: " + err.Error()
			return
		}
		m.notice = "opened " + it.Label
	}
}

func (m *Model) updateLinks(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "o":
		m.mode = m.prevMode
	case "j", "down":
		m.linkIdx = min(m.linkIdx+1, len(m.links)-1)
	case "k", "up":
		m.linkIdx = max(m.linkIdx-1, 0)
	case "enter":
		it := m.links[m.linkIdx]
		m.mode = m.prevMode
		m.openLink(it)
	case "x", "d": // remove an attachment
		it := m.links[m.linkIdx]
		if it.AttID == 0 {
			m.notice = "only attachments can be removed"
			break
		}
		m.fail(m.st.DeleteAttachment(it.AttID))
		m.links = append(m.links[:m.linkIdx], m.links[m.linkIdx+1:]...)
		m.linkIdx = max(0, min(m.linkIdx, len(m.links)-1))
		m.afterAttachChange()
		if len(m.links) == 0 {
			m.mode = m.prevMode
		}
	}
	return m, nil
}

// afterAttachChange refreshes what shows attachment counts.
func (m *Model) afterAttachChange() {
	m.loadDetailExtra()
	if m.notesOpen() || m.prevMode == modeNotes {
		m.loadNotes()
	}
}

// attach asks for a file path or URL and attaches it to a card or note ("label | target" names it).
func (m *Model) attach(kind string, id int64) tea.Cmd {
	return m.ask("Attach file or URL", "", func(s string) error {
		label, target := "", strings.TrimSpace(s)
		if l, t, ok := strings.Cut(s, "|"); ok {
			label, target = strings.TrimSpace(l), strings.TrimSpace(t)
		}
		if _, err := m.st.AddAttachment(kind, id, target, label); err != nil {
			return err
		}
		m.notice = "attached (o opens it)"
		m.afterAttachChange()
		return nil
	}, withHint("path or URL · optional name: Contract | ~/docs/contract.pdf"))
}

func (m *Model) viewLinks(base string) string {
	sw, sh := m.screenSize()
	W := clamp(sw*70/100, 50, 110)
	H := clamp(len(m.links)+2, 4, sh-4)
	rows := H - 2
	start := 0
	if m.linkIdx >= rows {
		start = m.linkIdx - rows + 1
	}
	var lines []string
	for i := start; i < len(m.links) && i < start+rows; i++ {
		bar, st := "  ", lipgloss.NewStyle()
		if i == m.linkIdx {
			bar = barStyle.Render("▌ ")
			st = st.Bold(true).Background(th.SelBg)
		}
		lines = append(lines, st.Render(fit(bar+m.links[i].Label, W-2)))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return m.center(base, drawBox("Open", lines, W, H, th.Accent))
}

// mentions lists where a card is referenced as #id: other cards, notes and diary days.
func (m *Model) mentions(id int64) []string {
	var out []string
	has := func(text string) bool {
		for _, r := range cardRefs(text) {
			if r == id {
				return true
			}
		}
		return false
	}
	if all, err := m.st.AllCards(); err == nil {
		for _, d := range all {
			if d.ID != id && has(d.Description) {
				out = append(out, fmt.Sprintf("card #%d %s", d.ID, d.Title))
			}
		}
	}
	if notes, err := m.st.Notes(); err == nil {
		for _, n := range notes {
			if has(n.Body) {
				out = append(out, "note "+noteLabel(n))
			}
		}
	}
	if days, err := m.st.JournalEntries(); err == nil {
		for _, d := range days {
			if has(d.Body) {
				out = append(out, "diary "+d.Day)
			}
		}
	}
	return out
}
