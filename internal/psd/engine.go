package psd

// Photoshop's text-engine dictionary (EngineData): a small PostScript-like
// subset — `<< >>` dictionaries, arrays, names, numbers, strings with
// escapes and hex strings — plus the descriptor walker used for the TySh
// record itself. Ports of PSDText's Engine/EngineCursor/Reader.

import (
	"encoding/binary"
	"math"
	"strings"
	"unicode/utf16"
)

// engineValue is one Engine tree node.
type engineValue interface{}

type engineDict map[string]engineValue

// walk descends the dictionary keys; nil at any step.
func walk(v engineValue, keys ...string) engineValue {
	current := v
	for _, key := range keys {
		d, ok := current.(*engineDict)
		if !ok {
			return nil
		}
		next, ok := (*d)[key]
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

func engineNumber(v engineValue) *float64 {
	if n, ok := v.(*float64); ok {
		return n
	}
	return nil
}

func engineBool(v engineValue) *bool {
	if b, ok := v.(*bool); ok {
		return b
	}
	return nil
}

func engineString(v engineValue) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func engineArray(v engineValue) []engineValue {
	if a, ok := v.([]engineValue); ok {
		return a
	}
	return nil
}

// engineValueData parses EngineData bytes into the engine dictionary.
func engineValueData(data []byte) *engineDict {
	if d := engineDictionaryAt(data, 0); d != nil {
		return d
	}
	start := indexSeq(data, []byte("<<"))
	if start < 0 {
		return nil
	}
	return engineDictionaryAt(data, start)
}

func engineDictionaryAt(data []byte, start int) *engineDict {
	c := &engineCursor{bytes: data}
	c.index = start
	v := c.parseValue()
	if d, ok := v.(*engineDict); ok {
		return d
	}
	return nil
}

func indexSeq(data, needle []byte) int {
	if len(needle) == 0 || len(data) < len(needle) {
		return -1
	}
	for i := 0; i+len(needle) <= len(data); i++ {
		match := true
		for j := range needle {
			if data[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

type engineCursor struct {
	bytes []byte
	index int
}

func (c *engineCursor) parseValue() engineValue {
	c.skipWhitespace()
	if !c.has(0) {
		return nil
	}
	switch b := c.bytes[c.index]; {
	case b == '<':
		if c.peekAhead(1) == '<' {
			return c.parseDictionary()
		}
		return c.parseHex()
	case b == '[':
		return c.parseArray()
	case b == '(':
		return c.parseString()
	case b == '/':
		c.index++
		return c.readToken()
	case b == '-' || b == '+' || b == '.' || (b >= '0' && b <= '9'):
		n := c.parseNumber()
		if n == nil {
			return nil
		}
		return n
	default:
		if c.takeWord("true") {
			b := true
			return &b
		}
		if c.takeWord("false") {
			b := false
			return &b
		}
		if c.takeWord("null") {
			return ""
		}
		return nil
	}
}

func (c *engineCursor) parseDictionary() engineValue {
	if !c.take("<<") {
		return nil
	}
	items := engineDict{}
	for {
		c.skipWhitespace()
		if !c.has(0) || c.bytes[c.index] == '>' {
			break
		}
		if c.bytes[c.index] != '/' {
			return nil
		}
		c.index++
		key := c.readToken()
		value := c.parseValue()
		if value == nil {
			return nil
		}
		items[key] = value
	}
	if !c.take(">>") {
		return nil
	}
	return &items
}

func (c *engineCursor) parseArray() engineValue {
	if !c.take("[") {
		return nil
	}
	var items []engineValue
	for {
		c.skipWhitespace()
		if !c.has(0) || c.bytes[c.index] == ']' {
			break
		}
		value := c.parseValue()
		if value == nil {
			return nil
		}
		items = append(items, value)
	}
	if !c.take("]") {
		return nil
	}
	return items
}

func (c *engineCursor) parseNumber() *float64 {
	start := c.index
	if c.has(0) && (c.bytes[c.index] == '+' || c.bytes[c.index] == '-') {
		c.index++
	}
	for c.has(0) && c.bytes[c.index] >= '0' && c.bytes[c.index] <= '9' {
		c.index++
	}
	if c.has(0) && c.bytes[c.index] == '.' {
		c.index++
		for c.has(0) && c.bytes[c.index] >= '0' && c.bytes[c.index] <= '9' {
			c.index++
		}
	}
	if c.has(0) && (c.bytes[c.index] == 'e' || c.bytes[c.index] == 'E') {
		c.index++
		if c.has(0) && (c.bytes[c.index] == '+' || c.bytes[c.index] == '-') {
			c.index++
		}
		for c.has(0) && c.bytes[c.index] >= '0' && c.bytes[c.index] <= '9' {
			c.index++
		}
	}
	if c.index <= start {
		return nil
	}
	text := string(c.bytes[start:c.index])
	var value float64
	if _, err := fmtSscan(text, &value); err != nil {
		return nil
	}
	return &value
}

func (c *engineCursor) parseString() engineValue {
	if !c.take("(") {
		return nil
	}
	var raw []byte
	for c.has(0) {
		b := c.bytes[c.index]
		c.index++
		if b == ')' {
			break
		}
		if b == '\\' {
			if !c.has(0) {
				return nil
			}
			escaped := c.bytes[c.index]
			c.index++
			switch {
			case escaped == 'n':
				raw = append(raw, 0x0A)
			case escaped == 'r':
				raw = append(raw, 0x0D)
			case escaped == 't':
				raw = append(raw, 0x09)
			case escaped >= '0' && escaped <= '7':
				value := int(escaped - '0')
				for i := 0; i < 2; i++ {
					if !c.has(0) || c.bytes[c.index] < '0' || c.bytes[c.index] > '7' {
						break
					}
					value = value*8 + int(c.bytes[c.index]-'0')
					c.index++
				}
				raw = append(raw, byte(value&0xFF))
			case escaped != '\n' && escaped != '\r':
				raw = append(raw, escaped)
			}
		} else {
			raw = append(raw, b)
		}
	}
	return decodeEngine(raw)
}

func (c *engineCursor) parseHex() engineValue {
	if !c.take("<") {
		return nil
	}
	var nibbles []byte
	for c.has(0) && c.bytes[c.index] != '>' {
		b := c.bytes[c.index]
		c.index++
		if n, ok := hexNibble(b); ok {
			nibbles = append(nibbles, n)
		}
	}
	if !c.take(">") {
		return nil
	}
	var raw []byte
	for i := 0; i+1 < len(nibbles); i += 2 {
		raw = append(raw, nibbles[i]<<4|nibbles[i+1])
	}
	return decodeEngine(raw)
}

func decodeEngine(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		units := make([]uint16, 0, (len(raw)-2)/2)
		for i := 2; i+1 < len(raw); i += 2 {
			units = append(units, uint16(raw[i])<<8|uint16(raw[i+1]))
		}
		runes := utf16.Decode(units)
		return string(runes)
	}
	// isoLatin1: bytes map to the same code points.
	var sb strings.Builder
	for _, b := range raw {
		sb.WriteRune(rune(b))
	}
	return sb.String()
}

func hexNibble(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	}
	return 0, false
}

func (c *engineCursor) readToken() string {
	start := c.index
	for c.has(0) && !engineDelimiter(c.bytes[c.index]) {
		c.index++
	}
	return string(c.bytes[start:c.index])
}

func engineDelimiter(b byte) bool {
	return b <= 0x20 || b == '/' || b == '<' || b == '>' || b == '[' || b == ']' || b == '(' || b == ')'
}

func (c *engineCursor) takeWord(word string) bool {
	encoded := []byte(word)
	if c.index+len(encoded) > len(c.bytes) {
		return false
	}
	for i, b := range encoded {
		if c.bytes[c.index+i] != b {
			return false
		}
	}
	after := c.index + len(encoded)
	if after < len(c.bytes) && !engineDelimiter(c.bytes[after]) {
		return false
	}
	c.index = after
	return true
}

func (c *engineCursor) take(token string) bool {
	encoded := []byte(token)
	if c.index+len(encoded) > len(c.bytes) {
		return false
	}
	for i, b := range encoded {
		if c.bytes[c.index+i] != b {
			return false
		}
	}
	c.index += len(encoded)
	return true
}

func (c *engineCursor) skipWhitespace() {
	for c.has(0) && (c.bytes[c.index] <= 0x20 || c.bytes[c.index] == '%') {
		if c.bytes[c.index] == '%' {
			for c.has(0) && c.bytes[c.index] != '\n' && c.bytes[c.index] != '\r' {
				c.index++
			}
		} else {
			c.index++
		}
	}
}

func (c *engineCursor) has(ahead int) bool { return c.index+ahead < len(c.bytes) }
func (c *engineCursor) peekAhead(ahead int) byte {
	if c.index+ahead < len(c.bytes) {
		return c.bytes[c.index+ahead]
	}
	return 0
}

// ---------------------------------------------------------------------------
// TySh descriptor reader

// textDescriptor is the parsed TySh descriptor tree.
type textDescriptor map[string]textDescriptorValue

type textDescriptorValue interface{}

func (d textDescriptor) str(key string) *string {
	if s, ok := d[key].(string); ok {
		return &s
	}
	return nil
}

func (d textDescriptor) enum(key string) string {
	if s, ok := d[key].(textEnum); ok {
		return string(s)
	}
	return ""
}

func (d textDescriptor) data(key string) []byte {
	if b, ok := d[key].([]byte); ok {
		return b
	}
	return nil
}

type textEnum string

type textRect struct {
	x, y, width, height float64
}

func (d textDescriptor) rect(key string) *textRect {
	items, ok := d[key].(textDescriptor)
	if !ok {
		return nil
	}
	side := func(name string) *float64 {
		if n, ok := items[name].(*float64); ok {
			return n
		}
		if n, ok := items[strings.TrimSpace(name)].(*float64); ok {
			return n
		}
		return nil
	}
	left, top, right, bottom := side("Left"), side("Top "), side("Rght"), side("Btom")
	if left == nil || top == nil || right == nil || bottom == nil {
		return nil
	}
	values := []*float64{left, top, right, bottom}
	for _, v := range values {
		if math.IsNaN(*v) || math.IsInf(*v, 0) {
			return nil
		}
	}
	return &textRect{x: *left, y: *top, width: *right - *left, height: *bottom - *top}
}

// tdReader walks the TySh byte stream: version, transform, descriptor.
type tdReader struct {
	data   []byte
	offset int
}

func (r *tdReader) remaining() int { return len(r.data) - r.offset }

func (r *tdReader) bytes(n int) []byte {
	if n < 0 || r.offset+n > len(r.data) {
		r.offset = len(r.data) + 1 // poison: every later read fails
		return nil
	}
	s := r.data[r.offset : r.offset+n]
	r.offset += n
	return s
}

func (r *tdReader) u8() byte {
	b := r.bytes(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (r *tdReader) u16() uint16 {
	b := r.bytes(2)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint16(b)
}

func (r *tdReader) u32() uint32 {
	b := r.bytes(4)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func (r *tdReader) i32() int32 { return int32(r.u32()) }

func (r *tdReader) f64() float64 {
	b := r.bytes(8)
	if b == nil {
		return math.NaN()
	}
	return math.Float64frombits(binary.BigEndian.Uint64(b))
}

func (r *tdReader) fourCC() string {
	b := r.bytes(4)
	if b == nil {
		return ""
	}
	return string(b)
}

func (r *tdReader) identifier() string {
	length := r.u32()
	if r.offset > len(r.data) {
		return ""
	}
	if length == 0 {
		return r.fourCC()
	}
	if length > 10_000 {
		r.offset = len(r.data) + 1
		return ""
	}
	b := r.bytes(int(length))
	if b == nil {
		return ""
	}
	return string(b)
}

func (r *tdReader) unicode() string {
	count := r.u32()
	if r.offset > len(r.data) || count > 1_000_000 {
		r.offset = len(r.data) + 1
		return ""
	}
	raw := r.bytes(int(count) * 2)
	if raw == nil {
		return ""
	}
	if len(raw) == 0 {
		return ""
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(raw[i*2:])
	}
	return string(utf16.Decode(units))
}

// descriptor parses a top-level Object descriptor: u32 version header,
// then the body.
func (r *tdReader) descriptor() textDescriptor {
	if r.u32() != 16 {
		return nil
	}
	return r.descriptorBody()
}

// descriptorBody parses the descriptor body (nested Objc values carry no
// version header).
func (r *tdReader) descriptorBody() textDescriptor {
	r.unicode()
	r.identifier()
	count := r.u32()
	if r.offset > len(r.data) || count > 10_000 {
		r.offset = len(r.data) + 1
		return nil
	}
	items := textDescriptor{}
	for i := uint32(0); i < count; i++ {
		key := r.identifier()
		typ := r.fourCC()
		if key == "" && typ == "" && r.offset > len(r.data) {
			return nil
		}
		value, ok := r.tdValue(typ)
		if !ok {
			return nil
		}
		items[key] = value
	}
	return items
}

func (r *tdReader) tdValue(typ string) (textDescriptorValue, bool) {
	switch typ {
	case "doub":
		n := r.f64()
		return &n, true
	case "UntF":
		r.fourCC()
		n := r.f64()
		return &n, true
	case "long":
		return float64(r.i32()), true
	case "comp":
		raw := r.bytes(8)
		if raw == nil {
			return nil, false
		}
		var bits uint64
		for _, b := range raw {
			bits = bits<<8 | uint64(b)
		}
		n := float64(int64(bits))
		return &n, true
	case "bool":
		if r.offset >= len(r.data) {
			return nil, false
		}
		r.u8()
		return float64(0), true
	case "TEXT":
		s := r.unicode()
		if r.offset > len(r.data) {
			return nil, false
		}
		return s, true
	case "enum":
		r.identifier()
		name := r.identifier()
		if r.offset > len(r.data) {
			return nil, false
		}
		return textEnum(name), true
	case "tdta":
		length := r.u32()
		if r.offset > len(r.data) || length > 8_000_000 {
			r.offset = len(r.data) + 1
			return nil, false
		}
		raw := r.bytes(int(length))
		if raw == nil {
			return nil, false
		}
		out := make([]byte, len(raw))
		copy(out, raw)
		return out, true
	case "Objc", "GlbO":
		nested := r.descriptorBody()
		if nested == nil {
			return nil, false
		}
		return nested, true
	case "VlLs":
		count := r.u32()
		if r.offset > len(r.data) || count > 10_000 {
			r.offset = len(r.data) + 1
			return nil, false
		}
		items := make([]textDescriptorValue, 0, count)
		for i := uint32(0); i < count; i++ {
			itemType := r.fourCC()
			item, ok := r.tdValue(itemType)
			if !ok {
				return nil, false
			}
			items = append(items, item)
		}
		return items, true
	case "alis":
		length := r.u32()
		if r.offset > len(r.data) || length > 8_000_000 {
			r.offset = len(r.data) + 1
			return nil, false
		}
		if r.bytes(int(length)) == nil {
			return nil, false
		}
		return float64(0), true
	case "obj ":
		if r.reference() {
			return float64(0), true
		}
		return nil, false
	case "type", "GlbC":
		r.unicode()
		r.identifier()
		if r.offset > len(r.data) {
			return nil, false
		}
		return float64(0), true
	}
	return nil, false
}

// reference skips a descriptor reference so a later EngineData still reads.
func (r *tdReader) reference() bool {
	count := r.u32()
	if r.offset > len(r.data) || count > 10_000 {
		r.offset = len(r.data) + 1
		return false
	}
	for i := uint32(0); i < count; i++ {
		form := r.fourCC()
		switch form {
		case "prop":
			r.unicode()
			r.identifier()
			r.identifier()
		case "Clss":
			r.unicode()
			r.identifier()
		case "Enmr":
			r.unicode()
			r.identifier()
			r.identifier()
			r.identifier()
		case "rele":
			r.unicode()
			r.identifier()
			r.i32()
		case "Idnt", "indx":
			r.i32()
		case "name":
			r.unicode()
		default:
			return false
		}
		if r.offset > len(r.data) {
			return false
		}
	}
	return true
}

// fmtSscan parses a float the way strconv would but keeps the port honest
// about rejecting trailing garbage (engine numbers never have any).
func fmtSscan(text string, out *float64) (int, error) {
	value := 0.0
	digits := false
	i := 0
	neg := false
	if i < len(text) && (text[i] == '+' || text[i] == '-') {
		neg = text[i] == '-'
		i++
	}
	for ; i < len(text) && text[i] >= '0' && text[i] <= '9'; i++ {
		value = value*10 + float64(text[i]-'0')
		digits = true
	}
	if i < len(text) && text[i] == '.' {
		i++
		frac := 0.1
		for ; i < len(text) && text[i] >= '0' && text[i] <= '9'; i++ {
			value += float64(text[i]-'0') * frac
			frac /= 10
			digits = true
		}
	}
	if !digits {
		return 0, errNotNumber
	}
	if i < len(text) && (text[i] == 'e' || text[i] == 'E') {
		i++
		exp := 0
		expNeg := false
		if i < len(text) && (text[i] == '+' || text[i] == '-') {
			expNeg = text[i] == '-'
			i++
		}
		expDigits := false
		for ; i < len(text) && text[i] >= '0' && text[i] <= '9'; i++ {
			exp = exp*10 + int(text[i]-'0')
			expDigits = true
		}
		if expDigits {
			for exp > 0 {
				if expNeg {
					value /= 10
				} else {
					value *= 10
				}
				exp--
				if math.IsInf(value, 0) {
					break
				}
			}
		}
	}
	if i != len(text) {
		return 0, errNotNumber
	}
	if neg {
		value = -value
	}
	*out = value
	return 1, nil
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

const errNotNumber = simpleError("not a number")
