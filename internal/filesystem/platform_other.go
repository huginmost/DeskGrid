//go:build !windows

package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func crossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }

func isLink(_ string, i os.FileInfo) bool       { return i.Mode()&os.ModeSymlink != 0 }
func isHidden(p string) bool                    { return strings.HasPrefix(filepath.Base(p), ".") }
func isHiddenInfo(p string, _ os.FileInfo) bool { return isHidden(p) }
func renameNoReplace(a, b string) error {
	if _, e := os.Lstat(b); !os.IsNotExist(e) {
		return os.ErrExist
	}
	return os.Rename(a, b)
}
func replaceFile(a, b string) error { return os.Rename(a, b) }
