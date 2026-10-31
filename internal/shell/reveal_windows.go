//go:build windows

package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Reveal uses Explorer's Shell namespace API rather than launching a hidden
// explorer.exe with an ambiguously split /select argument.
func Reveal(path string) error {
	done := initCOM()
	defer done()
	root := filepath.VolumeName(path) + string(filepath.Separator)
	if filepath.Clean(path) == filepath.Clean(root) {
		return windows.ShellExecute(0, u16("open"), u16(path), nil, nil, windows.SW_SHOWNORMAL)
	}
	var absolute uintptr
	r, _, _ := shellDLL.NewProc("SHParseDisplayName").Call(uintptr(unsafe.Pointer(u16(path))), 0, uintptr(unsafe.Pointer(&absolute)), 0, 0)
	if int32(r) < 0 {
		return fmt.Errorf("无法在资源管理器中定位 %s：0x%08X", path, uint32(r))
	}
	defer oleDLL.NewProc("CoTaskMemFree").Call(absolute)
	parent, _, _ := shellDLL.NewProc("ILClone").Call(absolute)
	if parent == 0 {
		return fmt.Errorf("无法创建父目录定位信息：%s", path)
	}
	defer oleDLL.NewProc("CoTaskMemFree").Call(parent)
	if ok, _, _ := shellDLL.NewProc("ILRemoveLastID").Call(parent); ok == 0 {
		return fmt.Errorf("无法确定父目录：%s", path)
	}
	child, _, _ := shellDLL.NewProc("ILFindLastID").Call(absolute)
	children := [1]uintptr{child}
	r, _, _ = shellDLL.NewProc("SHOpenFolderAndSelectItems").Call(parent, 1, uintptr(unsafe.Pointer(&children[0])), 0)
	if int32(r) < 0 {
		// The normal visible Shell launch also handles Explorer namespace extensions.
		explorer := filepath.Join(os.Getenv("WINDIR"), "explorer.exe")
		return windows.ShellExecute(0, u16("open"), u16(explorer), u16(`/select,"`+path+`"`), nil, windows.SW_SHOWNORMAL)
	}
	return nil
}
