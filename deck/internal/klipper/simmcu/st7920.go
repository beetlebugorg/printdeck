package simmcu

// st7920 is the 128 x 64 display of the simulated printer: the RAM of an ST7920, from the commands
// st7920_send_cmds and st7920_send_data.
//
//   - The text RAM (DDRAM) has 4 rows of 16 characters of 8 x 16 pixels, 2 characters at each
//     address. The addresses of the rows are 0x00, 0x10, 0x08 and 0x18.
//   - The graphics RAM (GDRAM) has 32 rows of 16 words of 16 pixels. Words 0 to 7 of row y are
//     the row y of the display, words 8 to 15 are the row y + 32.
//
// The display shows the text and the graphics together (LCD). The characters of the CGRAM (the
// glyphs of Klipper) show as spaces.
type st7920 struct {
	extended bool
	vertical bool // the next address of the extended mode is the vertical address
	text     bool // the data goes to the DDRAM, else to the GDRAM
	addr     int  // the DDRAM address, in bytes
	y, x     int  // the GDRAM address: the row and the byte in the row
	ddram    [64]byte
	gdram    [32][32]byte
}

func newST7920() *st7920 {
	d := &st7920{text: true}
	for i := range d.ddram {
		d.ddram[i] = ' '
	}
	return d
}

// command runs the bytes of st7920_send_cmds.
func (d *st7920) command(cmds []byte) {
	for _, c := range cmds {
		switch {
		case c&0xe0 == 0x20: // function set: RE (bit 2) is the extended mode
			d.extended, d.vertical = c&0x04 != 0, true
		case c&0x80 != 0 && d.extended: // a GDRAM address: the vertical address first
			if d.vertical {
				d.y = int(c&0x3f) % 32
			} else {
				d.x, d.text = 2*int(c&0x0f), false
			}
			d.vertical = !d.vertical
		case c&0x80 != 0: // a DDRAM address
			d.addr, d.text = 2*int(c&0x1f), true
		case c&0xc0 == 0x40 && !d.extended: // a CGRAM address: the glyphs are not drawn
			d.addr, d.text = -1, true
		case c == 0x01 && !d.extended: // clear
			for i := range d.ddram {
				d.ddram[i] = ' '
			}
			d.addr = 0
		}
	}
}

// data runs the bytes of st7920_send_data.
func (d *st7920) data(b []byte) {
	for _, v := range b {
		if d.text {
			if d.addr >= 0 {
				d.ddram[d.addr%64] = v
				d.addr++
			}
			continue
		}
		d.gdram[d.y][d.x%32] = v
		d.x++
	}
}

// graphics is the picture of the GDRAM: 64 rows of 16 bytes, the high bit at the left.
func (d *st7920) graphics() (c [64][16]byte) {
	for y := 0; y < 32; y++ {
		copy(c[y][:], d.gdram[y][:16])
		copy(c[y+32][:], d.gdram[y][16:])
	}
	return c
}

// rows is the text of the DDRAM: 4 rows of 16 characters, from the top. A character that is not
// printable ASCII is a glyph of the CGRAM.
func (d *st7920) rows() (t [4][16]byte) {
	rows := [4]int{0, 2, 1, 3} // the row of the display of each 16 bytes of the DDRAM
	for i, ch := range d.ddram {
		t[rows[i/16]][i%16] = ch
	}
	return t
}

// LCD is the display of the simulated printer: the picture of its graphics, and its text.
func LCD() (graphics [64][16]byte, text [4][16]byte, ok bool) {
	boardMu.Lock()
	b := current
	boardMu.Unlock()
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, d := range b.lcds {
		return d.graphics(), d.rows(), true
	}
	return
}
