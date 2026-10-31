//go:build windows

// Package shell contains Windows integrations. No entry-organisation operation calls it implicitly.
package shell

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var user = windows.NewLazySystemDLL("user32.dll")
var kernel = windows.NewLazySystemDLL("kernel32.dll")
var shellDLL = windows.NewLazySystemDLL("shell32.dll")
var oleDLL = windows.NewLazySystemDLL("ole32.dll")
var clipboardMu sync.Mutex

func u16(s string) *uint16 { return windows.StringToUTF16Ptr(s) }
func failure(name string, r uintptr, e error) error {
	if r != 0 {
		return nil
	}
	return fmt.Errorf("%s: %w", name, e)
}

type Drive struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
	Error string `json:"error,omitempty"`
}

func Open(path, verb string) error {
	if strings.ContainsRune(path, 0) || !filepath.IsAbs(path) {
		return errors.New("需要有效的绝对路径")
	}
	if _, e := os.Lstat(path); e != nil {
		return fmt.Errorf("%s: %w", path, e)
	}
	switch verb {
	case "reveal":
		return Reveal(path)
	case "cmd", "powershell":
		info, e := os.Stat(path)
		if e != nil {
			return e
		}
		dir := path
		if !info.IsDir() {
			dir = filepath.Dir(path)
		}
		return openTerminal(dir, verb)
	case "", "open", "runas", "openas":
		if verb == "" {
			verb = "open"
		}
		return windows.ShellExecute(0, u16(verb), u16(path), nil, nil, windows.SW_SHOWNORMAL)
	default:
		return fmt.Errorf("不支持的 Shell 动作 %q", verb)
	}
}
func Message(title, message string) {
	user.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(u16(message))), uintptr(unsafe.Pointer(u16(title))), 0x10)
}
func SystemDark() bool {
	k, e := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if e != nil {
		return true
	}
	defer k.Close()
	v, _, e := k.GetIntegerValue("AppsUseLightTheme")
	return e == nil && v == 0
}
func ReducedMotion() bool {
	var enabled uint32 = 1
	user.NewProc("SystemParametersInfoW").Call(0x1042, 0, uintptr(unsafe.Pointer(&enabled)), 0)
	return enabled == 0
}
func Modifiers() (bool, bool) {
	c, _, _ := user.NewProc("GetKeyState").Call(0x11)
	s, _, _ := user.NewProc("GetKeyState").Call(0x10)
	return c&0x8000 != 0, s&0x8000 != 0
}

func fileDropPayload(paths []string) ([]byte, error) {
	if len(paths) == 0 {
		return nil, errors.New("没有文件")
	}
	var text []uint16
	for _, p := range paths {
		if strings.ContainsRune(p, 0) {
			return nil, errors.New("路径含 NUL")
		}
		text = append(text, utf16.Encode([]rune(p))...)
		text = append(text, 0)
	}
	text = append(text, 0)
	b := make([]byte, 20+len(text)*2)
	binary.LittleEndian.PutUint32(b, 20)
	binary.LittleEndian.PutUint32(b[16:], 1)
	for i, v := range text {
		binary.LittleEndian.PutUint16(b[20+i*2:], v)
	}
	return b, nil
}
func withClipboard(fn func() error) error {
	clipboardMu.Lock()
	defer clipboardMu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var r uintptr
	var e error
	for i := 0; i < 10; i++ {
		r, _, e = user.NewProc("OpenClipboard").Call(0)
		if r != 0 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if r == 0 {
		return fmt.Errorf("系统剪贴板正被占用: %w", e)
	}
	defer user.NewProc("CloseClipboard").Call()
	return fn()
}
func putClipboard(format uintptr, b []byte) error {
	h, _, e := kernel.NewProc("GlobalAlloc").Call(0x42, uintptr(len(b)))
	if h == 0 {
		return e
	}
	p, _, e := kernel.NewProc("GlobalLock").Call(h)
	if p == 0 {
		kernel.NewProc("GlobalFree").Call(h)
		return e
	}
	kernel.NewProc("RtlMoveMemory").Call(p, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	kernel.NewProc("GlobalUnlock").Call(h)
	r, _, e := user.NewProc("SetClipboardData").Call(format, h)
	if r == 0 {
		kernel.NewProc("GlobalFree").Call(h)
		return e
	}
	return nil
}
func ClipboardWrite(paths []string, cut bool) error {
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			return errors.New("剪贴板要求绝对路径")
		}
		if _, e := os.Lstat(p); e != nil {
			return e
		}
	}
	b, e := fileDropPayload(paths)
	if e != nil {
		return e
	}
	return withClipboard(func() error {
		user.NewProc("EmptyClipboard").Call()
		if e := putClipboard(15, b); e != nil {
			return e
		}
		f, _, _ := user.NewProc("RegisterClipboardFormatW").Call(uintptr(unsafe.Pointer(u16("Preferred DropEffect"))))
		effect := []byte{1, 0, 0, 0}
		if cut {
			effect[0] = 2
		}
		return putClipboard(f, effect)
	})
}
func ClipboardText(text string) error {
	u := utf16.Encode([]rune(text))
	b := make([]byte, (len(u)+1)*2)
	for i, v := range u {
		binary.LittleEndian.PutUint16(b[2*i:], v)
	}
	return withClipboard(func() error { user.NewProc("EmptyClipboard").Call(); return putClipboard(13, b) })
}
func ClipboardFiles() (paths []string, cut bool, err error) {
	paths = []string{}
	err = withClipboard(func() error {
		h, _, _ := user.NewProc("GetClipboardData").Call(15)
		if h == 0 {
			return errors.New("剪贴板没有真实文件列表")
		}
		query := shellDLL.NewProc("DragQueryFileW")
		n, _, _ := query.Call(h, 0xffffffff, 0, 0)
		for i := uintptr(0); i < n; i++ {
			l, _, _ := query.Call(h, i, 0, 0)
			buf := make([]uint16, l+1)
			query.Call(h, i, uintptr(unsafe.Pointer(&buf[0])), l+1)
			paths = append(paths, windows.UTF16ToString(buf))
		}
		f, _, _ := user.NewProc("RegisterClipboardFormatW").Call(uintptr(unsafe.Pointer(u16("Preferred DropEffect"))))
		d, _, _ := user.NewProc("GetClipboardData").Call(f)
		if d != 0 {
			p, _, _ := kernel.NewProc("GlobalLock").Call(d)
			if p != 0 {
				var effect [4]byte
				kernel.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&effect[0])), p, 4)
				cut = binary.LittleEndian.Uint32(effect[:])&2 != 0
				kernel.NewProc("GlobalUnlock").Call(d)
			}
		}
		return nil
	})
	return
}

func IntegrationCommand(exe string) string { return `"` + exe + `" --pin "%1"` }

var integrationRoots = []string{`Software\Classes\*\shell\DeskGrid.Pin`, `Software\Classes\Directory\shell\DeskGrid.Pin`, `Software\Classes\Directory\Background\shell\DeskGrid.Pin`}

var legacyIntegrationRoots = []string{`Software\Classes\*\shell\Sticker.Pin`, `Software\Classes\Directory\shell\Sticker.Pin`, `Software\Classes\Directory\Background\shell\Sticker.Pin`}

func removeLegacyIntegration() error {
	for _, base := range legacyIntegrationRoots {
		for _, key := range []string{base + `\command`, base} {
			if err := registry.DeleteKey(registry.CURRENT_USER, key); err != nil && err != registry.ErrNotExist {
				return err
			}
		}
	}
	return nil
}

// MigrateLegacyRegistration updates only integrations installed by the former
// product, using the already-saved opt-in preferences. Fresh installs are untouched.
func MigrateLegacyRegistration(autoStart, integration bool, exe string) error {
	var failures []error
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE)
	if err == nil {
		_, _, legacy := k.GetStringValue("Sticker")
		k.Close()
		if legacy == nil {
			failures = append(failures, SetAutoStart(autoStart, exe))
		}
	}
	for _, base := range legacyIntegrationRoots {
		if k, err := registry.OpenKey(registry.CURRENT_USER, base, registry.QUERY_VALUE); err == nil {
			k.Close()
			failures = append(failures, SetIntegration(integration, exe))
			break
		}
	}
	return errors.Join(failures...)
}

func SetIntegration(enabled bool, exe string) error {
	for _, base := range integrationRoots {
		if !enabled {
			if e := registry.DeleteKey(registry.CURRENT_USER, base+`\command`); e != nil && e != registry.ErrNotExist {
				return e
			}
			if e := registry.DeleteKey(registry.CURRENT_USER, base); e != nil && e != registry.ErrNotExist {
				return e
			}
			continue
		}
		k, _, e := registry.CreateKey(registry.CURRENT_USER, base, registry.SET_VALUE)
		if e != nil {
			return e
		}
		e = k.SetStringValue("", "固定到 DeskGrid")
		if e == nil {
			e = k.SetStringValue("Icon", exe)
		}
		if e == nil {
			e = k.SetStringValue("MultiSelectModel", "Player")
		}
		k.Close()
		if e != nil {
			return e
		}
		c, _, e := registry.CreateKey(registry.CURRENT_USER, base+`\command`, registry.SET_VALUE)
		if e != nil {
			return e
		}
		command := IntegrationCommand(exe)
		if strings.Contains(base, "Background") {
			command = `"` + exe + `" --pin "%V"`
		}
		e = c.SetStringValue("", command)
		c.Close()
		if e != nil {
			return e
		}
	}
	if err := removeLegacyIntegration(); err != nil {
		return err
	}
	shellDLL.NewProc("SHChangeNotify").Call(0x08000000, 0, 0, 0)
	return nil
}
func SetAutoStart(enabled bool, exe string) error {
	k, _, e := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if e != nil {
		return e
	}
	defer k.Close()
	if enabled {
		if e := k.SetStringValue("DeskGrid", `"`+exe+`"`); e != nil {
			return e
		}
	} else if e := k.DeleteValue("DeskGrid"); e != nil && e != registry.ErrNotExist {
		return e
	}
	e = k.DeleteValue("Sticker")
	if e == registry.ErrNotExist {
		return nil
	}
	return e
}
func RegistrationStatus() (autostart, integration string) {
	autostart = "已禁用"
	integration = "已禁用"
	k, e := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE)
	if e == nil {
		_, _, e = k.GetStringValue("DeskGrid")
		k.Close()
		if e == nil {
			autostart = "已启用（当前用户）"
		}
	}
	k, e = registry.OpenKey(registry.CURRENT_USER, integrationRoots[0]+`\command`, registry.QUERY_VALUE)
	if e == nil {
		integration = "已安装（传统菜单，Windows 11 显示更多选项）"
		k.Close()
	}
	return
}
