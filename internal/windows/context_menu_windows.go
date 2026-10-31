//go:build windows

package windows

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	win "golang.org/x/sys/windows"
)

type PopupItem struct {
	Text      string
	Enabled   bool
	Separator bool
}

type PopupStyle struct {
	Background, Text string
	Opacity          float64
}
type popupDrawing struct {
	items                         []PopupItem
	width, height, padding        uint32
	hoverInsetX, hoverInsetY      int32
	hoverDiameter                 int32
	background, hover, line, font uintptr
	text, disabled                uint32
	opacity                       float64
	ownsFont                      bool
}
type menuInfo struct {
	Size, Mask, Style, MaxHeight uint32
	Background                   uintptr
	HelpID                       uint32
	Data                         uintptr
}
type measureItem struct {
	Type, ControlID, ItemID, Width, Height uint32
	Data                                   uintptr
}
type drawItem struct {
	Type, ControlID, ItemID, Action, State uint32
	Window, DC                             uintptr
	Rect                                   nativeRect
	Data                                   uintptr
}
type menuWindowMessage struct {
	LParam, WParam uintptr
	Message        uint32
	Window         uintptr
}

var popupOwners sync.Map
var popupThreads sync.Map
var popupCount atomic.Int32
var menuGDI = win.NewLazySystemDLL("gdi32.dll")

func MenuActive() bool { return popupCount.Load() > 0 }
func colour(value string) uint32 {
	rgb, _ := strconv.ParseUint(strings.TrimPrefix(value, "#"), 16, 32)
	return uint32((rgb>>16)&255) | uint32(rgb&0xff00) | uint32((rgb&255)<<16)
}
func blendColour(a, b uint32, amount float64) uint32 {
	var c uint32
	for shift := uint(0); shift < 24; shift += 8 {
		c |= uint32(math.Round(float64((a>>shift)&255)*(1-amount)+float64((b>>shift)&255)*amount)) << shift
	}
	return c
}
func currentThread() uintptr {
	tid, _, _ := win.NewLazySystemDLL("kernel32.dll").NewProc("GetCurrentThreadId").Call()
	return tid
}

var menuCreateHook = syscall.NewCallback(func(code, wparam, lparam uintptr) uintptr {
	if int32(code) >= 0 {
		message := (*menuWindowMessage)(unsafe.Pointer(lparam))
		if (message.Message == 1 || message.Message == 0x18 && message.WParam != 0) && windowClass(message.Window) == "#32768" {
			if value, ok := popupThreads.Load(currentThread()); ok {
				_ = SetOpacity(message.Window, value.(*popupDrawing).opacity)
			}
		}
	}
	r, _, _ := user32.NewProc("CallNextHookEx").Call(0, code, wparam, lparam)
	return r
})

// Owner drawing preserves the panel palette while retaining native menu input
// and screen-edge placement. The owner window forwards these two messages.
func MenuMessage(hwnd uintptr, msg uint32, lparam uintptr) (uintptr, bool) {
	if msg != 0x2c && msg != 0x2b {
		return 0, false
	}
	value, ok := popupOwners.Load(hwnd)
	if !ok {
		return 0, false
	}
	ctx := value.(*popupDrawing)
	if msg == 0x2c {
		item := (*measureItem)(unsafe.Pointer(lparam))
		if item.Type != 1 {
			return 0, false
		}
		index := int(item.Data) - 1
		if index < 0 || index >= len(ctx.items) {
			return 0, false
		}
		item.Width = ctx.width
		item.Height = ctx.height
		if ctx.items[index].Separator {
			item.Height = max(4, ctx.padding/2)
		}
		return 1, true
	}
	item := (*drawItem)(unsafe.Pointer(lparam))
	if item.Type != 1 {
		return 0, false
	}
	index := int(item.Data) - 1
	if index < 0 || index >= len(ctx.items) {
		return 0, false
	}
	entry := ctx.items[index]
	user32.NewProc("FillRect").Call(item.DC, uintptr(unsafe.Pointer(&item.Rect)), ctx.background)
	if item.State&1 != 0 && entry.Enabled && !entry.Separator {
		selection := item.Rect
		selection.Left += ctx.hoverInsetX
		selection.Right -= ctx.hoverInsetX
		selection.Top += ctx.hoverInsetY
		selection.Bottom -= ctx.hoverInsetY
		if selection.Right > selection.Left && selection.Bottom > selection.Top {
			region, _, _ := menuGDI.NewProc("CreateRoundRectRgn").Call(uintptr(selection.Left), uintptr(selection.Top), uintptr(selection.Right), uintptr(selection.Bottom), uintptr(ctx.hoverDiameter), uintptr(ctx.hoverDiameter))
			if region != 0 {
				menuGDI.NewProc("FillRgn").Call(item.DC, region, ctx.hover)
				menuGDI.NewProc("DeleteObject").Call(region)
			}
		}
	}
	if entry.Separator {
		line := item.Rect
		line.Left += int32(ctx.padding)
		line.Right -= int32(ctx.padding)
		line.Top = (line.Top + line.Bottom) / 2
		line.Bottom = line.Top + 1
		user32.NewProc("FillRect").Call(item.DC, uintptr(unsafe.Pointer(&line)), ctx.line)
		return 1, true
	}
	old, _, _ := menuGDI.NewProc("SelectObject").Call(item.DC, ctx.font)
	defer menuGDI.NewProc("SelectObject").Call(item.DC, old)
	menuGDI.NewProc("SetBkMode").Call(item.DC, 1)
	text := ctx.text
	if !entry.Enabled {
		text = ctx.disabled
	}
	menuGDI.NewProc("SetTextColor").Call(item.DC, uintptr(text))
	rect := item.Rect
	rect.Left += int32(ctx.padding)
	rect.Right -= int32(ctx.padding)
	label, err := win.UTF16PtrFromString(entry.Text)
	if err != nil {
		return 1, true
	}
	user32.NewProc("DrawTextW").Call(item.DC, uintptr(unsafe.Pointer(label)), ^uintptr(0), uintptr(unsafe.Pointer(&rect)), 0x20|4|0x800|0x8000)
	return 1, true
}

// A native popup can extend beyond the panel. Windows constrains it to the
// monitor work area and flips it only when the screen edge requires that.
func ContextMenu(hwnd uintptr, items []PopupItem, x, y, ratio float64, style PopupStyle) (int, error) {
	ctx := &popupDrawing{items: items, width: uint32(120 * ratio), height: uint32(math.Round(27 * ratio)), padding: uint32(math.Round(12 * ratio)), hoverInsetX: max(1, int32(math.Round(4*ratio))), hoverInsetY: max(1, int32(math.Round(2*ratio))), hoverDiameter: max(2, int32(math.Round(8*ratio))), text: colour(style.Text), opacity: style.Opacity}
	background := colour(style.Background)
	ctx.disabled = blendColour(background, ctx.text, .45)
	ctx.background, _, _ = menuGDI.NewProc("CreateSolidBrush").Call(uintptr(background))
	ctx.hover, _, _ = menuGDI.NewProc("CreateSolidBrush").Call(uintptr(blendColour(background, ctx.text, .12)))
	ctx.line, _, _ = menuGDI.NewProc("CreateSolidBrush").Call(uintptr(blendColour(background, ctx.text, .18)))
	face := win.StringToUTF16Ptr("Segoe UI")
	fontHeight := -int(math.Round(12 * ratio))
	ctx.font, _, _ = menuGDI.NewProc("CreateFontW").Call(uintptr(fontHeight), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)))
	ctx.ownsFont = ctx.font != 0
	if ctx.font == 0 {
		ctx.font, _, _ = menuGDI.NewProc("GetStockObject").Call(17)
	}
	defer func() {
		for _, brush := range []uintptr{ctx.background, ctx.hover, ctx.line} {
			if brush != 0 {
				menuGDI.NewProc("DeleteObject").Call(brush)
			}
		}
		if ctx.ownsFont {
			menuGDI.NewProc("DeleteObject").Call(ctx.font)
		}
	}()
	dc, _, _ := user32.NewProc("GetDC").Call(hwnd)
	old, _, _ := menuGDI.NewProc("SelectObject").Call(dc, ctx.font)
	defer func() {
		menuGDI.NewProc("SelectObject").Call(dc, old)
		user32.NewProc("ReleaseDC").Call(hwnd, dc)
	}()
	for _, item := range items {
		label, err := win.UTF16FromString(item.Text)
		if err != nil {
			return -1, err
		}
		var size struct{ X, Y int32 }
		menuGDI.NewProc("GetTextExtentPoint32W").Call(dc, uintptr(unsafe.Pointer(&label[0])), uintptr(len(label)-1), uintptr(unsafe.Pointer(&size)))
		ctx.width = max(ctx.width, uint32(max(0, size.X))+2*ctx.padding)
	}
	menu, _, e := user32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return -1, fmt.Errorf("创建菜单失败: %w", e)
	}
	defer user32.NewProc("DestroyMenu").Call(menu)
	popupOwners.Store(hwnd, ctx)
	defer popupOwners.Delete(hwnd)
	info := menuInfo{Size: uint32(unsafe.Sizeof(menuInfo{})), Mask: 2, Background: ctx.background}
	user32.NewProc("SetMenuInfo").Call(menu, uintptr(unsafe.Pointer(&info)))
	for i, item := range items {
		flags := uintptr(0x100)
		if !item.Enabled || item.Separator {
			flags |= 1
		}
		if r, _, err := user32.NewProc("AppendMenuW").Call(menu, flags, uintptr(i+1), uintptr(i+1)); r == 0 {
			return -1, err
		}
	}
	point := nativePoint{int32(math.Round(x * ratio)), int32(math.Round(y * ratio))}
	if r, _, err := user32.NewProc("ClientToScreen").Call(hwnd, uintptr(unsafe.Pointer(&point))); r == 0 {
		return -1, err
	}
	var monitor struct {
		Size         uint32
		Bounds, Work nativeRect
		Flags        uint32
	}
	monitor.Size = uint32(unsafe.Sizeof(monitor))
	packed := uintptr(uint64(uint32(point.X)) | uint64(uint32(point.Y))<<32)
	handle, _, _ := user32.NewProc("MonitorFromPoint").Call(packed, 2)
	if r, _, _ := user32.NewProc("GetMonitorInfoW").Call(handle, uintptr(unsafe.Pointer(&monitor))); r != 0 {
		ctx.width = min(ctx.width, uint32(max(40, monitor.Work.Right-monitor.Work.Left-8)))
	}
	tid := currentThread()
	popupThreads.Store(tid, ctx)
	popupCount.Add(1)
	defer func() { popupThreads.Delete(tid); popupCount.Add(-1) }()
	hook, _, err := user32.NewProc("SetWindowsHookExW").Call(4, menuCreateHook, 0, tid)
	if hook == 0 {
		return -1, fmt.Errorf("菜单透明度钩子: %w", err)
	}
	defer user32.NewProc("UnhookWindowsHookEx").Call(hook)
	// TPM_RETURNCMD | TPM_NONOTIFY | TPM_RIGHTBUTTON, default top-left alignment.
	chosen, _, _ := user32.NewProc("TrackPopupMenuEx").Call(menu, 0x100|0x80|2|0x4000, uintptr(point.X), uintptr(point.Y), hwnd, 0)
	if chosen == 0 {
		return -1, nil
	}
	return int(chosen) - 1, nil
}
