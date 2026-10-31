//go:build windows

package windows

import (
	"fmt"
	win "golang.org/x/sys/windows"
	"math"
	"runtime"
	"syscall"
	"unsafe"
)

var user32 = win.NewLazySystemDLL("user32.dll")

type WorkArea struct{ X, Y, Width, Height int }

func Clamp(x, y, w, h int, areas []WorkArea) (int, int, int, int) {
	if len(areas) == 0 {
		return x, y, w, h
	}
	chosen := areas[0]
	best := math.MaxFloat64
	for _, a := range areas {
		cx := math.Max(float64(a.X), math.Min(float64(x), float64(a.X+a.Width)))
		cy := math.Max(float64(a.Y), math.Min(float64(y), float64(a.Y+a.Height)))
		distance := math.Hypot(cx-float64(x), cy-float64(y))
		if distance < best {
			best = distance
			chosen = a
		}
	}
	w = max(220, min(w, chosen.Width))
	h = max(32, min(h, chosen.Height))
	x = max(chosen.X, min(x, chosen.X+chosen.Width-w))
	y = max(chosen.Y, min(y, chosen.Y+chosen.Height-h))
	return x, y, w, h
}
func SetOpacity(hwnd uintptr, opacity float64) error {
	return SetWindowAlpha(hwnd, byte(math.Round(max(.1, min(1, opacity))*255)))
}

func SetWindowAlpha(hwnd uintptr, alpha byte) error {
	return setWindowAlpha(peekRoot(hwnd), alpha)
}

func setWindowAlpha(hwnd uintptr, alpha byte) error {
	get := user32.NewProc("GetWindowLongPtrW")
	style, _, _ := get.Call(hwnd, ^uintptr(19)) // GWL_EXSTYLE -20
	if style&0x80000 == 0 {
		user32.NewProc("SetWindowLongPtrW").Call(hwnd, ^uintptr(19), style|0x80000)
	}
	r, _, e := user32.NewProc("SetLayeredWindowAttributes").Call(hwnd, 0, uintptr(alpha), 2)
	if r == 0 {
		return fmt.Errorf("窗口透明度: %w", e)
	}
	return nil
}

// Clip the HWND as well as the WebView content: an opaque native background
// otherwise remains visible beyond CSS corners and below the collapsed divider.
func RoundWindow(hwnd uintptr, collapsedHeight int, pixelRatio, radius float64) error {
	if root := peekRoot(hwnd); root != hwnd {
		if err := roundWindow(root, collapsedHeight, pixelRatio, radius); err != nil {
			return err
		}
	}
	return roundWindow(hwnd, collapsedHeight, pixelRatio, radius)
}

func roundWindow(hwnd uintptr, collapsedHeight int, pixelRatio, radius float64) error {
	var bounds nativeRect
	if r, _, e := user32.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&bounds))); r == 0 {
		return e
	}
	dpi, _, _ := user32.NewProc("GetDpiForWindow").Call(hwnd)
	if dpi == 0 {
		dpi = 96
	}
	scale := float64(dpi) / 96
	if pixelRatio >= .25 && pixelRatio <= 8 {
		scale = pixelRatio
	}
	width, height := int(bounds.Right-bounds.Left), int(bounds.Bottom-bounds.Top)
	if collapsedHeight > 0 {
		height = min(height, int(math.Ceil(float64(collapsedHeight)*scale)))
	}
	diameter := min(width, height, int(math.Round(2*max(0, min(24, radius))*scale)))
	gdi := win.NewLazySystemDLL("gdi32.dll")
	region, _, e := gdi.NewProc("CreateRoundRectRgn").Call(0, 0, uintptr(width), uintptr(height), uintptr(diameter), uintptr(diameter))
	if region == 0 {
		return fmt.Errorf("创建窗口圆角失败: %w", e)
	}
	if r, _, err := user32.NewProc("SetWindowRgn").Call(hwnd, region, 1); r == 0 {
		gdi.NewProc("DeleteObject").Call(region)
		return fmt.Errorf("裁切窗口圆角失败: %w", err)
	}
	return nil // Windows owns the region after SetWindowRgn succeeds.
}
func BeginWindowDrag(hwnd uintptr) error {
	hwnd = peekRoot(hwnd)
	user32.NewProc("ReleaseCapture").Call()
	user32.NewProc("SendMessageW").Call(hwnd, 0xa1, 2, 0)
	return nil
}

type desktopSearch struct {
	pid      uint32
	iconHost uintptr
	worker   uintptr
}

func windowClass(hwnd uintptr) string {
	var name [128]uint16
	user32.NewProc("GetClassNameW").Call(hwnd, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
	return win.UTF16ToString(name[:])
}
func childWindow(parent, after uintptr, class string) uintptr {
	h, _, _ := user32.NewProc("FindWindowExW").Call(parent, after, uintptr(unsafe.Pointer(win.StringToUTF16Ptr(class))), 0)
	return h
}
func sameShellProcess(hwnd uintptr, pid uint32) bool {
	var actual uint32
	user32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&actual)))
	return actual == pid
}

// Callbacks are allocated once; repeated remounts must not exhaust Go's Windows callback table.
var desktopSearchCallback = syscall.NewCallback(func(top uintptr, search *desktopSearch) uintptr {
	if !sameShellProcess(top, search.pid) {
		return 1
	}
	class := windowClass(top)
	if class != "Progman" && class != "WorkerW" {
		return 1
	}
	if childWindow(top, 0, "SHELLDLL_DefView") != 0 {
		search.iconHost = top
		// Classic Explorer puts the wallpaper WorkerW after the icon host.
		if worker := childWindow(0, top, "WorkerW"); worker != 0 && sameShellProcess(worker, search.pid) {
			search.worker = worker
		}
	}
	return 1
})

func findDesktopParent() (uintptr, error) {
	progman, _, _ := user32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(win.StringToUTF16Ptr("Progman"))), 0)
	if progman == 0 {
		return 0, fmt.Errorf("未找到 Windows 桌面；主内容窗口保持隐藏，等待桌面恢复")
	}
	search := desktopSearch{}
	user32.NewProc("GetWindowThreadProcessId").Call(progman, uintptr(unsafe.Pointer(&search.pid)))
	user32.NewProc("EnumWindows").Call(desktopSearchCallback, uintptr(unsafe.Pointer(&search)))
	runtime.KeepAlive(&search)
	// Interactive panels must be above SHELLDLL_DefView, not behind its full-screen
	// input surface on the wallpaper WorkerW. They remain children of the desktop.
	if search.iconHost != 0 {
		return search.iconHost, nil
	}
	// Recent Windows 11 versions can keep WorkerW inside Progman instead of as a sibling.
	var childWorker uintptr
	for child := childWindow(progman, 0, "WorkerW"); child != 0; child = childWindow(progman, child, "WorkerW") {
		if !sameShellProcess(child, search.pid) {
			continue
		}
		if childWindow(child, 0, "SHELLDLL_DefView") != 0 {
			search.iconHost = child
			continue
		}
		visible, _, _ := user32.NewProc("IsWindowVisible").Call(child)
		if visible != 0 && childWorker == 0 {
			childWorker = child
		}
	}
	// The icon host itself is also part of Explorer's desktop, never a normal app window.
	// This avoids repeatedly sending undocumented wallpaper-creation messages to Explorer.
	if search.iconHost != 0 {
		return search.iconHost, nil
	}
	if childWindow(progman, 0, "SHELLDLL_DefView") != 0 {
		return progman, nil
	}
	if childWorker != 0 {
		return childWorker, nil
	}
	if search.worker != 0 && IsNativeWindow(search.worker) {
		visible, _, _ := user32.NewProc("IsWindowVisible").Call(search.worker)
		if visible != 0 {
			return search.worker, nil
		}
	}
	return 0, fmt.Errorf("Windows 桌面尚未就绪；主内容窗口保持隐藏，等待桌面恢复")
}

func IsNativeWindow(hwnd uintptr) bool {
	r, _, _ := user32.NewProc("IsWindow").Call(hwnd)
	return r != 0
}
func IsDesktopAttached(hwnd uintptr) bool {
	if !IsNativeWindow(hwnd) {
		return false
	}
	style, _, _ := user32.NewProc("GetWindowLongPtrW").Call(hwnd, ^uintptr(15))
	if style&0x40000000 == 0 {
		return false
	} // WS_CHILD
	parent, _, _ := user32.NewProc("GetParent").Call(hwnd)
	if !IsNativeWindow(parent) {
		return false
	}
	class := windowClass(parent)
	return class == "Progman" || class == "WorkerW"
}
func DesktopParent(hwnd uintptr) uintptr {
	parent, _, _ := user32.NewProc("GetParent").Call(hwnd)
	return parent
}

// HWND_TOP reorders a child only among its desktop siblings. It must never be
// replaced with HWND_TOPMOST or foreground activation of an ordinary window.
func RaiseDesktopWindow(hwnd uintptr) error {
	if !IsDesktopAttached(hwnd) {
		return fmt.Errorf("窗口尚未挂载到桌面")
	}
	r, _, e := user32.NewProc("SetWindowPos").Call(hwnd, 0, 0, 0, 0, 0, 0x1|0x2|0x10)
	if r == 0 {
		return fmt.Errorf("调整桌面窗口前后顺序失败: %w", e)
	}
	return nil
}

type nativePoint struct{ X, Y int32 }
type nativeRect struct{ Left, Top, Right, Bottom int32 }

func AttachDesktop(hwnd uintptr) error {
	if IsDesktopAttached(hwnd) {
		return nil
	}
	parent, e := findDesktopParent()
	if e != nil {
		return e
	}
	var bounds nativeRect
	if r, _, err := user32.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&bounds))); r == 0 {
		return err
	}
	style, _, _ := user32.NewProc("GetWindowLongPtrW").Call(hwnd, ^uintptr(15))
	exstyle, _, _ := user32.NewProc("GetWindowLongPtrW").Call(hwnd, ^uintptr(19))
	if root := peekRoot(hwnd); root != hwnd {
		// A previous return may have waited for Explorer to recreate its desktop.
		// Dispose of the temporary host when the normal mount retry succeeds.
		return RestoreDesktopWindow(hwnd, DesktopPlacement{Parent: parent, Style: style, ExStyle: exstyle, Host: root})
	}
	// SetParent does not change WS_POPUP/WS_CHILD itself. Explicitly make this a desktop child.
	user32.NewProc("SetWindowLongPtrW").Call(hwnd, ^uintptr(15), (style&^0x80000000)|0x40000000)
	user32.NewProc("SetWindowLongPtrW").Call(hwnd, ^uintptr(19), (exstyle&^uintptr(0x40008))|0x80)
	win.NewLazySystemDLL("kernel32.dll").NewProc("SetLastError").Call(0)
	previous, _, err := user32.NewProc("SetParent").Call(hwnd, parent)
	if previous == 0 && err != syscall.Errno(0) {
		user32.NewProc("SetWindowLongPtrW").Call(hwnd, ^uintptr(15), style)
		user32.NewProc("SetWindowLongPtrW").Call(hwnd, ^uintptr(19), exstyle)
		return fmt.Errorf("桌面挂载失败，主内容窗口保持隐藏: %w", err)
	}
	if !IsDesktopAttached(hwnd) {
		return fmt.Errorf("桌面父窗口未生效，主内容窗口保持隐藏")
	}
	return SetDesktopBounds(hwnd, int(bounds.Left), int(bounds.Top), int(bounds.Right-bounds.Left), int(bounds.Bottom-bounds.Top), true)
}

// SetDesktopBounds accepts physical screen coordinates, translating them to the
// desktop parent's client coordinates before SetWindowPos. No activation occurs.
func SetDesktopBounds(hwnd uintptr, x, y, width, height int, frameChanged bool) error {
	point := nativePoint{int32(x), int32(y)}
	if IsDesktopAttached(hwnd) {
		parent := DesktopParent(hwnd)
		if r, _, e := user32.NewProc("ScreenToClient").Call(parent, uintptr(unsafe.Pointer(&point))); r == 0 {
			return e
		}
	}
	flags := uintptr(0x10 | 0x4) // SWP_NOACTIVATE | SWP_NOZORDER
	if frameChanged {
		flags |= 0x20
		flags &^= 0x4
	} // put the child above desktop siblings, below all ordinary applications
	r, _, e := user32.NewProc("SetWindowPos").Call(hwnd, 0, uintptr(point.X), uintptr(point.Y), uintptr(width), uintptr(height), flags)
	if r == 0 {
		return fmt.Errorf("调整桌面窗口失败: %w", e)
	}
	return nil
}
func ResizeDesktopWindow(hwnd uintptr, width, height int) error {
	hwnd = peekRoot(hwnd)
	dpi, _, _ := user32.NewProc("GetDpiForWindow").Call(hwnd)
	if dpi == 0 {
		dpi = 96
	}
	w := uintptr(math.Round(float64(width) * float64(dpi) / 96))
	h := uintptr(math.Round(float64(height) * float64(dpi) / 96))
	r, _, e := user32.NewProc("SetWindowPos").Call(hwnd, 0, 0, 0, w, h, 0x2|0x4|0x10)
	if r == 0 {
		return fmt.Errorf("调整窗口尺寸失败: %w", e)
	}
	return nil
}
