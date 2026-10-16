package cmd

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Banner is the DRARIUX ASCII wordmark shown on install and on a bare
// `drariux` invocation. Fixed-width block glyphs keep every column aligned
// in the terminal.
const Banner = `████  ████   ███  ████  █████ █   █ █   █
█   █ █   █ █   █ █   █   █   █   █  █ █
█   █ ████  █████ ████    █   █   █   █
█   █ █ █   █   █ █ █     █   █   █  █ █
████  █  █  █   █ █  █  █████  ███  █   █`

// Subtitle is printed right under the banner.
const Subtitle = "Drariux chain client · EVM 9854 · native DRX"

// PrintBanner writes the wordmark + subtitle to stdout.
func PrintBanner() {
	fmt.Println(Banner)
	fmt.Println(Subtitle)
}

// box draws a titled panel of aligned "label  value" rows, e.g.
//
//	┌ Balance ────────────────┐
//	│ address  0xabc…          │
//	│ balance  5010 DRX        │
//	└──────────────────────────┘
func box(title string, rows [][2]string) {
	lw, vw := 0, 0
	for _, r := range rows {
		v := trunc(r[1], 64)
		if n := utf8.RuneCountInString(r[0]); n > lw {
			lw = n
		}
		if n := utf8.RuneCountInString(v); n > vw {
			vw = n
		}
	}
	inner := lw + 2 + vw // label + gap + value
	top := "┌─ " + title
	if pad := inner + 2 - utf8.RuneCountInString(top); pad > 0 {
		top += " " + strings.Repeat("─", pad)
	} else {
		top = "┌" + strings.Repeat("─", inner+2)
	}
	fmt.Println(top + "┐")
	for _, r := range rows {
		label := padRight(r[0], lw)
		value := padRight(trunc(r[1], 64), vw)
		fmt.Printf("│ %s  %s │\n", label, value)
	}
	fmt.Println("└" + strings.Repeat("─", inner+2) + "┘")
}

// boxTable draws a titled panel with a header row and aligned columns:
//
//	┌ Wallets ──────────────────────┐
//	│ NAME   ADDRESS       DEFAULT  │
//	│ ─────  ────────────  ───────  │
//	│ alice  0xabc…        *        │
//	└───────────────────────────────┘
func boxTable(title string, header []string, rows [][]string) {
	if len(header) == 0 {
		return
	}
	cols := len(header)
	widths := make([]int, cols)
	measure := func(r []string) {
		for i := 0; i < cols && i < len(r); i++ {
			if n := utf8.RuneCountInString(r[i]); n > widths[i] {
				widths[i] = n
			}
		}
	}
	measure(header)
	for _, r := range rows {
		measure(r)
	}
	// inner width = sum(columns) + 2-space gaps between them
	inner := 0
	for i, w := range widths {
		inner += w
		if i < cols-1 {
			inner += 2
		}
	}
	head := "┌─ " + title
	if pad := inner + 2 - utf8.RuneCountInString(head); pad > 0 {
		head += " " + strings.Repeat("─", pad)
	} else {
		head = "┌" + strings.Repeat("─", inner+2)
	}
	fmt.Println(head + "┐")
	fmt.Println("│ " + renderRow(header, widths) + " │")
	segs := make([]string, cols)
	for i, w := range widths {
		segs[i] = strings.Repeat("─", w)
	}
	fmt.Println("│ " + strings.Join(segs, "  ") + " │")
	for _, r := range rows {
		fmt.Println("│ " + renderRow(r, widths) + " │")
	}
	fmt.Println("└" + strings.Repeat("─", inner+2) + "┘")
}

func renderRow(cells []string, widths []int) string {
	cols := len(widths)
	parts := make([]string, cols)
	for i := 0; i < cols; i++ {
		v := ""
		if i < len(cells) {
			v = cells[i]
		}
		parts[i] = padRight(v, widths[i])
	}
	return strings.Join(parts, "  ")
}

func padRight(s string, n int) string {
	if r := utf8.RuneCountInString(s); r >= n {
		return s
	}
	return s + strings.Repeat(" ", n-utf8.RuneCountInString(s))
}

// trunc clips a value to n runes for terminal-friendly panels, appending an
// ellipsis plus the hidden byte count so long payloads (e.g. deployed
// bytecode) never force a panel to full-screen width.
func trunc(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	hidden := len(r) - (n - 1)
	return string(r[:n-1]) + "…" + fmt.Sprintf("(+%d)", hidden)
}
