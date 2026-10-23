package service

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"regexp"
	"strconv"
)

// This is a bounded syntax scan for the invoice attachment denylist, not a PDF
// renderer or malware scanner. Stream bytes are data; only PDF name tokens are
// checked. Ambiguous lengths and unsupported object-stream encodings fail closed.
type invoicePDFToken struct {
	kind byte
	text string
	pos  int
}

type invoicePDFValue struct {
	kind  byte
	num   int64
	text  string
	ref   string
	dict  map[string]invoicePDFValue
	array []invoicePDFValue
}

type invoicePDFScan struct {
	data    []byte
	pos     int
	values  *int
	decoded *int64
	lengths map[string]invoicePDFToken
	used    map[string]invoicePDFToken
	scalars map[string]invoicePDFToken
}

const invoicePDFGap = `(?:[\x00\t\n\f\r ]|%[^\r\n]*(?:\r\n?|\n))+`

var invoicePDFScalarObject = regexp.MustCompile(`([0-9]+)` + invoicePDFGap + `([0-9]+)` + invoicePDFGap + `obj` + invoicePDFGap + `([+]?[0-9]+)` + invoicePDFGap + `endobj\b`)

func invoicePDFSpace(c byte) bool {
	return c == 0 || c == 9 || c == 10 || c == 12 || c == 13 || c == 32
}
func invoicePDFDelimiter(c byte) bool {
	return invoicePDFSpace(c) || bytes.IndexByte([]byte("()<>[]{}/%"), c) >= 0
}

func (s *invoicePDFScan) token() (invoicePDFToken, error) {
	for s.pos < len(s.data) {
		if invoicePDFSpace(s.data[s.pos]) {
			s.pos++
			continue
		}
		if s.data[s.pos] != '%' {
			break
		}
		for s.pos < len(s.data) && s.data[s.pos] != '\r' && s.data[s.pos] != '\n' {
			s.pos++
		}
	}
	t := invoicePDFToken{pos: s.pos}
	if s.pos == len(s.data) {
		return t, io.EOF
	}
	c := s.data[s.pos]
	s.pos++
	switch c {
	case '(':
		depth := 1
		for s.pos < len(s.data) && depth > 0 {
			c = s.data[s.pos]
			s.pos++
			switch c {
			case '\\':
				if s.pos == len(s.data) {
					return t, fmt.Errorf("truncated PDF string")
				}
				s.pos++
			case '(':
				depth++
				if depth > 64 {
					return t, fmt.Errorf("PDF string nesting limit")
				}
			case ')':
				depth--
			}
		}
		if depth != 0 {
			return t, fmt.Errorf("unterminated PDF string")
		}
		t.kind = 's'
	case '<':
		if s.pos < len(s.data) && s.data[s.pos] == '<' {
			s.pos++
			t.kind = 'd'
			break
		}
		for s.pos < len(s.data) && s.data[s.pos] != '>' {
			c = s.data[s.pos]
			s.pos++
			if !invoicePDFSpace(c) && !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return t, fmt.Errorf("invalid PDF hex string")
			}
		}
		if s.pos == len(s.data) {
			return t, fmt.Errorf("unterminated PDF hex string")
		}
		s.pos++
		t.kind = 's'
	case '>':
		if s.pos == len(s.data) || s.data[s.pos] != '>' {
			return t, fmt.Errorf("invalid PDF dictionary")
		}
		s.pos++
		t.kind = 'e'
	case '[', ']':
		t.kind = c
	case ')', '{', '}':
		return t, fmt.Errorf("invalid PDF delimiter")
	default:
		start := t.pos
		if c == '/' {
			t.kind, start = 'n', s.pos
		} else {
			t.kind = 'w'
		}
		for s.pos < len(s.data) && !invoicePDFDelimiter(s.data[s.pos]) {
			s.pos++
		}
		t.text = string(s.data[start:s.pos])
		if t.kind == 'n' {
			var name []byte
			for i := 0; i < len(t.text); i++ {
				if t.text[i] != '#' {
					name = append(name, t.text[i])
					continue
				}
				if i+2 >= len(t.text) {
					return t, fmt.Errorf("invalid PDF name escape")
				}
				value, err := strconv.ParseUint(t.text[i+1:i+3], 16, 8)
				if err != nil || value == 0 {
					return t, fmt.Errorf("invalid PDF name escape")
				}
				name = append(name, byte(value))
				i += 2
			}
			t.text = string(name)
			switch t.text {
			case "JavaScript", "JS", "Launch", "EmbeddedFile", "RichMedia", "OpenAction", "AA", "Encrypt":
				return t, fmt.Errorf("active PDF name")
			}
		}
	}
	return t, nil
}

func (s *invoicePDFScan) value(t invoicePDFToken, depth int) (invoicePDFValue, error) {
	v := invoicePDFValue{kind: t.kind, text: t.text}
	(*s.values)++
	if depth > 64 || *s.values > 100000 {
		return v, fmt.Errorf("PDF value limit")
	}
	switch t.kind {
	case 'd':
		v.dict = map[string]invoicePDFValue{}
		for {
			key, err := s.token()
			if err != nil {
				return v, err
			}
			if key.kind == 'e' {
				return v, nil
			}
			if key.kind != 'n' {
				return v, fmt.Errorf("invalid PDF dictionary key")
			}
			if _, exists := v.dict[key.text]; exists {
				return v, fmt.Errorf("duplicate PDF dictionary key")
			}
			t, err := s.token()
			if err != nil {
				return v, err
			}
			entry, err := s.value(t, depth+1)
			if err != nil {
				return v, err
			}
			v.dict[key.text] = entry
		}
	case '[':
		for {
			t, err := s.token()
			if err != nil {
				return v, err
			}
			if t.kind == ']' {
				return v, nil
			}
			entry, err := s.value(t, depth+1)
			if err != nil {
				return v, err
			}
			v.array = append(v.array, entry)
		}
	case 'n', 's':
		return v, nil
	case 'w':
		if t.text == "true" || t.text == "false" || t.text == "null" {
			return v, nil
		}
		n, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil {
			if _, err := strconv.ParseFloat(t.text, 64); err != nil {
				return v, fmt.Errorf("invalid PDF value")
			}
			return v, nil
		}
		v.kind, v.num = 'i', n
		position := s.pos
		generation, e := s.token()
		g, e2 := strconv.ParseInt(generation.text, 10, 64)
		if e == nil && e2 == nil && generation.kind == 'w' && n > 0 && g >= 0 {
			r, e := s.token()
			if e == nil && r.kind == 'w' && r.text == "R" {
				v.kind, v.ref = 'r', fmt.Sprintf("%d:%d", n, g)
				return v, nil
			}
		}
		s.pos = position
		return v, nil
	default:
		return v, fmt.Errorf("invalid PDF value")
	}
}

func (s *invoicePDFScan) objectStream(content []byte, dictionary map[string]invoicePDFValue) error {
	filter := dictionary["Filter"]
	if filter.kind == '[' && len(filter.array) == 1 {
		filter = filter.array[0]
	}
	if filter.kind != 0 && !(filter.kind == 'n' && (filter.text == "FlateDecode" || filter.text == "Fl")) {
		return fmt.Errorf("unsupported PDF object-stream encoding")
	}
	params := dictionary["DecodeParms"]
	if params.kind == '[' && len(params.array) == 1 {
		params = params.array[0]
	}
	if params.kind != 0 && !(params.kind == 'w' && params.text == "null") {
		return fmt.Errorf("unsupported PDF object-stream parameters")
	}
	if filter.kind != 0 {
		reader, err := zlib.NewReader(bytes.NewReader(content))
		if err != nil {
			return err
		}
		defer reader.Close()
		content, err = io.ReadAll(io.LimitReader(reader, *s.decoded+1))
		if err != nil {
			return err
		}
	}
	*s.decoded -= int64(len(content))
	if *s.decoded < 0 {
		return fmt.Errorf("PDF decoded-data limit")
	}
	n, first := dictionary["N"], dictionary["First"]
	if n.kind != 'i' || n.num < 1 || n.num > 100000 || first.kind != 'i' || first.num < 0 || first.num > int64(len(content)) {
		return fmt.Errorf("invalid PDF object-stream header")
	}
	header := invoicePDFScan{data: content[:first.num]}
	offsets := make([]int, 0, n.num+1)
	for range n.num {
		id, err := header.token()
		number, e := strconv.ParseInt(id.text, 10, 64)
		if err != nil || e != nil || id.kind != 'w' || number <= 0 {
			return fmt.Errorf("invalid PDF object-stream reference")
		}
		t, err := header.token()
		offset, e := strconv.ParseInt(t.text, 10, 64)
		if err != nil || e != nil || t.kind != 'w' || offset < 0 || offset >= int64(len(content))-first.num || len(offsets) > 0 && int(offset)+int(first.num) <= offsets[len(offsets)-1] {
			return fmt.Errorf("invalid PDF object-stream offset")
		}
		offsets = append(offsets, int(offset)+int(first.num))
	}
	if _, err := header.token(); err != io.EOF {
		return fmt.Errorf("invalid PDF object-stream header length")
	}
	offsets = append(offsets, len(content))
	for i := 0; i < len(offsets)-1; i++ {
		object := invoicePDFScan{data: content[offsets[i]:offsets[i+1]], values: s.values}
		t, err := object.token()
		if err != nil {
			return err
		}
		if _, err := object.value(t, 0); err != nil {
			return err
		}
		if _, err := object.token(); err != io.EOF {
			return fmt.Errorf("invalid PDF object-stream value")
		}
	}
	return nil
}

func invoicePDFNamesSafe(content []byte) bool {
	values, decoded := 0, InvoiceAttachmentMaxBytes
	s := invoicePDFScan{data: content, values: &values, decoded: &decoded, lengths: map[string]invoicePDFToken{}, used: map[string]invoicePDFToken{}, scalars: map[string]invoicePDFToken{}}
	// Resolve forward indirect /Length references without mistaking arbitrary
	// stream bytes for objects: every chosen offset must later be visited as a
	// top-level scalar object. Duplicate/ambiguous definitions are unsupported.
	for _, match := range invoicePDFScalarObject.FindAllSubmatchIndex(content, 100001) {
		if match[0] > 0 && !invoicePDFDelimiter(content[match[0]-1]) || match[1] < len(content) && !invoicePDFDelimiter(content[match[1]]) {
			continue
		}
		id, _ := strconv.ParseInt(string(content[match[2]:match[3]]), 10, 64)
		generation, _ := strconv.ParseInt(string(content[match[4]:match[5]]), 10, 64)
		ref := fmt.Sprintf("%d:%d", id, generation)
		if _, exists := s.lengths[ref]; exists {
			s.lengths[ref] = invoicePDFToken{pos: -1}
		} else {
			s.lengths[ref] = invoicePDFToken{pos: match[0], text: string(content[match[6]:match[7]])}
		}
	}
	objects := 0
	for {
		t, err := s.token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
		if t.kind == 'd' || t.kind == '[' {
			if _, err := s.value(t, 0); err != nil {
				return false
			}
			continue
		}
		position := s.pos
		id, err := strconv.ParseInt(t.text, 10, 64)
		if t.kind != 'w' || err != nil || id <= 0 {
			if t.kind == 'w' && (t.text == "stream" || t.text == "endstream" || t.text == "obj" || t.text == "endobj") {
				return false
			}
			continue
		}
		generation, e := s.token()
		g, e2 := strconv.ParseInt(generation.text, 10, 64)
		obj, e3 := s.token()
		if e != nil || e2 != nil || e3 != nil || generation.kind != 'w' || g < 0 || obj.kind != 'w' || obj.text != "obj" {
			s.pos = position
			continue
		}
		objects++
		if objects > 100000 {
			return false
		}
		ref := fmt.Sprintf("%d:%d", id, g)
		first, err := s.token()
		if err != nil {
			return false
		}
		value, err := s.value(first, 0)
		if err != nil {
			return false
		}
		end, err := s.token()
		if err != nil {
			return false
		}
		if end.kind == 'w' && end.text == "stream" {
			length := value.dict["Length"]
			if value.kind != 'd' || s.pos == len(content) || content[s.pos] != '\r' && content[s.pos] != '\n' {
				return false
			}
			if content[s.pos] == '\r' {
				s.pos++
			}
			if s.pos < len(content) && content[s.pos] == '\n' {
				s.pos++
			}
			if length.kind == 'r' {
				target, ok := s.lengths[length.ref]
				if !ok || target.pos < 0 {
					return false
				}
				s.used[length.ref] = target
				length.num, err = strconv.ParseInt(target.text, 10, 64)
				length.kind = 'i'
			}
			if err != nil || length.kind != 'i' || length.num < 0 || length.num > int64(len(content)-s.pos) {
				return false
			}
			stream := content[s.pos : s.pos+int(length.num)]
			s.pos += int(length.num)
			end, err = s.token()
			if err != nil || end.kind != 'w' || end.text != "endstream" {
				return false
			}
			kind := value.dict["Type"]
			// Readers can select an object stream through xref type-2 entries even
			// when /Type is missing or mislabeled. Its required First offset still
			// identifies it; N alone is also used by ordinary ICC color profiles.
			if kind.kind == 'n' && kind.text == "ObjStm" || value.dict["First"].kind != 0 {
				if s.objectStream(stream, value.dict) != nil {
					return false
				}
			} else if kind.kind != 0 && kind.kind != 'n' {
				return false // Indirect/ambiguous stream types cannot hide object streams.
			}
			end, err = s.token()
		}
		if err != nil || end.kind != 'w' || end.text != "endobj" {
			return false
		}
		if value.kind == 'i' {
			s.scalars[ref] = invoicePDFToken{pos: t.pos, text: strconv.FormatInt(value.num, 10)}
		}
	}
	for ref, target := range s.used {
		if scalar, ok := s.scalars[ref]; !ok || scalar.pos != target.pos {
			return false
		}
	}
	return objects > 0
}
