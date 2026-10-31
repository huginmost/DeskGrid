//go:build windows

package filesystem

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

func crossDevice(err error) bool { return errors.Is(err, syscall.Errno(17)) }

func attributes(p string) uint32 {
	u, e := syscall.UTF16PtrFromString(p)
	if e != nil {
		return 0
	}
	a, e := syscall.GetFileAttributes(u)
	if e != nil {
		return 0
	}
	return a
}
func isLink(p string, i os.FileInfo) bool {
	return i.Mode()&os.ModeSymlink != 0 || infoAttributes(p, i)&0x400 != 0
}
func isHidden(p string) bool { return attributes(p)&6 != 0 }
func infoAttributes(p string, i os.FileInfo) uint32 {
	if data, ok := i.Sys().(*syscall.Win32FileAttributeData); ok {
		return data.FileAttributes
	}
	return attributes(p)
}
func isHiddenInfo(p string, i os.FileInfo) bool { return infoAttributes(p, i)&6 != 0 }
func renameNoReplace(a, b string) error {
	from, e := syscall.UTF16PtrFromString(a)
	if e != nil {
		return e
	}
	to, e := syscall.UTF16PtrFromString(b)
	if e != nil {
		return e
	}
	return syscall.MoveFile(from, to)
}

var moveFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replaceFile(a, b string) error {
	from, e := syscall.UTF16PtrFromString(a)
	if e != nil {
		return e
	}
	to, e := syscall.UTF16PtrFromString(b)
	if e != nil {
		return e
	}
	r, _, err := moveFileEx.Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), 9)
	if r == 0 {
		return err
	}
	return nil
}
