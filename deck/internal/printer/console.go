package printer

import (
	"strings"
	"sync"
)

// ConsoleLine is a line of the G-code console.
type ConsoleLine struct {
	Kind string // "in": a command of the user; "out": a reply of Klipper; "error": a refusal
	Text string
}

// maxConsole is the number of lines that the console keeps.
const maxConsole = 200

// console keeps the last lines of the G-code terminal of Klipper.
type console struct {
	mu    sync.Mutex
	lines []ConsoleLine
}

func (c *console) add(kind, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		k := kind
		if strings.HasPrefix(l, "!! ") {
			k, l = "error", l[3:]
		} else if strings.HasPrefix(l, "// ") {
			l = l[3:]
		}
		c.lines = append(c.lines, ConsoleLine{Kind: k, Text: l})
	}
	if n := len(c.lines); n > maxConsole {
		c.lines = append([]ConsoleLine(nil), c.lines[n-maxConsole:]...)
	}
}

// Console returns the lines of the console, oldest first.
func (c *console) Console() []ConsoleLine {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ConsoleLine(nil), c.lines...)
}
