package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"golang.org/x/term"

	"github.com/colybri/skilus/internal/cli/i18n"
)

// option is one row of a selector.
type option struct {
	label   string // shown in bold
	detail  string // dimmed after the label
	match   string // what the filter looks in, lower case
	checked bool
}

// selector is a multiple choice list driven by the keyboard: arrows move,
// space ticks, right ticks every row shown and left none, typing filters
// and enter accepts.
type selector struct {
	title   string
	hint    string
	options []option
	// footer describes the list below it; it gets the rows that match and
	// the number of ticked options.
	footer func(shown, checked int) string

	query  string
	cursor int // position in shown()
	top    int // first row of shown() on screen
}

// keyKind is a key the selector understands.
type keyKind int

const (
	keyNone keyKind = iota
	keyUp
	keyDown
	keyRight
	keyLeft
	keySpace
	keyEnter
	keyBackspace
	keyCancel
	keyRune
)

type key struct {
	kind keyKind
	r    rune
}

// readKey reads one key from a terminal in raw mode. Escape sequences
// other than the arrows are ignored.
func readKey(r *bufio.Reader) (key, error) {
	c, _, err := r.ReadRune()
	if err != nil {
		return key{}, err
	}
	switch c {
	case '\r', '\n':
		return key{kind: keyEnter}, nil
	case ' ':
		return key{kind: keySpace}, nil
	case 0x7f, 0x08:
		return key{kind: keyBackspace}, nil
	case 0x03, 0x04:
		return key{kind: keyCancel}, nil
	case 0x1b:
		if r.Buffered() == 0 {
			return key{kind: keyCancel}, nil // a lone Esc
		}
		next, _, err := r.ReadRune()
		if err != nil {
			return key{}, err
		}
		if next != '[' && next != 'O' {
			return key{kind: keyNone}, nil
		}
		// Read up to the final byte of the sequence (a letter or ~).
		for {
			b, _, err := r.ReadRune()
			if err != nil {
				return key{}, err
			}
			if b >= '0' && b <= '9' || b == ';' {
				continue
			}
			switch b {
			case 'A':
				return key{kind: keyUp}, nil
			case 'B':
				return key{kind: keyDown}, nil
			case 'C':
				return key{kind: keyRight}, nil
			case 'D':
				return key{kind: keyLeft}, nil
			}
			return key{kind: keyNone}, nil
		}
	}
	if unicode.IsPrint(c) {
		return key{kind: keyRune, r: c}, nil
	}
	return key{kind: keyNone}, nil
}

// shown returns the indexes of the options that match the filter.
func (s *selector) shown() []int {
	q := strings.ToLower(s.query)
	var out []int
	for i, o := range s.options {
		if q == "" || strings.Contains(o.match, q) {
			out = append(out, i)
		}
	}
	return out
}

func (s *selector) checked() int {
	n := 0
	for _, o := range s.options {
		if o.checked {
			n++
		}
	}
	return n
}

// press applies k and reports whether the user accepted or cancelled.
func (s *selector) press(k key) (accepted, cancelled bool) {
	shown := s.shown()
	switch k.kind {
	case keyUp:
		if s.cursor > 0 {
			s.cursor--
		}
	case keyDown:
		if s.cursor < len(shown)-1 {
			s.cursor++
		}
	case keySpace:
		if len(shown) > 0 {
			o := &s.options[shown[s.cursor]]
			o.checked = !o.checked
		}
	case keyRight, keyLeft:
		for _, i := range shown {
			s.options[i].checked = k.kind == keyRight
		}
	case keyBackspace:
		if r := []rune(s.query); len(r) > 0 {
			s.query = string(r[:len(r)-1])
			s.cursor, s.top = 0, 0
		}
	case keyRune:
		s.query += string(k.r)
		s.cursor, s.top = 0, 0
	case keyEnter:
		return true, false
	case keyCancel:
		return false, true
	}
	return false, false
}

// view returns the lines to draw, none wider than width, showing at most
// rows options around the cursor.
func (s *selector) view(st style, t *i18n.Catalog, width, rows int) []string {
	lines := []string{st.bold(truncate(s.title, width)), fit(st.dim(s.hint), s.hint, width)}
	if s.query != "" {
		lines = append(lines, st.dim(t.T("Filtro:"))+" "+st.bold(clean(s.query)))
	}
	shown := s.shown()
	if s.cursor < s.top {
		s.top = s.cursor
	}
	if s.cursor >= s.top+rows {
		s.top = s.cursor - rows + 1
	}
	labelWidth := 0
	for _, i := range shown {
		labelWidth = max(labelWidth, textWidth(s.options[i].label))
	}
	if s.top > 0 {
		lines = append(lines, st.dim("  "+t.T("↑ %d más", s.top)))
	}
	end := min(len(shown), s.top+rows)
	for row := s.top; row < end; row++ {
		o := s.options[shown[row]]
		pointer, box := "  ", "[ ] "
		if row == s.cursor {
			pointer = st.paint("36", "›") + " "
		}
		if o.checked {
			box = st.green("[x]") + " "
		}
		label := o.label + strings.Repeat(" ", labelWidth-textWidth(o.label))
		room := width - 6 - labelWidth - 2
		detail := ""
		if o.detail != "" && room > 3 {
			detail = "  " + st.dim(truncate(o.detail, room))
		}
		lines = append(lines, pointer+box+st.bold(truncate(label, width-6))+detail)
	}
	if end < len(shown) {
		lines = append(lines, st.dim("  "+t.T("↓ %d más", len(shown)-end)))
	}
	if len(shown) == 0 {
		lines = append(lines, st.dim("  "+t.T("ninguna opción coincide con el filtro")))
	}
	if s.footer != nil {
		footer := s.footer(len(shown), s.checked())
		lines = append(lines, fit(st.dim("  "+footer), "  "+footer, width))
	}
	return lines
}

// fit returns styled when its plain text fits in width, or plain shortened.
func fit(styled, plain string, width int) string {
	if textWidth(plain) <= width {
		return styled
	}
	return truncate(plain, width)
}

// truncate shortens s to width columns, ending in "…" when it is cut.
func truncate(s string, width int) string {
	if textWidth(s) <= width {
		return s
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := runeWidth(r)
		if w+rw > width-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// run shows the selector on the terminal until the user accepts or
// cancels, and leaves a line with the choice. It reports false when the
// user cancelled.
func (s *selector) run(in, out *os.File, st style, t *i18n.Catalog) (bool, error) {
	old, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return false, err
	}
	defer term.Restore(int(in.Fd()), old) //nolint:errcheck // nothing to do if the terminal is gone
	size := func() (int, int) {
		w, h, err := term.GetSize(int(out.Fd()))
		if err != nil || w <= 0 {
			return 80, 24
		}
		return w, h
	}
	return s.loop(bufio.NewReader(in), out, st, t, size)
}

// loop draws the selector on out and reads keys from in; size returns the
// terminal's width and height.
func (s *selector) loop(in *bufio.Reader, out io.Writer, st style, t *i18n.Catalog, size func() (int, int)) (bool, error) {
	drawn := 0
	draw := func(lines []string) {
		var b strings.Builder
		if drawn > 0 {
			b.WriteString("\r")
			if drawn > 1 {
				fmt.Fprintf(&b, "\x1b[%dA", drawn-1)
			}
			b.WriteString("\x1b[J")
		}
		b.WriteString(strings.Join(lines, "\r\n"))
		fmt.Fprint(out, b.String())
		drawn = len(lines)
	}
	fmt.Fprint(out, "\x1b[?25l")
	defer fmt.Fprint(out, "\x1b[?25h")
	for {
		w, h := size()
		draw(s.view(st, t, w-1, min(12, max(3, h-6))))
		k, err := readKey(in)
		if err != nil {
			return false, err
		}
		accepted, cancelled := s.press(k)
		if !accepted && !cancelled {
			continue
		}
		var names []string
		for _, o := range s.options {
			if o.checked && accepted {
				names = append(names, o.label)
			}
		}
		summary := st.bold(s.title) + " " + strings.Join(names, ", ")
		if cancelled {
			summary = st.bold(s.title) + " " + st.dim(t.T("cancelado"))
		}
		draw([]string{fit(summary, s.title+" "+strings.Join(names, ", "), w-1)})
		fmt.Fprint(out, "\r\n")
		return accepted, nil
	}
}
