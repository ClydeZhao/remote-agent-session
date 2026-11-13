package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"golang.org/x/term"
)

func Terminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u202e' || r == '\u202d' {
			return -1
		}
		return r
	}, s)
}

func (p Printer) Status(w io.Writer, status, message string) {
	symbol, color := "○", "33"
	switch status {
	case "pass":
		symbol, color = "✓", "32"
	case "failed":
		symbol, color = "✗", "31"
	case "running":
		symbol, color = "…", "36"
	}
	label := symbol + " " + Clean(message)
	_, disabled := os.LookupEnv("NO_COLOR")
	if !p.JSON && Terminal(w) && !disabled && os.Getenv("TERM") != "dumb" {
		label = "\x1b[" + color + "m" + label + "\x1b[0m"
	}
	fmt.Fprintln(w, "  "+label)
}
