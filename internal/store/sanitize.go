package store

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text that reaches the database comes from the terminal, imports and the phone API. It is later
// drawn on a terminal, so control characters (ESC starts every terminal escape sequence: set the
// window title, move the cursor, write the clipboard…) and the invisible Unicode controls that spoof
// text direction must never be stored.

// Limits keep one bad request from bloating the database or freezing the UI.
const (
	MaxLine = 500     // title, project, tags, labels
	MaxText = 200_000 // card description, note body, diary entry
)

// CleanLine makes a single-line text safe: control characters and line breaks become spaces.
func CleanLine(s string) string {
	return strings.TrimSpace(clean(s, false))
}

// CleanText makes a multi-line text safe: only "\n" and "\t" survive among the control characters.
func CleanText(s string) string {
	return clean(s, true)
}

func clean(s string, multiline bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range s {
		if r == utf8.RuneError {
			if _, size := utf8.DecodeRuneInString(s[i:]); size == 1 {
				continue // an invalid byte (e.g. a raw 0x9b, the 8-bit CSI): never keep it
			}
		}
		switch {
		case r == '\n' || r == '\t':
			if multiline {
				b.WriteRune(r)
			} else {
				b.WriteByte(' ')
			}
		case r == '\r':
			if !multiline {
				b.WriteByte(' ')
			}
		case unicode.IsControl(r): // C0, DEL and C1 (includes the 8-bit CSI/OSC introducers)
			if !multiline {
				b.WriteByte(' ')
			}
		case r == '؜' || r == '‎' || r == '‏' || (r >= '‪' && r <= '‮') || (r >= '⁦' && r <= '⁩'):
			// bidi controls: they can make text read differently from what it says
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func tooLong(what string, s string, max int) error {
	if len([]rune(s)) > max {
		return &LimitError{What: what, Max: max}
	}
	return nil
}

// LimitError is returned when a text is longer than allowed.
type LimitError struct {
	What string
	Max  int
}

func (e *LimitError) Error() string {
	return e.What + " is too long (max " + itoa(e.Max) + " characters)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	return string(d)
}
