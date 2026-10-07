package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// printer writes human-readable output, with color when enabled.
type printer struct {
	w     io.Writer
	color bool
}

func (p *printer) paint(code, s string) string {
	if !p.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p *printer) green(s string) string  { return p.paint("32", s) }
func (p *printer) red(s string) string    { return p.paint("31", s) }
func (p *printer) yellow(s string) string { return p.paint("33", s) }
func (p *printer) bold(s string) string   { return p.paint("1", s) }
func (p *printer) dim(s string) string    { return p.paint("2", s) }

func (p *printer) ok() string   { return p.green("✓") }
func (p *printer) fail() string { return p.red("✗") }
func (p *printer) warn() string { return p.yellow("!") }

func (p *printer) println(a ...any) { _, _ = fmt.Fprintln(p.w, a...) }

func (p *printer) printf(format string, a ...any) { _, _ = fmt.Fprintf(p.w, format, a...) }

// table prints rows with padded columns. The first row is the header; a
// separator line is drawn under it.
func (p *printer) table(rows [][]string, indent string) {
	if len(rows) == 0 {
		return
	}
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			if n := visibleLen(c); n > widths[i] {
				widths[i] = n
			}
		}
	}
	total := 0
	for i, w := range widths {
		if i < len(widths)-1 {
			widths[i] = w + 4
		}
		total += widths[i]
	}
	for ri, r := range rows {
		var b strings.Builder
		b.WriteString(indent)
		for i, c := range r {
			if ri == 0 {
				b.WriteString(p.bold(c))
			} else {
				b.WriteString(c)
			}
			if i < len(r)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-visibleLen(c)))
			}
		}
		p.println(strings.TrimRight(b.String(), " "))
		if ri == 0 {
			p.println(indent + p.dim(strings.Repeat("─", total)))
		}
	}
}

// visibleLen is the display width of s ignoring ANSI color codes. All text
// in tables is ASCII apart from status symbols, which are one column wide.
func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		default:
			n++
		}
	}
	return n
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
