//go:build windows

package shell

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

func nativeOnly(t *testing.T) {
	t.Helper()
	if os.Getenv("DESKGRID_NATIVE_TESTS") != "1" {
		t.Skip("set DESKGRID_NATIVE_TESTS=1 for native clipboard/recycle tests")
	}
}
func TestNativeClipboardInteroperableFormats(t *testing.T) {
	nativeOnly(t)
	done := initCOM()
	defer done()
	var original *comObject
	r, _, _ := oleDLL.NewProc("OleGetClipboard").Call(uintptr(unsafe.Pointer(&original)))
	if int32(r) >= 0 && original != nil {
		defer original.release()
		defer func() {
			oleDLL.NewProc("OleSetClipboard").Call(uintptr(unsafe.Pointer(original)))
			oleDLL.NewProc("OleFlushClipboard").Call()
		}()
	}
	p := filepath.Join(t.TempDir(), "中文 空格.txt")
	if e := os.WriteFile(p, []byte("test"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, cut := range []bool{false, true} {
		if e := ClipboardWrite([]string{p}, cut); e != nil {
			t.Fatal(e)
		}
		paths, actual, e := ClipboardFiles()
		if e != nil || len(paths) != 1 || paths[0] != p || actual != cut {
			t.Fatalf("%#v %v %v", paths, actual, e)
		}
	}
}
func TestNativeRecycleTemporaryFile(t *testing.T) {
	nativeOnly(t)
	p := filepath.Join(t.TempDir(), "DeskGrid 回收站验收.txt")
	if e := os.WriteFile(p, []byte("DeskGrid native recycle test"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := RecycleOne(p); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(p); !os.IsNotExist(e) {
		t.Fatalf("source remains %v", e)
	}
}
func TestNativeShortcutAndSystemIcon(t *testing.T) {
	nativeOnly(t)
	done := initCOM()
	defer done()
	p := filepath.Join(t.TempDir(), "测试快捷方式.lnk")
	target := filepath.Join(os.Getenv("WINDIR"), "System32", "notepad.exe")
	if e := makeLink(target, p); e != nil {
		t.Fatal(e)
	}
	if st, e := os.Stat(p); e != nil || st.Size() < 76 {
		t.Fatalf("shortcut %v %v", st, e)
	}
	b, e := FileIcon(p)
	if e != nil || len(b) < 8 {
		t.Fatalf("icon %d %v", len(b), e)
	}
}
