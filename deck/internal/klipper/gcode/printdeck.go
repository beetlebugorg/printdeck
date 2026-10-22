package gcode

// The command stack: an addition of Printdeck, not part of Klipper. The deck shows the steps of a
// start macro from the commands that run now, for example PRINT_START, then NOZZLE_WIPE in it,
// then a G1 in that.

// pushCommand adds a command line to the stack, and popCommand removes the last one.
func (g *Dispatch) pushCommand(line string) {
	g.stackMu.Lock()
	g.stack = append(g.stack, line)
	g.stackMu.Unlock()
}

func (g *Dispatch) popCommand() {
	g.stackMu.Lock()
	if n := len(g.stack); n > 0 {
		g.stack = g.stack[:n-1]
	}
	g.stackMu.Unlock()
}

// CommandStack returns the command lines that run now: the first is the command of the script,
// and each next one runs inside the one before it (a macro).
func (g *Dispatch) CommandStack() []string {
	g.stackMu.Lock()
	defer g.stackMu.Unlock()
	return append([]string(nil), g.stack...)
}

// The comment handlers: an addition of Printdeck, not part of Klipper. Max mode reads the feature
// type and layer comments of the slicer with them.

// RegisterCommentHandler adds a function that gets the comment of each G-code line: the text after
// the ';'. It runs before the command of the line.
func (g *Dispatch) RegisterCommentHandler(fn func(comment string)) {
	g.commentHandlers = append(g.commentHandlers, fn)
}

func (g *Dispatch) comment(c string) {
	for _, fn := range g.commentHandlers {
		fn(c)
	}
}
