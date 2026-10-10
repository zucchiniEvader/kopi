package encoding

import (
	"testing"
)

func TestDetectAndEncode(t *testing.T) {
	gbk := []byte{0xc4, 0xe3, 0xba, 0xc3, 0xa3, 0xac, 0xca, 0xc0, 0xbd, 0xe7, '\n'} // 你好，世界
	for _, c := range []struct {
		data      []byte
		text, enc string
	}{
		{[]byte("hello\n"), "hello\n", UTF8},
		{[]byte("你好\n"), "你好\n", UTF8},
		{gbk, "你好，世界\n", "gb18030"},
		{[]byte("caf\xe9\n"), "café\n", "windows-1252"},
		{[]byte{0xFF, 0xFE, 'h', 0, 'i', 0}, "hi", "utf-16le"},
		{[]byte{0xFE, 0xFF, 0, 'h', 0, 'i'}, "hi", "utf-16be"},
	} {
		text, enc, err := Detect(c.data)
		if err != nil || text != c.text || enc != c.enc {
			t.Errorf("%q: %q %s %v", c.data, text, enc, err)
			continue
		}
		back, err := Encode(text, enc)
		if err != nil || string(back) != string(c.data) {
			t.Errorf("%q: encoded %q %v", c.data, back, err)
		}
	}
	if _, _, err := Detect([]byte("a\x00b")); err != ErrBinary {
		t.Errorf("binary: %v", err)
	}
	if _, err := Encode("你好", "windows-1252"); err == nil {
		t.Error("你好 in Windows 1252")
	}
	if e, ok := ByID("GBK"); !ok || e.ID != "gb18030" {
		t.Errorf("GBK: %v", e)
	}
}
