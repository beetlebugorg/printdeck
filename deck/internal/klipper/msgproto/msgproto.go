// Package msgproto is the message protocol between the Klipper host and the firmware of the printer's
// board: the data dictionary of the firmware, the encoding of commands and responses, and the
// framing of the messages.
//
// Ported to Go from klippy/msgproto.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2016-2024  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package msgproto

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// The framing of a message block: length, sequence, payload, CRC16 and a sync byte.
const (
	MessageMin         = 5
	MessageMax         = 64
	MessageHeaderSize  = 2
	MessageTrailerSize = 3
	MessagePosLen      = 0
	MessagePosSeq      = 1
	MessageTrailerCRC  = 3
	MessageTrailerSync = 1
	MessagePayloadMax  = MessageMax - MessageMin
	MessageSeqMask     = 0x0f
	MessageDest        = 0x10
	MessageSync        = 0x7e
)

// defaultMessages are known before the data dictionary is read.
var defaultMessages = map[string]int{
	"identify_response offset=%u data=%.*s": 0,
	"identify offset=%u count=%c":           1,
}

// Error is an error of the protocol.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

func errorf(format string, args ...any) error { return &Error{fmt.Sprintf(format, args...)} }

// CRC16CCITT is the checksum of a message block.
func CRC16CCITT(buf []byte) [2]byte {
	crc := uint16(0xffff)
	for _, b := range buf {
		d := b ^ byte(crc&0xff)
		d ^= (d & 0x0f) << 4
		crc = (uint16(d)<<8 | crc>>8) ^ uint16(d>>4) ^ uint16(d)<<3
	}
	return [2]byte{byte(crc >> 8), byte(crc & 0xff)}
}

// A Value of a message parameter is an int64 (integers and enumerations by number), a string (an
// enumeration by name), or a []byte (a buffer or string).
type Value any

// paramType is the encoding of one parameter type.
type paramType interface {
	encode(out []byte, v Value) ([]byte, error)
	parse(s []byte, pos int) (Value, int, error)
	isInt() bool
	isDynamicString() bool
	maxLength() int
}

// ptInt is a variable-length integer: %u, %i, %hu, %hi and %c.
type ptInt struct {
	signed bool
	maxLen int
}

func (t ptInt) isInt() bool           { return true }
func (t ptInt) isDynamicString() bool { return false }
func (t ptInt) maxLength() int        { return t.maxLen }

func encodeVLQ(out []byte, v int64) []byte {
	if v >= 0xc000000 || v < -0x4000000 {
		out = append(out, byte((v>>28)&0x7f|0x80))
	}
	if v >= 0x180000 || v < -0x80000 {
		out = append(out, byte((v>>21)&0x7f|0x80))
	}
	if v >= 0x3000 || v < -0x1000 {
		out = append(out, byte((v>>14)&0x7f|0x80))
	}
	if v >= 0x60 || v < -0x20 {
		out = append(out, byte((v>>7)&0x7f|0x80))
	}
	return append(out, byte(v&0x7f))
}

func (t ptInt) encode(out []byte, v Value) ([]byte, error) {
	n, ok := v.(int64)
	if !ok {
		return out, errorf("integer parameter has a value of type %T", v)
	}
	return encodeVLQ(out, n), nil
}

func parseVLQ(s []byte, pos int, signed bool) (int64, int, error) {
	if pos >= len(s) {
		return 0, pos, errorf("message ends inside a parameter")
	}
	c := s[pos]
	pos++
	v := int64(c & 0x7f)
	if c&0x60 == 0x60 {
		v |= -0x20
	}
	for c&0x80 != 0 {
		if pos >= len(s) {
			return 0, pos, errorf("message ends inside a parameter")
		}
		c = s[pos]
		pos++
		v = v<<7 | int64(c&0x7f)
	}
	if !signed {
		v = int64(uint32(v))
	}
	return v, pos, nil
}

func (t ptInt) parse(s []byte, pos int) (Value, int, error) {
	v, pos, err := parseVLQ(s, pos, t.signed)
	return v, pos, err
}

// ptString is a buffer with a length byte: %s, %.*s and %*s.
type ptString struct{}

func (ptString) isInt() bool           { return false }
func (ptString) isDynamicString() bool { return true }
func (ptString) maxLength() int        { return 64 }

func (ptString) encode(out []byte, v Value) ([]byte, error) {
	b, ok := v.([]byte)
	if !ok {
		return out, errorf("buffer parameter has a value of type %T", v)
	}
	out = append(out, byte(len(b)))
	return append(out, b...), nil
}

func (ptString) parse(s []byte, pos int) (Value, int, error) {
	if pos >= len(s) {
		return nil, pos, errorf("message ends inside a parameter")
	}
	l := int(s[pos])
	if pos+1+l > len(s) {
		return nil, pos, errorf("message ends inside a buffer")
	}
	return append([]byte(nil), s[pos+1:pos+1+l]...), pos + 1 + l, nil
}

var messageTypes = map[string]paramType{
	"%u": ptInt{false, 5}, "%i": ptInt{true, 5},
	"%hu": ptInt{false, 3}, "%hi": ptInt{true, 3},
	"%c": ptInt{false, 2},
	"%s": ptString{}, "%.*s": ptString{}, "%*s": ptString{},
}

// enumeration is a parameter whose integers have names, for example the pins.
type enumeration struct {
	pt      paramType
	name    string
	enums   map[string]int64
	reverse map[int64]string
}

func (e *enumeration) isInt() bool           { return false }
func (e *enumeration) isDynamicString() bool { return false }
func (e *enumeration) maxLength() int        { return e.pt.maxLength() }

func (e *enumeration) encode(out []byte, v Value) ([]byte, error) {
	name, ok := v.(string)
	if !ok {
		return out, errorf("enumeration '%s' has a value of type %T", e.name, v)
	}
	tv, ok := e.enums[name]
	if !ok {
		return out, &EnumerationError{Enum: e.name, Value: name}
	}
	return e.pt.encode(out, tv)
}

func (e *enumeration) parse(s []byte, pos int) (Value, int, error) {
	v, pos, err := e.pt.parse(s, pos)
	if err != nil {
		return nil, pos, err
	}
	if name, ok := e.reverse[v.(int64)]; ok {
		return name, pos, nil
	}
	return fmt.Sprintf("?%d", v.(int64)), pos, nil
}

// EnumerationError is the error for a name that the enumeration does not have, for example a pin
// that the board does not have.
type EnumerationError struct{ Enum, Value string }

func (e *EnumerationError) Error() string {
	return fmt.Sprintf("Unknown value '%s' in enumeration '%s'", e.Value, e.Enum)
}

type param struct {
	name string
	pt   paramType
}

// lookupParams makes the parameter types of a format, for example "config_spi oid=%c pin=%u".
// A parameter whose name is an enumeration, or ends with "_" and its name, uses the enumeration.
func lookupParams(msgformat string, enumerations map[string]*enumeration) ([]param, error) {
	fields := strings.Fields(msgformat)
	var out []param
	for _, arg := range fields[1:] {
		name, fmtStr, ok := strings.Cut(arg, "=")
		if !ok {
			return nil, errorf("bad parameter '%s' in '%s'", arg, msgformat)
		}
		pt, ok := messageTypes[fmtStr]
		if !ok {
			return nil, errorf("unknown parameter type '%s' in '%s'", fmtStr, msgformat)
		}
		// The enumerations in a fixed order, so the match does not depend on the map order.
		for _, en := range sortedEnums(enumerations) {
			if name == en.name || strings.HasSuffix(name, "_"+en.name) {
				pt = &enumeration{pt: pt, name: en.name, enums: en.enums, reverse: en.reverse}
				break
			}
		}
		out = append(out, param{name, pt})
	}
	return out, nil
}

func sortedEnums(m map[string]*enumeration) []*enumeration {
	out := make([]*enumeration, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// lookupOutputParams makes the parameter types of a debug "output()" format.
func lookupOutputParams(msgformat string) ([]paramType, error) {
	var out []paramType
	args := msgformat
	for {
		pos := strings.IndexByte(args, '%')
		if pos < 0 {
			break
		}
		if pos+1 >= len(args) || args[pos+1] != '%' {
			found := false
			for i := 0; i < 4 && pos+1+i <= len(args); i++ {
				if t, ok := messageTypes[args[pos:pos+1+i]]; ok {
					out = append(out, t)
					found = true
					break
				}
			}
			if !found {
				return nil, errorf("Invalid output format for '%s'", msgformat)
			}
		}
		args = args[pos+1:]
	}
	return out, nil
}

// debugFormat replaces the parameter formats with %s, as convert_msg_format of msgproto.py.
func debugFormat(msgformat string) string {
	for _, c := range []string{"%u", "%i", "%hu", "%hi", "%c", "%.*s", "%*s"} {
		msgformat = strings.ReplaceAll(msgformat, c, "%s")
	}
	return msgformat
}

// pyPercent formats like the % operator of Python with only %s fields and %% (the debug formats).
func pyPercent(format string, args []string) string {
	var b strings.Builder
	i := 0
	for p := 0; p < len(format); p++ {
		if format[p] == '%' && p+1 < len(format) {
			switch format[p+1] {
			case 's':
				if i < len(args) {
					b.WriteString(args[i])
				}
				i++
				p++
				continue
			case '%':
				b.WriteByte('%')
				p++
				continue
			}
		}
		b.WriteByte(format[p])
	}
	return b.String()
}

// formatValue writes a parameter value as msgproto.py prints it: integers in decimal, names as
// they are, and buffers with Python's repr of bytes.
func formatValue(v Value, dynamic bool) string {
	switch x := v.(type) {
	case int64:
		return strconv.FormatInt(x, 10)
	case string:
		return x
	case []byte:
		if dynamic {
			return PyBytesRepr(x)
		}
		return string(x)
	}
	return fmt.Sprint(v)
}

// PyBytesRepr writes bytes as Python's repr of a bytes object, for example b'ab\x00'.
func PyBytesRepr(b []byte) string {
	quote := byte('\'')
	if bytes.IndexByte(b, '\'') >= 0 && bytes.IndexByte(b, '"') < 0 {
		quote = '"'
	}
	var s strings.Builder
	s.WriteString("b")
	s.WriteByte(quote)
	for _, c := range b {
		switch {
		case c == quote || c == '\\':
			s.WriteByte('\\')
			s.WriteByte(c)
		case c == '\t':
			s.WriteString(`\t`)
		case c == '\n':
			s.WriteString(`\n`)
		case c == '\r':
			s.WriteString(`\r`)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&s, `\x%02x`, c)
		default:
			s.WriteByte(c)
		}
	}
	s.WriteByte(quote)
	return s.String()
}

// Format is a format of a message, by which the parser decodes it.
type Format interface {
	Name() string
	parse(s []byte, pos int) (map[string]Value, int, error)
	FormatParams(params map[string]Value) string
}

// MessageFormat is a command or a response, for example "get_clock" or "clock clock=%u".
type MessageFormat struct {
	MsgIDBytes []byte
	MsgFormat  string
	debug      string
	name       string
	params     []param
	byName     map[string]paramType
}

func newMessageFormat(idBytes []byte, msgformat string, enums map[string]*enumeration) (*MessageFormat, error) {
	params, err := lookupParams(msgformat, enums)
	if err != nil {
		return nil, err
	}
	m := &MessageFormat{MsgIDBytes: idBytes, MsgFormat: msgformat, debug: debugFormat(msgformat),
		name: strings.Fields(msgformat)[0], params: params, byName: map[string]paramType{}}
	for _, p := range params {
		m.byName[p.name] = p.pt
	}
	return m, nil
}

func (m *MessageFormat) Name() string { return m.name }

// ParamNames are the names of the parameters, in order.
func (m *MessageFormat) ParamNames() []string {
	out := make([]string, len(m.params))
	for i, p := range m.params {
		out[i] = p.name
	}
	return out
}

// Encode encodes the message with its parameters in order.
func (m *MessageFormat) Encode(args ...Value) ([]byte, error) {
	if len(args) != len(m.params) {
		return nil, errorf("%s has %d parameters, not %d", m.name, len(m.params), len(args))
	}
	out := append([]byte(nil), m.MsgIDBytes...)
	var err error
	for i, p := range m.params {
		if out, err = p.pt.encode(out, args[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// EncodeByName encodes the message with its parameters by name.
func (m *MessageFormat) EncodeByName(args map[string]Value) ([]byte, error) {
	out := append([]byte(nil), m.MsgIDBytes...)
	var err error
	for _, p := range m.params {
		v, ok := args[p.name]
		if !ok {
			return nil, errorf("Unable to encode: %s", m.name)
		}
		if out, err = p.pt.encode(out, v); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (m *MessageFormat) parse(s []byte, pos int) (map[string]Value, int, error) {
	pos += len(m.MsgIDBytes)
	out := make(map[string]Value, len(m.params))
	for _, p := range m.params {
		v, np, err := p.pt.parse(s, pos)
		if err != nil {
			return nil, np, err
		}
		out[p.name], pos = v, np
	}
	return out, pos, nil
}

func (m *MessageFormat) FormatParams(params map[string]Value) string {
	args := make([]string, len(m.params))
	for i, p := range m.params {
		args[i] = formatValue(params[p.name], p.pt.isDynamicString())
	}
	return pyPercent(m.debug, args)
}

// outputFormat is a debug message of the firmware, from its output() calls.
type outputFormat struct {
	idBytes []byte
	debug   string
	types   []paramType
}

func (o *outputFormat) Name() string { return "#output" }

func (o *outputFormat) parse(s []byte, pos int) (map[string]Value, int, error) {
	pos += len(o.idBytes)
	args := make([]string, 0, len(o.types))
	for _, t := range o.types {
		v, np, err := t.parse(s, pos)
		if err != nil {
			return nil, np, err
		}
		pos = np
		args = append(args, formatValue(v, t.isDynamicString()))
	}
	return map[string]Value{"#msg": pyPercent(o.debug, args)}, pos, nil
}

func (o *outputFormat) FormatParams(params map[string]Value) string {
	return "#output " + params["#msg"].(string)
}

// unknownFormat is a message whose ID the dictionary does not have.
type unknownFormat struct{}

func (unknownFormat) Name() string { return "#unknown" }

func (unknownFormat) parse(s []byte, pos int) (map[string]Value, int, error) {
	msgid, _, err := parseVLQ(s, pos, true)
	if err != nil {
		return nil, pos, err
	}
	return map[string]Value{"#msgid": msgid, "#msg": append([]byte(nil), s...)}, len(s) - MessageTrailerSize, nil
}

func (unknownFormat) FormatParams(params map[string]Value) string {
	return "#unknown " + PyBytesRepr(params["#msg"].([]byte))
}

// MessageKind tells if a message of the dictionary is a command, a response, or debug output.
type MessageKind string

const (
	KindCommand  MessageKind = "command"
	KindResponse MessageKind = "response"
	KindOutput   MessageKind = "output"
)

// Message is one message of the data dictionary.
type Message struct {
	ID     int
	Kind   MessageKind
	Format string
}

// Parser decodes and encodes the messages of one firmware, after it read the firmware's data
// dictionary with ProcessIdentify.
type Parser struct {
	WarnPrefix string

	enumerations  map[string]*enumeration
	messages      []Message
	messagesByID  map[int64]Format
	messagesByNam map[string]*MessageFormat
	msgidByFormat map[string]int
	config        map[string]any
	kconfig       any
	version       string
	buildVersions string
	rawIdentify   []byte
}

func NewParser() *Parser {
	p := &Parser{enumerations: map[string]*enumeration{}, messagesByID: map[int64]Format{},
		messagesByNam: map[string]*MessageFormat{}, msgidByFormat: map[string]int{}, config: map[string]any{}}
	p.initMessages(defaultMessages, nil, nil)
	return p
}

func (p *Parser) errorf(format string, args ...any) error {
	return &Error{p.WarnPrefix + fmt.Sprintf(format, args...)}
}

// CheckPacket checks the start of s for a whole message block. It returns the length of the block,
// 0 when more data is needed, or -1 when the data is not a valid block.
func (p *Parser) CheckPacket(s []byte) int {
	if len(s) < MessageMin {
		return 0
	}
	msglen := int(s[MessagePosLen])
	if msglen < MessageMin || msglen > MessageMax {
		return -1
	}
	if s[MessagePosSeq]&^MessageSeqMask != MessageDest {
		return -1
	}
	if len(s) < msglen {
		return 0
	}
	if s[msglen-MessageTrailerSync] != MessageSync {
		return -1
	}
	crc := CRC16CCITT(s[:msglen-MessageTrailerSize])
	if crc[0] != s[msglen-MessageTrailerCRC] || crc[1] != s[msglen-MessageTrailerCRC+1] {
		return -1
	}
	return msglen
}

func (p *Parser) lookup(s []byte, pos int) (Format, error) {
	msgid, _, err := parseVLQ(s, pos, true)
	if err != nil {
		return nil, err
	}
	if f, ok := p.messagesByID[msgid]; ok {
		return f, nil
	}
	return unknownFormat{}, nil
}

// Dump decodes a message block as text: "seq: 1a", and then one line for each message.
func (p *Parser) Dump(s []byte) ([]string, error) {
	out := []string{fmt.Sprintf("seq: %02x", s[MessagePosSeq])}
	pos := MessageHeaderSize
	for {
		f, err := p.lookup(s, pos)
		if err != nil {
			return out, err
		}
		params, np, err := f.parse(s, pos)
		if err != nil {
			return out, err
		}
		pos = np
		out = append(out, f.FormatParams(params))
		if pos >= len(s)-MessageTrailerSize {
			break
		}
	}
	return out, nil
}

// Parse decodes a message block with one message. The name of the message is params["#name"].
func (p *Parser) Parse(s []byte) (map[string]Value, error) {
	f, err := p.lookup(s, MessageHeaderSize)
	if err != nil {
		return nil, err
	}
	params, pos, err := f.parse(s, MessageHeaderSize)
	if err != nil {
		return nil, err
	}
	if pos != len(s)-MessageTrailerSize {
		return nil, p.errorf("Extra data at end of message")
	}
	params["#name"] = f.Name()
	return params, nil
}

// EncodeMsgblock frames a command payload with its sequence number.
func EncodeMsgblock(seq int, cmd []byte) []byte {
	out := make([]byte, 0, MessageMin+len(cmd))
	out = append(out, byte(MessageMin+len(cmd)), byte(seq&MessageSeqMask|MessageDest))
	out = append(out, cmd...)
	crc := CRC16CCITT(out)
	return append(out, crc[0], crc[1], MessageSync)
}

// parseBuffer reads a buffer parameter of a command text, in hex.
func parseBuffer(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	if len(value)%2 != 0 {
		value = "0" + value
	}
	out := make([]byte, len(value)/2)
	for i := range out {
		b, err := strconv.ParseUint(value[2*i:2*i+2], 16, 8)
		if err != nil {
			return nil, err
		}
		out[i] = byte(b)
	}
	return out, nil
}

// LookupCommand returns the command of a format, for example "get_clock". The format must be the
// same as in the dictionary.
func (p *Parser) LookupCommand(msgformat string) (*MessageFormat, error) {
	parts := strings.Fields(msgformat)
	if len(parts) == 0 {
		return nil, p.errorf("Unknown command: %s", msgformat)
	}
	mp, ok := p.messagesByNam[parts[0]]
	if !ok {
		return nil, p.errorf("Unknown command: %s", parts[0])
	}
	if msgformat != mp.MsgFormat {
		return nil, p.errorf("Command format mismatch: %s vs %s", msgformat, mp.MsgFormat)
	}
	return mp, nil
}

// LookupMsgID returns the ID of a message format.
func (p *Parser) LookupMsgID(msgformat string) (int, error) {
	id, ok := p.msgidByFormat[msgformat]
	if !ok {
		return 0, p.errorf("Unknown command: %s", msgformat)
	}
	return id, nil
}

// LookupResponse returns a response format by name, or nil.
func (p *Parser) LookupResponse(name string) *MessageFormat { return p.messagesByNam[name] }

// CreateCommand encodes a command text, for example "set_digital_out pin=PG1 value=1".
func (p *Parser) CreateCommand(msg string) ([]byte, error) {
	parts := strings.Fields(msg)
	if len(parts) == 0 {
		return nil, nil
	}
	mp, ok := p.messagesByNam[parts[0]]
	if !ok {
		return nil, p.errorf("Unknown command: %s", parts[0])
	}
	args := map[string]Value{}
	for _, arg := range parts[1:] {
		name, value, ok := strings.Cut(arg, "=")
		t, known := mp.byName[name]
		if !ok || !known {
			return nil, p.errorf("Unable to extract params from: %s", parts[0])
		}
		switch {
		case t.isDynamicString():
			b, err := parseBuffer(value)
			if err != nil {
				return nil, p.errorf("Unable to extract params from: %s", parts[0])
			}
			args[name] = b
		case t.isInt():
			n, err := strconv.ParseInt(value, 0, 64)
			if err != nil {
				return nil, p.errorf("Unable to extract params from: %s", parts[0])
			}
			args[name] = n
		default:
			args[name] = value
		}
	}
	return mp.EncodeByName(args)
}

func (p *Parser) fillEnumerations(enumerations map[string]map[string]json.RawMessage) error {
	for addName, addEnums := range enumerations {
		en := p.enumerations[addName]
		if en == nil {
			en = &enumeration{name: addName, enums: map[string]int64{}, reverse: map[int64]string{}}
			p.enumerations[addName] = en
		}
		for enum, raw := range addEnums {
			var simple int64
			if json.Unmarshal(raw, &simple) == nil {
				en.enums[enum] = simple
				continue
			}
			var rng [2]int64
			if err := json.Unmarshal(raw, &rng); err != nil {
				return err
			}
			// A range: "PA0": [0, 16] is PA0 to PA15.
			root := strings.TrimRight(enum, "0123456789")
			start := int64(0)
			if len(root) != len(enum) {
				start, _ = strconv.ParseInt(enum[len(root):], 10, 64)
			}
			for i := int64(0); i < rng[1]; i++ {
				en.enums[root+strconv.FormatInt(start+i, 10)] = rng[0] + i
			}
		}
		en.reverse = make(map[int64]string, len(en.enums))
		for k, v := range en.enums {
			en.reverse[v] = k
		}
	}
	return nil
}

func (p *Parser) initMessages(messages map[string]int, commandIDs, outputIDs map[int]bool) error {
	formats := make([]string, 0, len(messages))
	for f := range messages {
		formats = append(formats, f)
	}
	sort.Strings(formats)
	for _, msgformat := range formats {
		msgid := messages[msgformat]
		kind := KindResponse
		if commandIDs[msgid] {
			kind = KindCommand
		} else if outputIDs[msgid] {
			kind = KindOutput
		}
		p.messages = append(p.messages, Message{msgid, kind, msgformat})
		p.msgidByFormat[msgformat] = msgid
		idBytes := encodeVLQ(nil, int64(msgid))
		if kind == KindOutput {
			types, err := lookupOutputParams(msgformat)
			if err != nil {
				return err
			}
			p.messagesByID[int64(msgid)] = &outputFormat{idBytes: idBytes, debug: debugFormat(msgformat), types: types}
			continue
		}
		m, err := newMessageFormat(idBytes, msgformat, p.enumerations)
		if err != nil {
			return err
		}
		p.messagesByID[int64(msgid)] = m
		p.messagesByNam[m.name] = m
	}
	return nil
}

// ProcessIdentify reads the data dictionary of the firmware. compressed is true for the data of the
// "identify" command, which the firmware sends zlib-compressed.
func (p *Parser) ProcessIdentify(data []byte, compressed bool) error {
	if compressed {
		r, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			return p.errorf("Error during identify: %v", err)
		}
		if data, err = io.ReadAll(r); err != nil {
			return p.errorf("Error during identify: %v", err)
		}
	}
	p.rawIdentify = data
	var d struct {
		Enumerations  map[string]map[string]json.RawMessage `json:"enumerations"`
		Commands      map[string]int                        `json:"commands"`
		Responses     map[string]int                        `json:"responses"`
		Output        map[string]int                        `json:"output"`
		Config        map[string]any                        `json:"config"`
		Kconfig       any                                   `json:"kconfig"`
		Version       string                                `json:"version"`
		BuildVersions string                                `json:"build_versions"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return p.errorf("Error during identify: %v", err)
	}
	if err := p.fillEnumerations(d.Enumerations); err != nil {
		return p.errorf("Error during identify: %v", err)
	}
	all := map[string]int{}
	commandIDs, outputIDs := map[int]bool{}, map[int]bool{}
	for f, id := range d.Commands {
		all[f] = id
		commandIDs[id] = true
	}
	for f, id := range d.Responses {
		all[f] = id
	}
	for f, id := range d.Output {
		all[f] = id
		outputIDs[id] = true
	}
	if err := p.initMessages(all, commandIDs, outputIDs); err != nil {
		return p.errorf("Error during identify: %v", err)
	}
	for k, v := range d.Config {
		p.config[k] = v
	}
	p.kconfig, p.version, p.buildVersions = d.Kconfig, d.Version, d.BuildVersions
	return nil
}

// RawDataDictionary is the data dictionary as the firmware sent it, uncompressed.
func (p *Parser) RawDataDictionary() []byte { return p.rawIdentify }

// Version is the firmware version and its build versions.
func (p *Parser) Version() (string, string) { return p.version, p.buildVersions }

// Messages lists the messages of the dictionary.
func (p *Parser) Messages() []Message { return append([]Message(nil), p.messages...) }

// Constants are the firmware constants, for example CLOCK_FREQ.
func (p *Parser) Constants() map[string]any {
	out := make(map[string]any, len(p.config))
	for k, v := range p.config {
		out[k] = v
	}
	return out
}

// Constant returns a firmware constant as a string.
func (p *Parser) Constant(name string) (string, error) {
	v, ok := p.config[name]
	if !ok {
		return "", p.errorf("Firmware constant '%s' not found", name)
	}
	switch x := v.(type) {
	case string:
		return x, nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	}
	return fmt.Sprint(v), nil
}

// ConstantFloat returns a firmware constant as a number.
func (p *Parser) ConstantFloat(name string) (float64, error) {
	s, err := p.Constant(name)
	if err != nil {
		return 0, err
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, p.errorf("Unable to parse firmware constant %s: %s", name, s)
	}
	return f, nil
}

// ConstantInt returns a firmware constant as an integer.
func (p *Parser) ConstantInt(name string) (int64, error) {
	s, err := p.Constant(name)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, p.errorf("Unable to parse firmware constant %s: %s", name, s)
	}
	return n, nil
}
