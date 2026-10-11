package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/colybri/skilus/internal/cli/i18n"
)

// maxWidth keeps long lines readable on wide terminals.
const maxWidth = 100

// style decides how plans are drawn: colors and wrapping only make sense on
// a terminal, so pipes and files get plain, unwrapped lines.
type style struct {
	color bool
	// width is the terminal's width; 0 means do not wrap or shorten.
	width int
	// full shows descriptions whole instead of in two lines.
	full bool
}

// styleFor returns the style for writing to w from cmd, honoring the
// --no-color and --full flags, NO_COLOR and TERM=dumb.
func styleFor(cmd *cobra.Command, w io.Writer) style {
	var st style
	st.full, _ = cmd.Flags().GetBool("full")
	f, ok := w.(*os.File)
	if !ok || !isTerminal(f) {
		return st
	}
	if width, _, err := term.GetSize(int(f.Fd())); err == nil && width > 0 {
		st.width = min(width, maxWidth)
	}
	noColor, _ := cmd.Flags().GetBool("no-color")
	_, noColorEnv := os.LookupEnv("NO_COLOR")
	st.color = !noColor && !noColorEnv && os.Getenv("TERM") != "dumb" && enableColor(f)
	return st
}

// isTerminal reports whether f is a terminal.
func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

func (st style) paint(code, s string) string {
	if !st.color || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (st style) bold(s string) string   { return st.paint("1", s) }
func (st style) dim(s string) string    { return st.paint("2", s) }
func (st style) green(s string) string  { return st.paint("32", s) }
func (st style) yellow(s string) string { return st.paint("33", s) }
func (st style) red(s string) string    { return st.paint("31", s) }

// Status marks, readable without color too.
const (
	markOK    = "✓"
	markWarn  = "!"
	markBlock = "✗"
	markSkill = "●"
	sep       = " · "
)

// line writes text indented by indent spaces, wrapped to the terminal's
// width. maxLines > 0 shortens it to that many lines, ending in "…".
func (st style) line(w io.Writer, indent int, text string, maxLines int) {
	pad := strings.Repeat(" ", indent)
	for _, l := range wrap(text, st.width-indent, maxLines) {
		fmt.Fprintf(w, "%s%s\n", pad, l)
	}
}

// item writes mark and text indented by indent spaces, wrapping text under
// itself. mark takes one column, whatever its color.
func (st style) item(w io.Writer, indent int, mark, text string) {
	st.tagged(w, indent, mark, text, "")
}

// tagged is item with a dimmed tag, such as a rule's code, after text.
func (st style) tagged(w io.Writer, indent int, mark, text, tag string) {
	pad := strings.Repeat(" ", indent)
	if tag != "" {
		text += " " + tag
	}
	lines := wrap(text, st.width-indent-2, 0)
	if last := lines[len(lines)-1]; tag != "" && strings.HasSuffix(last, tag) {
		lines[len(lines)-1] = strings.TrimSuffix(last, tag) + st.dim(tag)
	}
	for i, l := range lines {
		if i == 0 {
			fmt.Fprintf(w, "%s%s %s\n", pad, mark, l)
			continue
		}
		fmt.Fprintf(w, "%s  %s\n", pad, l)
	}
}

// wrap splits text into lines of at most width columns, breaking at spaces
// and inside words that do not fit (as CJK text, which has no spaces).
// width <= 0 returns text as one line.
func wrap(text string, width, maxLines int) []string {
	if width <= 0 {
		return []string{text}
	}
	width = max(width, 20)
	var lines []string
	var cur strings.Builder
	curW := 0
	flush := func() {
		lines = append(lines, cur.String())
		cur.Reset()
		curW = 0
	}
	for _, word := range strings.Fields(text) {
		ww := textWidth(word)
		if curW > 0 && curW+1+ww > width {
			flush()
		}
		if curW > 0 {
			cur.WriteByte(' ')
			curW++
		}
		for _, r := range word {
			rw := runeWidth(r)
			if curW+rw > width {
				flush()
			}
			cur.WriteRune(r)
			curW += rw
		}
	}
	if cur.Len() > 0 || len(lines) == 0 {
		flush()
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
		last := []rune(lines[maxLines-1])
		for len(last) > 0 && textWidth(string(last))+1 > width {
			last = last[:len(last)-1]
		}
		lines[maxLines-1] = strings.TrimRight(string(last), " ") + "…"
	}
	return lines
}

// textWidth is the number of terminal columns s takes.
func textWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

// runeWidth approximates the columns a terminal gives r: two for East Asian
// wide characters, none for combining marks.
func runeWidth(r rune) int {
	switch {
	case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf):
		return 0
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf && r != 0x303f,
		r >= 0xac00 && r <= 0xd7a3, r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f,
		r >= 0xff00 && r <= 0xff60, r >= 0xffe0 && r <= 0xffe6, r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

// size formats n bytes for people: 512 B, 8,8 KiB, 21 KiB, 1,5 MiB.
func size(n int64, t *i18n.Catalog) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, suffix := float64(n)/unit, "KiB"
	if v >= unit {
		v, suffix = v/unit, "MiB"
	}
	s := fmt.Sprintf("%.0f", v)
	if v < 10 {
		s = fmt.Sprintf("%.1f", v)
		if decimalComma(t.Lang()) {
			s = strings.Replace(s, ".", ",", 1)
		}
	}
	return s + " " + suffix
}

// decimalComma reports whether lang writes 8,8 rather than 8.8.
func decimalComma(lang string) bool {
	switch lang {
	case "es", "fr", "de", "pt", "id", "ru", "pl":
		return true
	}
	return false
}

// count formats n with the singular or plural message, as "%d fichero" and
// "%d ficheros". Languages with more forms translate them so both read well.
func count(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf(one, n)
	}
	return fmt.Sprintf(many, n)
}
