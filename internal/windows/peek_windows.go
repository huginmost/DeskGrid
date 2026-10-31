//go:build windows

package windows

import (
	"fmt"
	"sync/atomic"
	"syscall"
	"unsafe"

	win "golang.org/x/sys/windows"
)

type DesktopPlacement struct{ Parent, Style, ExStyle, Host uintptr }

func PeekWindow(hwnd uintptr, focus func(bool)) (DesktopPlacement, error) {
	var saved DesktopPlacement
	if !IsDesktopAttached(hwnd) {
		return saved, fmt.Errorf("窗口尚未挂载桌面")
	}
	var bounds nativeRect
	if r, _, e := user32.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&bounds))); r == 0 {
		return saved, e
	}
	saved.Parent = DesktopParent(hwnd)
	saved.Style, _, _ = user32.NewProc("GetWindowLongPtrW").Call(hwnd, ^uintptr(15))
	saved.ExStyle, _, _ = user32.NewProc("GetWindowLongPtrW").Call(hwnd, ^uintptr(19))
	host, err := createPeekHost(bounds, focus)
	if err != nil {
		return saved, err
	}
	saved.Host = host.window
	if err = setWindowAlpha(host.window, 0); err != nil {
		user32.NewProc("DestroyWindow").Call(host.window)
		return saved, err
	}
	user32.NewProc("ShowWindow").Call(hwnd, 0)
	win.NewLazySystemDLL("kernel32.dll").NewProc("SetLastError").Call(0)
	if previous, _, e := user32.NewProc("SetParent").Call(hwnd, host.window); previous == 0 && e != syscall.Errno(0) {
		user32.NewProc("DestroyWindow").Call(host.window)
		return saved, e
	}
	host.child = hwnd
	peekChildren.Store(hwnd, host)
	// Only the host applies opacity; never multiply parent and content alpha.
	if err = setWindowAlpha(hwnd, 255); err != nil {
		_ = RestoreDesktopWindow(hwnd, saved)
		return saved, err
	}
	if r, _, e := user32.NewProc("SetWindowPos").Call(hwnd, 0, 0, 0, uintptr(bounds.Right-bounds.Left), uintptr(bounds.Bottom-bounds.Top), 0x10|0x40); r == 0 {
		_ = RestoreDesktopWindow(hwnd, saved)
		return saved, e
	}
	if err := RaisePeekWindow(hwnd); err != nil {
		_ = RestoreDesktopWindow(hwnd, saved)
		return saved, err
	}
	ShowInactive(host.window)
	if err := RaisePeekWindow(hwnd); err != nil {
		_ = RestoreDesktopWindow(hwnd, saved)
		return saved, err
	}
	return saved, nil
}

func RestoreDesktopWindow(hwnd uintptr, saved DesktopPlacement) error {
	var bounds nativeRect
	if r, _, e := user32.NewProc("GetWindowRect").Call(peekRoot(hwnd), uintptr(unsafe.Pointer(&bounds))); r == 0 {
		return e
	}
	user32.NewProc("ShowWindow").Call(hwnd, 0)
	if IsNativeWindow(saved.Host) {
		user32.NewProc("ShowWindow").Call(saved.Host, 0)
	}
	parent := saved.Parent
	if !IsNativeWindow(parent) {
		var e error
		parent, e = findDesktopParent()
		if e != nil {
			return e
		}
	}
	// Pinning or a collapse animation may change resizing while peek is active.
	// Restore the desktop styles without restoring an obsolete resize permission.
	currentStyle, _, _ := user32.NewProc("GetWindowLongPtrW").Call(hwnd, ^uintptr(15))
	style := (saved.Style &^ uintptr(0x80000000|0x10000000|0x40000)) | 0x40000000 | (currentStyle & 0x40000)
	user32.NewProc("SetWindowLongPtrW").Call(hwnd, ^uintptr(15), style)
	user32.NewProc("SetWindowLongPtrW").Call(hwnd, ^uintptr(19), saved.ExStyle&^uintptr(0x8|0x40000))
	win.NewLazySystemDLL("kernel32.dll").NewProc("SetLastError").Call(0)
	if previous, _, e := user32.NewProc("SetParent").Call(hwnd, parent); previous == 0 && e != syscall.Errno(0) {
		return e
	}
	peekChildren.Delete(hwnd)
	if value, ok := peekContainers.Load(saved.Host); ok {
		value.(*peekHost).child = 0
		user32.NewProc("DestroyWindow").Call(saved.Host)
	}
	return SetDesktopBounds(hwnd, int(bounds.Left), int(bounds.Top), int(bounds.Right-bounds.Left), int(bounds.Bottom-bounds.Top), true)
}

func ShowInactive(hwnd uintptr) { user32.NewProc("ShowWindow").Call(hwnd, 4) }
func ForegroundWindow() uintptr { h, _, _ := user32.NewProc("GetForegroundWindow").Call(); return h }
func WindowAt(x, y int32) uintptr {
	packed := uintptr(uint64(uint32(x)) | uint64(uint32(y))<<32)
	h, _, _ := user32.NewProc("WindowFromPoint").Call(packed)
	return h
}
func OwnsWindow(parent, child uintptr) bool {
	if parent == 0 || child == 0 {
		return false
	}
	parent = peekRoot(parent)
	if parent == child {
		return true
	}
	r, _, _ := user32.NewProc("IsChild").Call(parent, child)
	return r != 0
}
func IsMenuWindow(hwnd uintptr) bool { return windowClass(hwnd) == "#32768" }
func RaisePeekWindow(hwnd uintptr) error {
	hwnd = peekRoot(hwnd)
	style, _, _ := user32.NewProc("GetWindowLongPtrW").Call(hwnd, ^uintptr(15))
	root, _, _ := user32.NewProc("GetAncestor").Call(hwnd, 2) // GA_ROOT
	owner, _, _ := user32.NewProc("GetWindow").Call(hwnd, 4)  // GW_OWNER
	if style&0x40000000 != 0 || root != hwnd || owner != 0 {
		return fmt.Errorf("临时前置窗口尚未脱离桌面父级或所有者")
	}
	if r, _, err := user32.NewProc("SetWindowPos").Call(hwnd, ^uintptr(0), 0, 0, 0, 0, 0x1|0x2|0x10); r == 0 {
		return fmt.Errorf("临时置顶失败: %w", err)
	}
	exstyle, _, _ := user32.NewProc("GetWindowLongPtrW").Call(hwnd, ^uintptr(19))
	if exstyle&8 == 0 { // WS_EX_TOPMOST must apply to a top-level HWND, not a desktop child.
		return fmt.Errorf("临时置顶样式未生效")
	}
	return nil
}
func FocusPeekWindow(hwnd uintptr) bool {
	root := peekRoot(hwnd)
	thread := currentThread()
	foreground := ForegroundWindow()
	other, _, _ := user32.NewProc("GetWindowThreadProcessId").Call(foreground, 0)
	attached := false
	if other != 0 && thread != other {
		r, _, _ := user32.NewProc("AttachThreadInput").Call(thread, other, 1)
		attached = r != 0
	}
	user32.NewProc("SetForegroundWindow").Call(root)
	user32.NewProc("SetFocus").Call(hwnd)
	if attached {
		user32.NewProc("AttachThreadInput").Call(thread, other, 0)
	}
	return OwnsWindow(hwnd, ForegroundWindow())
}

type PeekInput struct {
	keyboard, mouse uintptr
	active          atomic.Bool
	binding         atomic.Uint64
	blocked         [256]bool
	recordOwner     uintptr
	recordRequest   string
	record          func(uint32, uint32)
	toggle          func()
	click           func(int32, int32)
}
type keyHookData struct {
	Key, Scan, Flags, Time uint32
	Extra                  uintptr
}
type mouseHookData struct {
	Point             nativePoint
	Data, Flags, Time uint32
	Extra             uintptr
}
type keyboardInput struct {
	Key, Scan   uint16
	Flags, Time uint32
	Extra       uintptr
}
type inputPacket struct {
	Type uint32
	_    uint32
	Data [32]byte
}

var peekInput atomic.Pointer[PeekInput]

func maskShortcutMenu() bool {
	packets := [2]inputPacket{{Type: 1}, {Type: 1}}
	*(*keyboardInput)(unsafe.Pointer(&packets[0].Data[0])) = keyboardInput{Key: 0xe8}
	*(*keyboardInput)(unsafe.Pointer(&packets[1].Data[0])) = keyboardInput{Key: 0xe8, Flags: 2}
	r, _, _ := user32.NewProc("SendInput").Call(2, uintptr(unsafe.Pointer(&packets[0])), unsafe.Sizeof(inputPacket{}))
	return r == 2
}

func shortcutModifiers() uint32 {
	var mods uint32
	for _, modifier := range []struct {
		key uintptr
		bit uint32
	}{{0x12, 1}, {0x11, 2}, {0x10, 4}, {0x5b, 8}, {0x5c, 8}} {
		state, _, _ := user32.NewProc("GetAsyncKeyState").Call(modifier.key)
		if state&0x8000 != 0 {
			mods |= modifier.bit
		}
	}
	return mods
}

func shortcutModifierKey(key uint32) bool {
	return key == 0x10 || key == 0x11 || key == 0x12 || key == 0x5b || key == 0x5c || key >= 0xa0 && key <= 0xa5
}

var peekKeyboardHook = syscall.NewCallback(func(code, message, data uintptr) uintptr {
	input := peekInput.Load()
	if int32(code) == 0 && input != nil {
		event := (*keyHookData)(unsafe.Pointer(data))
		key := event.Key
		if event.Flags&0x10 == 0 && key < 256 {
			down := message == 0x100 || message == 0x104
			up := message == 0x101 || message == 0x105
			if input.blocked[key] {
				if up {
					input.blocked[key] = false
				}
				return 1 // Consume repeats and the matching key-up, even after rebinding.
			}
			if down && !shortcutModifierKey(key) {
				if input.record != nil && !OwnsWindow(input.recordOwner, ForegroundWindow()) {
					cancel := input.record
					input.StopRecording(input.recordOwner, input.recordRequest)
					go cancel(0, 0)
				}
				binding := input.binding.Load()
				if input.record != nil || binding != 0 && uint32(binding) == key {
					mods := shortcutModifiers()
					if input.record != nil || uint32(binding>>32) == mods {
						// Only mask the menu after a complete configured/recorded chord.
						// Standalone Win down/up and unrelated Win combinations pass intact.
						if mods&9 == 0 || maskShortcutMenu() {
							input.blocked[key] = true
							if input.record != nil {
								recorded := input.record
								input.StopRecording(input.recordOwner, input.recordRequest)
								go recorded(mods, key)
							} else {
								go input.toggle()
							}
							return 1
						}
					}
				}
			}
		}
	}
	r, _, _ := user32.NewProc("CallNextHookEx").Call(0, code, message, data)
	return r
})
var peekMouseHook = syscall.NewCallback(func(code, message, data uintptr) uintptr {
	input := peekInput.Load()
	if int32(code) == 0 && input != nil && (message == 0x201 || message == 0x204 || message == 0x207 || message == 0x20b) {
		event := (*mouseHookData)(unsafe.Pointer(data))
		if event.Flags&1 == 0 {
			if input.active.Load() {
				go input.click(event.Point.X, event.Point.Y)
			}
		}
	}
	r, _, _ := user32.NewProc("CallNextHookEx").Call(0, code, message, data)
	return r
})

// Install/configure on the UI thread, which already runs a Windows message loop.
// Only a complete configured chord is intercepted; standalone Win is untouched.
func NewPeekInput(toggle func(), click func(int32, int32)) (*PeekInput, error) {
	input := &PeekInput{toggle: toggle, click: click}
	module, _, _ := win.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	var e error
	input.keyboard, _, e = user32.NewProc("SetWindowsHookExW").Call(13, peekKeyboardHook, module, 0)
	if input.keyboard == 0 {
		return nil, fmt.Errorf("组合快捷键监听: %w", e)
	}
	input.mouse, _, e = user32.NewProc("SetWindowsHookExW").Call(14, peekMouseHook, module, 0)
	if input.mouse == 0 {
		user32.NewProc("UnhookWindowsHookEx").Call(input.keyboard)
		return nil, fmt.Errorf("外部点击监听: %w", e)
	}
	peekInput.Store(input)
	return input, nil
}
func (input *PeekInput) Configure(mods, key uint32) {
	input.binding.Store(uint64(mods)<<32 | uint64(key))
}
func (input *PeekInput) Recording(owner uintptr, request string, callback func(uint32, uint32)) {
	if input.record != nil && (input.recordOwner != owner || input.recordRequest != request) {
		cancel := input.record
		go cancel(0, 0)
	}
	input.recordOwner, input.recordRequest, input.record = owner, request, callback
}
func (input *PeekInput) StopRecording(owner uintptr, request string) {
	if input.recordOwner == owner && input.recordRequest == request {
		input.recordOwner, input.recordRequest, input.record = 0, "", nil
	}
}
func (input *PeekInput) Active(active bool) { input.active.Store(active) }
func (input *PeekInput) Close() {
	input.active.Store(false)
	input.binding.Store(0)
	peekInput.CompareAndSwap(input, nil)
	user32.NewProc("UnhookWindowsHookEx").Call(input.keyboard)
	user32.NewProc("UnhookWindowsHookEx").Call(input.mouse)
}
