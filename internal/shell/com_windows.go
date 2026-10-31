//go:build windows

package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type comObject struct{ vtable *[32]uintptr }

func (o *comObject) call(slot int, args ...uintptr) (uintptr, error) {
	args = append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)
	r, _, _ := syscall.SyscallN(o.vtable[slot], args...)
	if int32(r) < 0 {
		return r, fmt.Errorf("Windows COM 0x%08X", uint32(r))
	}
	return r, nil
}
func (o *comObject) release() {
	if o != nil {
		o.call(2)
	}
}
func guid(s string) *windows.GUID {
	g, e := windows.GUIDFromString(s)
	if e != nil {
		panic(e)
	}
	return &g
}
func createCOM(clsid, iid string) (*comObject, error) {
	var o *comObject
	r, _, _ := oleDLL.NewProc("CoCreateInstance").Call(uintptr(unsafe.Pointer(guid(clsid))), 0, 1, uintptr(unsafe.Pointer(guid(iid))), uintptr(unsafe.Pointer(&o)))
	if int32(r) < 0 {
		return nil, fmt.Errorf("CoCreateInstance 0x%08x", r)
	}
	return o, nil
}
func initCOM() func() {
	runtime.LockOSThread()
	r, _, _ := oleDLL.NewProc("CoInitializeEx").Call(0, 2)
	return func() {
		if int32(r) >= 0 {
			oleDLL.NewProc("CoUninitialize").Call()
		}
		runtime.UnlockOSThread()
	}
}

// RecycleOne requests FOFX_RECYCLEONDELETE and WANTNUKEWARNING. The latter
// overrides automatic confirmation if Windows proposes permanent deletion.
// There is deliberately no application-level direct-delete fallback.
func RecycleOne(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("回收站操作需要绝对路径")
	}
	if filepath.Clean(path) == filepath.VolumeName(path)+string(filepath.Separator) {
		return errors.New("不能删除磁盘根目录")
	}
	if _, e := os.Lstat(path); e != nil {
		return e
	}
	done := initCOM()
	defer done()
	operation, e := createCOM("{3AD05575-8857-4850-9277-11B85BDB8E09}", "{947AAB5F-0A5C-4C13-B4D6-4BF7836FC9F8}")
	if e != nil {
		return e
	}
	defer operation.release()
	// IFileOperation::SetOperationFlags index 5; no elevation/no confirmation, recycle only, early failure.
	if _, e = operation.call(5, 0x00080000|0x00100000|0x00000004|0x00000010|0x00000400|0x00004000); e != nil {
		return e
	}
	var item *comObject
	r, _, _ := shellDLL.NewProc("SHCreateItemFromParsingName").Call(uintptr(unsafe.Pointer(u16(path))), 0, uintptr(unsafe.Pointer(guid("{43826D1E-E718-42EE-BC55-A1E261C37BFE}"))), uintptr(unsafe.Pointer(&item)))
	if int32(r) < 0 {
		return fmt.Errorf("无法访问回收对象 %s (0x%x)", path, r)
	}
	defer item.release()
	if _, e = operation.call(18, uintptr(unsafe.Pointer(item)), 0); e != nil {
		return e
	}
	if _, e = operation.call(21); e != nil {
		return fmt.Errorf("未删除 %s：回收站操作失败，未改为永久删除: %w", path, e)
	}
	var aborted int32
	operation.call(22, uintptr(unsafe.Pointer(&aborted)))
	if aborted != 0 {
		return errors.New("回收站操作已取消或不能回收；源文件保留")
	}
	if _, e = os.Lstat(path); e == nil {
		return errors.New("系统未完成回收站操作；源文件保留")
	}
	return nil
}

type dropSource struct {
	vtable *[5]uintptr
	refs   int32
}

var dropSourceVTable = [5]uintptr{
	syscall.NewCallback(func(this uintptr, riid *windows.GUID, out *uintptr) uintptr {
		g := *riid
		if g == *guid("{00000122-0000-0000-C000-000000000046}") || g == *guid("{00000000-0000-0000-C000-000000000046}") {
			*out = this
			return 0
		}
		*out = 0
		return 0x80004002
	}),
	syscall.NewCallback(func(this uintptr) uintptr { return 2 }), syscall.NewCallback(func(this uintptr) uintptr { return 1 }),
	syscall.NewCallback(func(this, escape, keys uintptr) uintptr {
		if escape != 0 {
			return 0x40101
		}
		if keys&1 == 0 {
			return 0x40100
		}
		return 0
	}),
	syscall.NewCallback(func(this, effect uintptr) uintptr { return 0x40102 }),
}

// DragFiles runs on an STA/UI thread. SHCreateDataObject exposes CF_HDROP and Shell formats.
func DragFiles(paths []string, virtual bool) error {
	if len(paths) == 0 {
		return errors.New("没有可拖出的文件")
	}
	done := initCOM()
	defer done()
	if virtual {
		dir, e := os.MkdirTemp("", "deskgrid-shortcuts-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(dir)
		links := []string{}
		for i, p := range paths {
			name := filepath.Base(p) + ".lnk"
			target := filepath.Join(dir, name)
			if _, e = os.Stat(target); e == nil {
				target = filepath.Join(dir, fmt.Sprintf("%s-%d.lnk", filepath.Base(p), i+1))
			}
			if e = makeLink(p, target); e != nil {
				return e
			}
			links = append(links, target)
		}
		paths = links
	}
	pidls := make([]uintptr, len(paths))
	for i, p := range paths {
		if _, e := os.Lstat(p); e != nil {
			return e
		}
		r, _, _ := shellDLL.NewProc("SHParseDisplayName").Call(uintptr(unsafe.Pointer(u16(p))), 0, uintptr(unsafe.Pointer(&pidls[i])), 0, 0)
		if int32(r) < 0 {
			return fmt.Errorf("无法创建 Shell 拖放对象 0x%x", r)
		}
		defer oleDLL.NewProc("CoTaskMemFree").Call(pidls[i])
	}
	var data *comObject
	r, _, _ := shellDLL.NewProc("SHCreateDataObject").Call(0, uintptr(len(pidls)), uintptr(unsafe.Pointer(&pidls[0])), 0, uintptr(unsafe.Pointer(guid("{0000010E-0000-0000-C000-000000000046}"))), uintptr(unsafe.Pointer(&data)))
	if int32(r) < 0 {
		return fmt.Errorf("SHCreateDataObject 0x%x", r)
	}
	defer data.release()
	source := &dropSource{vtable: &dropSourceVTable, refs: 1}
	var effect uint32
	allowed := uintptr(3)
	if virtual {
		allowed = 1
	}
	r, _, _ = oleDLL.NewProc("DoDragDrop").Call(uintptr(unsafe.Pointer(data)), uintptr(unsafe.Pointer(source)), allowed, uintptr(unsafe.Pointer(&effect)))
	runtime.KeepAlive(source)
	if int32(r) < 0 {
		return fmt.Errorf("原生拖出失败 0x%x", r)
	}
	return nil
}
func makeLink(target, path string) error {
	o, e := createCOM("{00021401-0000-0000-C000-000000000046}", "{000214F9-0000-0000-C000-000000000046}")
	if e != nil {
		return e
	}
	defer o.release()
	if _, e = o.call(20, uintptr(unsafe.Pointer(u16(target)))); e != nil {
		return e
	}
	o.call(9, uintptr(unsafe.Pointer(u16(filepath.Dir(target)))))
	var persist *comObject
	if _, e = o.call(0, uintptr(unsafe.Pointer(guid("{0000010B-0000-0000-C000-000000000046}"))), uintptr(unsafe.Pointer(&persist))); e != nil {
		return e
	}
	defer persist.release()
	_, e = persist.call(6, uintptr(unsafe.Pointer(u16(path))), 1)
	return e
}
