package printer

// LCDScreen is a screen of the display of the printer, for the module printdeck_display of the
// Klipper host: up to 4 rows, or a title, a caption and a full screen animation. Its JSON is the
// screen of the endpoint printdeck/screen.
type LCDScreen struct {
	Rows    []LCDRow `json:"rows,omitempty"`
	Anim    string   `json:"anim,omitempty"` // "work", "heat", "in", "out", or ""
	Title   string   `json:"title,omitempty"`
	Caption string   `json:"caption,omitempty"`
	Font    string   `json:"font,omitempty"` // "5x8", "6x10", or "" for 8 x 16
	// Status is the status screen, drawn with icons. With a status, the rows are not used.
	Status *LCDStatus `json:"status,omitempty"`
}

// LCDStatus is the status screen of the display.
type LCDStatus struct {
	State    string  `json:"state"`
	Clock    string  `json:"clock,omitempty"`
	Message  string  `json:"message,omitempty"`
	Tool     float64 `json:"tool"`
	ToolT    float64 `json:"tool_target"`
	Bed      float64 `json:"bed"`
	BedT     float64 `json:"bed_target"`
	Fan      float64 `json:"fan"`      // 0 to 1
	Progress float64 `json:"progress"` // 0 to 1
	Printing bool    `json:"printing,omitempty"`
	Elapsed  string  `json:"elapsed,omitempty"`  // the time of the print
	Left     string  `json:"left,omitempty"`     // the time left of the print
	Position string  `json:"position,omitempty"` // when no print runs
	Hint     string  `json:"hint,omitempty"`     // the bottom line when no print runs
}

// LCDRow is a row: text at the left and at the right, or a progress bar from 0 to 1. An Item is a
// key of a menu, with the cursor at the Selected item. A Bold row is a heading.
type LCDRow struct {
	Left     string  `json:"left,omitempty"`
	Right    string  `json:"right,omitempty"`
	Item     bool    `json:"item,omitempty"`
	Selected bool    `json:"selected,omitempty"`
	Bold     bool    `json:"bold,omitempty"`
	Bar      float64 `json:"bar,omitempty"`
	HasBar   bool    `json:"has_bar,omitempty"`
}
