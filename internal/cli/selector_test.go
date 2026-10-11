package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func keys(s string) *bufio.Reader { return bufio.NewReader(strings.NewReader(s)) }

func TestReadKey(t *testing.T) {
	tests := map[string]key{
		"\x1b[A": {kind: keyUp}, "\x1b[B": {kind: keyDown}, "\x1b[C": {kind: keyRight}, "\x1b[D": {kind: keyLeft},
		"\x1bOA": {kind: keyUp}, "\x1b[1;5C": {kind: keyRight}, "\x1b[3~": {kind: keyNone},
		" ": {kind: keySpace}, "\r": {kind: keyEnter}, "\n": {kind: keyEnter},
		"\x7f": {kind: keyBackspace}, "\x08": {kind: keyBackspace},
		"\x03": {kind: keyCancel}, "\x04": {kind: keyCancel}, "\x1b": {kind: keyCancel},
		"p": {kind: keyRune, r: 'p'}, "é": {kind: keyRune, r: 'é'}, "技": {kind: keyRune, r: '技'},
		"\x01": {kind: keyNone},
	}
	for in, want := range tests {
		if got, err := readKey(keys(in)); err != nil || got != want {
			t.Errorf("readKey(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	if _, err := readKey(keys("")); err == nil {
		t.Error("readKey at end of input should fail")
	}
}

func newSelector(labels ...string) *selector {
	s := &selector{title: "¿Qué skills instalo?", hint: "ayuda"}
	for _, l := range labels {
		s.options = append(s.options, option{label: l, detail: "desc " + l, match: l, checked: true})
	}
	return s
}

func checkedLabels(s *selector) string {
	var out []string
	for _, o := range s.options {
		if o.checked {
			out = append(out, o.label)
		}
	}
	return strings.Join(out, ",")
}

func TestSelectorKeys(t *testing.T) {
	s := newSelector("pdf", "docx", "pptx")
	press := func(ks ...key) {
		for _, k := range ks {
			if accepted, cancelled := s.press(k); accepted || cancelled {
				t.Fatalf("%+v ended the selector", k)
			}
		}
	}

	press(key{kind: keyDown}, key{kind: keySpace})
	if got := checkedLabels(s); got != "pdf,pptx" {
		t.Fatalf("after unticking docx: %s", got)
	}
	// Left and right only touch the rows the filter shows.
	press(key{kind: keyRune, r: 'p'})
	if len(s.shown()) != 2 || s.cursor != 0 {
		t.Fatalf("filter p shows %v, cursor %d", s.shown(), s.cursor)
	}
	press(key{kind: keyLeft})
	if got := checkedLabels(s); got != "" {
		t.Fatalf("after left: %q", got)
	}
	press(key{kind: keyRune, r: 'p'}, key{kind: keyRune, r: 't'})
	if len(s.shown()) != 1 {
		t.Fatalf("filter ppt shows %v", s.shown())
	}
	press(key{kind: keyBackspace}, key{kind: keyBackspace}, key{kind: keyBackspace}, key{kind: keyRight})
	if s.query != "" || checkedLabels(s) != "pdf,docx,pptx" {
		t.Fatalf("query %q, checked %s", s.query, checkedLabels(s))
	}
	// The cursor stays inside the list.
	press(key{kind: keyUp}, key{kind: keyUp}, key{kind: keyDown}, key{kind: keyDown}, key{kind: keyDown}, key{kind: keyDown})
	if s.cursor != 2 {
		t.Fatalf("cursor = %d", s.cursor)
	}
	if accepted, _ := s.press(key{kind: keyEnter}); !accepted {
		t.Fatal("enter should accept")
	}
	if _, cancelled := s.press(key{kind: keyCancel}); !cancelled {
		t.Fatal("Esc should cancel")
	}
}

func TestSelectorView(t *testing.T) {
	es := load(t, "es")
	var labels []string
	for i := range 8 {
		labels = append(labels, fmt.Sprintf("skill-%d", i))
	}
	s := newSelector(labels...)
	s.options[0].detail = strings.Repeat("una descripción muy larga ", 10)
	s.footer = func(shown, checked int) string { return fmt.Sprintf("%d mostradas · %d marcadas", shown, checked) }
	s.cursor = 5

	lines := s.view(style{}, es, 40, 3)
	want := []string{
		"¿Qué skills instalo?",
		"ayuda",
		"  ↑ 3 más",
		"  [x] skill-3  desc skill-3",
		"  [x] skill-4  desc skill-4",
		"› [x] skill-5  desc skill-5",
		"  ↓ 2 más",
		"  8 mostradas · 8 marcadas",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("view =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}

	s.cursor, s.top = 0, 0
	for _, l := range s.view(style{}, es, 40, 8) {
		if textWidth(l) > 40 {
			t.Errorf("%q is wider than 40 columns", l)
		}
	}
	s.query = "zzz"
	if got := s.view(style{}, es, 40, 8); got[2] != "Filtro: zzz" || got[3] != "  ninguna opción coincide con el filtro" {
		t.Fatalf("filtered view = %q", got)
	}
}

func TestSelectorLoop(t *testing.T) {
	es := load(t, "es")
	size := func() (int, int) { return 80, 24 }

	s := newSelector("pdf", "docx")
	var out bytes.Buffer
	ok, err := s.loop(keys("\x1b[B \r"), &out, style{}, es, size)
	if err != nil || !ok {
		t.Fatalf("loop = %v, %v", ok, err)
	}
	if got := checkedLabels(s); got != "pdf" {
		t.Fatalf("checked = %s", got)
	}
	// Each redraw goes back to the first line and clears; the last one
	// leaves only the choice.
	if !strings.Contains(out.String(), "\r\x1b[3A\x1b[J") || !strings.HasSuffix(out.String(), "¿Qué skills instalo? pdf\r\n\x1b[?25h") {
		t.Fatalf("output = %q", out.String())
	}

	s = newSelector("pdf")
	out.Reset()
	if ok, err := s.loop(keys("\x03"), &out, style{}, es, size); err != nil || ok {
		t.Fatalf("Ctrl+C: loop = %v, %v", ok, err)
	}
	if !strings.Contains(out.String(), "cancelado") {
		t.Fatalf("output = %q", out.String())
	}
	if _, err := newSelector("pdf").loop(keys(""), &out, style{}, es, size); err == nil {
		t.Fatal("loop should fail when input ends")
	}
}
