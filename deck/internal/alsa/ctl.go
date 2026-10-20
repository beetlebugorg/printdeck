package alsa

import (
	"fmt"
	"unsafe"
)

const (
	ifaceMixer      = 2
	elemTypeInteger = 2
)

// ctlElemID is struct snd_ctl_elem_id.
type ctlElemID struct {
	numid     uint32
	iface     int32
	device    uint32
	subdevice uint32
	name      [44]byte
	index     uint32
}

// ctlElemInfo is struct snd_ctl_elem_info, with the integer part of its value union.
type ctlElemInfo struct {
	id       ctlElemID
	typ      int32
	access   uint32
	count    uint32
	owner    int32
	min      int64
	max      int64
	step     int64
	_        [104]byte
	dimen    [8]byte
	reserved [56]byte
}

// ctlElemValue is struct snd_ctl_elem_value, with the integer part of its value union.
type ctlElemValue struct {
	id       ctlElemID
	indirect uint32
	_        uint32
	value    [128]int64
	reserved [128]byte
}

// Control is an integer mixer control of a card, for example "Digital Playback Volume".
type Control struct {
	card     int
	id       ctlElemID
	Count    int // the number of values (channels)
	Min, Max int
}

// OpenControl finds an integer mixer control of a card by its name.
func OpenControl(card, name string) (*Control, error) {
	n, err := CardIndex(card)
	if err != nil {
		return nil, err
	}
	if len(name) >= 44 {
		return nil, fmt.Errorf("alsa: control name %q is too long", name)
	}
	c := &Control{card: n}
	c.id.iface = ifaceMixer
	copy(c.id.name[:], name)
	var info ctlElemInfo
	info.id = c.id
	if err := c.do(ioctlElemInfo, unsafe.Pointer(&info)); err != nil {
		return nil, fmt.Errorf("alsa: control %q: %w", name, err)
	}
	if info.typ != elemTypeInteger {
		return nil, fmt.Errorf("alsa: control %q is not an integer control", name)
	}
	c.id = info.id
	c.Count, c.Min, c.Max = int(info.count), int(info.min), int(info.max)
	return c, nil
}

func (c *Control) do(req uintptr, arg unsafe.Pointer) error {
	f, err := openDevice(fmt.Sprintf("/dev/snd/controlC%d", c.card))
	if err != nil {
		return err
	}
	defer f.Close()
	return ioctl(f.Fd(), req, arg)
}

// Get returns the value of the first channel.
func (c *Control) Get() (int, error) {
	v := ctlElemValue{id: c.id}
	if err := c.do(ioctlElemRead, unsafe.Pointer(&v)); err != nil {
		return 0, fmt.Errorf("alsa: read control: %w", err)
	}
	return int(v.value[0]), nil
}

// Set sets all channels to a value, limited to Min and Max.
func (c *Control) Set(value int) error {
	value = max(c.Min, min(c.Max, value))
	v := ctlElemValue{id: c.id}
	for i := 0; i < c.Count && i < len(v.value); i++ {
		v.value[i] = int64(value)
	}
	if err := c.do(ioctlElemWrite, unsafe.Pointer(&v)); err != nil {
		return fmt.Errorf("alsa: write control: %w", err)
	}
	return nil
}
