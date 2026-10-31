//go:build windows

package shell

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestFileDropPayloadPreservesUnicodeAndTerminators(t *testing.T) {
	b, err := fileDropPayload([]string{`C:\中文 空格\a.txt`, `D:\b.txt`})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 24 || binary.LittleEndian.Uint32(b[:4]) != 20 || binary.LittleEndian.Uint32(b[16:20]) != 1 {
		t.Fatal("invalid DROPFILES header")
	}
	u := make([]uint16, (len(b)-20)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[20+2*i:])
	}
	s := string(utf16.Decode(u))
	if s != "C:\\中文 空格\\a.txt\x00D:\\b.txt\x00\x00" {
		t.Fatalf("bad paths %q", s)
	}
	if _, err = fileDropPayload([]string{"bad\x00path"}); err == nil {
		t.Fatal("embedded NUL accepted")
	}
}

func TestExplorerCommandQuotesExecutableAndPlaceholder(t *testing.T) {
	s := IntegrationCommand(`D:\中文 软件\DeskGrid.exe`)
	if s != `"D:\中文 软件\DeskGrid.exe" --pin "%1"` {
		t.Fatal(s)
	}
	if strings.Count(s, `"`) != 4 {
		t.Fatal("broken quoting")
	}
}

func TestHotkeyValidationRejectsAmbiguousBindings(t *testing.T) {
	m, k, e := ParseHotkey("Ctrl+Alt+S")
	if e != nil || m != 3 || k != 83 {
		t.Fatalf("%d %d %v", m, k, e)
	}
	for _, s := range []string{"S", "Ctrl+Unknown", "Ctrl+Alt", "Ctrl+S+P"} {
		if _, _, e := ParseHotkey(s); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
