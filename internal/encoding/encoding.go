// Package encoding reads and writes the text of files in the encodings
// they come in: UTF-8, UTF-16 by its byte order mark, and the legacy
// ones of Chinese, Japanese, Korean and Western text.
package encoding

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	xenc "golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

// UTF8 is the encoding of most files, and of new ones.
const UTF8 = "utf-8"

// Info is an encoding, by its ID, as .editorconfig names those it knows,
// and its name.
type Info struct {
	ID, Name string
	enc      xenc.Encoding
}

// All are the encodings Kopi reads and writes.
var All = []Info{
	{UTF8, "UTF-8", nil},
	{"utf-16le", "UTF-16 LE", unicode.UTF16(unicode.LittleEndian, unicode.UseBOM)},
	{"utf-16be", "UTF-16 BE", unicode.UTF16(unicode.BigEndian, unicode.UseBOM)},
	{"gb18030", "GB18030 (Simplified Chinese, GBK)", simplifiedchinese.GB18030},
	{"big5", "Big5 (Traditional Chinese)", traditionalchinese.Big5},
	{"shift_jis", "Shift JIS (Japanese)", japanese.ShiftJIS},
	{"euc-jp", "EUC-JP (Japanese)", japanese.EUCJP},
	{"euc-kr", "EUC-KR (Korean)", korean.EUCKR},
	{"windows-1252", "Windows 1252 (Western)", charmap.Windows1252},
	{"latin1", "ISO 8859-1 (Latin 1)", charmap.ISO8859_1},
}

// ByID returns an encoding by its ID, false for none.
func ByID(id string) (Info, bool) {
	id = strings.ToLower(id)
	if id == "gbk" || id == "gb2312" {
		id = "gb18030"
	}
	for _, e := range All {
		if e.ID == id {
			return e, true
		}
	}
	return Info{}, false
}

// ErrBinary tells that a file holds no text.
var ErrBinary = fmt.Errorf("this file is binary")

// Detect returns the text of a file's bytes and the ID of their
// encoding: UTF-16 by its byte order mark, UTF-8 when they are, else the
// first of GB18030 and Windows 1252 they read as without fault.
// Bytes of NULs, but UTF-16's, are binary.
func Detect(data []byte) (string, string, error) {
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		s, err := Decode(data, "utf-16le")
		return s, "utf-16le", err
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		s, err := Decode(data, "utf-16be")
		return s, "utf-16be", err
	}
	if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return "", "", ErrBinary
	}
	if utf8.Valid(data) {
		return string(data), UTF8, nil
	}
	for _, id := range []string{"gb18030", "windows-1252"} {
		if s, err := Decode(data, id); err == nil && !strings.ContainsRune(s, utf8.RuneError) {
			return s, id, nil
		}
	}
	s, err := Decode(data, "windows-1252")
	return s, "windows-1252", err
}

// Decode returns the text of bytes in an encoding.
func Decode(data []byte, id string) (string, error) {
	e, ok := ByID(id)
	if !ok {
		return "", fmt.Errorf("unknown encoding %q", id)
	}
	if e.enc == nil {
		if !utf8.Valid(data) {
			return "", fmt.Errorf("the file is not UTF-8")
		}
		return string(data), nil
	}
	out, err := e.enc.NewDecoder().Bytes(data)
	return string(out), err
}

// Encode returns the bytes of text in an encoding; an error when it has
// characters the encoding has not.
func Encode(text, id string) ([]byte, error) {
	e, ok := ByID(id)
	if !ok {
		return nil, fmt.Errorf("unknown encoding %q", id)
	}
	if e.enc == nil {
		return []byte(text), nil
	}
	out, err := xenc.HTMLEscapeUnsupported(e.enc.NewEncoder()).Bytes([]byte(text))
	if err != nil {
		return nil, err
	}
	// HTMLEscapeUnsupported only to find them: refuse to write &#...;.
	if back, err := e.enc.NewDecoder().Bytes(out); err != nil || string(back) != text {
		return nil, fmt.Errorf("%s has no way to write some characters of the text", e.Name)
	}
	return out, nil
}
