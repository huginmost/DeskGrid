//go:build windows

package shell

import (
	"fmt"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// shortcutTargetPath reads the stored target on the caller's COM thread. It
// never calls Resolve, changes the link, or reads/changes launch arguments.
func shortcutTargetPath(path string) (string, error) {
	link, err := createCOM("{00021401-0000-0000-C000-000000000046}", "{000214F9-0000-0000-C000-000000000046}") // ShellLink / IShellLinkW
	if err != nil {
		return "", err
	}
	defer link.release()
	var persist *comObject
	if _, err = link.call(0, uintptr(unsafe.Pointer(guid("{0000010B-0000-0000-C000-000000000046}"))), uintptr(unsafe.Pointer(&persist))); err != nil { // IPersistFile
		return "", err
	}
	defer persist.release()
	if _, err = persist.call(5, uintptr(unsafe.Pointer(u16(path))), 0); err != nil { // IPersistFile::Load, STGM_READ
		return "", err
	}
	buffer := make([]uint16, 32768)
	r, err := link.call(3, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0, 0x4) // IShellLinkW::GetPath, SLGP_RAWPATH
	if err != nil {
		return "", err
	}
	target := windows.UTF16ToString(buffer)
	if r != 0 || target == "" { // S_FALSE also means no stored filesystem target.
		return "", fmt.Errorf("快捷方式未提供文件目标")
	}
	// RAWPATH may contain environment variables. Expand the stored path only;
	// do not ask the Shell to search for, repair, or launch a missing target.
	n, err := windows.ExpandEnvironmentStrings(u16(target), &buffer[0], uint32(len(buffer)))
	if err != nil {
		return "", err
	}
	if n > uint32(len(buffer)) {
		return "", fmt.Errorf("快捷方式目标路径过长")
	}
	target = windows.UTF16ToString(buffer)
	if !filepath.IsAbs(target) {
		return "", fmt.Errorf("快捷方式未提供绝对文件目标")
	}
	return filepath.Clean(target), nil
}
