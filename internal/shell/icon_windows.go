//go:build windows

package shell

import (
	"bytes"
	"fmt"
	"golang.org/x/sys/windows"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"
)

type shFileInfo struct {
	Icon        uintptr
	Index       int32
	Attributes  uint32
	DisplayName [260]uint16
	TypeName    [80]uint16
}
type bitmapInfo struct {
	Size                   uint32
	Width, Height          int32
	Planes, BitCount       uint16
	Compression, SizeImage uint32
	XPels, YPels           int32
	Colors, Important      uint32
}
type stockIconInfo struct {
	Size                       uint32
	Icon                       uintptr
	SystemIndex, ResourceIndex int32
	Path                       [260]uint16
}
type iconRecord struct {
	stamp int64
	data  []byte
	time  time.Time
}

var iconCache = struct {
	sync.Mutex
	items map[string]iconRecord
}{items: map[string]iconRecord{}}

// FileIcon reads icon resources without executing programs or resolving shortcuts for launch.
func FileIcon(path string) ([]byte, error) {
	return FileIconSize(path, 48)
}

func FileIconSize(path string, pixels int) ([]byte, error) {
	if pixels <= 48 {
		pixels = 48
	} else {
		pixels = 256
	}
	st, e := os.Stat(path)
	if e != nil {
		return nil, e
	}
	extension := strings.ToLower(filepath.Ext(path))
	if !st.IsDir() && sharedImageType(extension) {
		// These are file-type icons, not thumbnails. Query the association once
		// instead of invoking an individual image's Shell provider for every row.
		return associatedIcon(extension, true, pixels)
	}
	stamp := st.ModTime().UnixNano()
	key := fmt.Sprintf("%s\x00%d", path, pixels)
	if data, ok := cachedIcon(key, stamp); ok {
		return data, nil
	}
	done := initCOM()
	defer done()
	if extension == ".lnk" {
		if target, err := shortcutTargetPath(path); err == nil && strings.EqualFold(filepath.Ext(target), ".exe") {
			// A shortcut can specify artwork with an arrow baked into it. Once
			// its executable is known, use only that file's icon pipeline, even
			// on failure; falling back to the .lnk would restore the arrow.
			data, err := FileIconSize(target, pixels)
			if err != nil {
				return nil, fmt.Errorf("读取快捷方式目标图标: %w", err)
			}
			storeIcon(key, stamp, data)
			return data, nil
		}
	}
	individual := !st.IsDir() && (extension == ".exe" || extension == ".dll" || extension == ".ico" || extension == ".lnk")
	if individual {
		// The Shell image list can contain a generic executable icon even when
		// the file contains complete, valid application artwork. Prefer the
		// file's own resources before consulting that cache.
		if data, err := resourceIcon(path, extension, pixels); err == nil {
			storeIcon(key, stamp, data)
			return data, nil
		}
	}
	info, e := shellIcon(path, 0, 0x100|0x4000) // HICON plus system image-list index
	var data []byte
	if e == nil {
		data, e = shellIconPNG(info, pixels)
	}
	if e != nil && !st.IsDir() && extension != "" && !individual {
		data, e = associatedIcon(extension, false, pixels)
	}
	if e != nil {
		return nil, e
	}
	storeIcon(key, stamp, data)
	return data, nil
}

func resourceIcon(path, extension string, pixels int) ([]byte, error) {
	location, index := path, int32(0)
	if extension == ".lnk" {
		var info shFileInfo
		r, _, err := shellDLL.NewProc("SHGetFileInfoW").Call(uintptr(unsafe.Pointer(u16(path))), 0, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0x1000) // SHGFI_ICONLOCATION
		if r == 0 {
			return nil, fmt.Errorf("读取快捷方式图标位置: %w", err)
		}
		location, index = windows.UTF16ToString(info.DisplayName[:]), info.Index
		if location == "" {
			return nil, fmt.Errorf("快捷方式未提供文件图标位置")
		}
		buffer := make([]uint16, 260)
		n, err := windows.ExpandEnvironmentStrings(u16(location), &buffer[0], uint32(len(buffer)))
		if err != nil {
			return nil, err
		}
		if n > uint32(len(buffer)) {
			if n > 32768 {
				return nil, fmt.Errorf("快捷方式图标路径过长")
			}
			buffer = make([]uint16, n)
			if _, err = windows.ExpandEnvironmentStrings(u16(location), &buffer[0], n); err != nil {
				return nil, err
			}
		}
		location = windows.UTF16ToString(buffer)
	}
	extract := user.NewProc("PrivateExtractIconsW")
	if err := extract.Find(); err != nil {
		return nil, err
	}
	sizes := []int{48, 32}
	if pixels > 48 {
		sizes = []int{256, 48, 32}
	}
	var lastError error
	for _, size := range sizes {
		var icon uintptr
		count, _, err := extract.Call(uintptr(unsafe.Pointer(u16(location))), uintptr(int64(index)), uintptr(size), uintptr(size), uintptr(unsafe.Pointer(&icon)), 0, 1, 0)
		if count == 0 || uint32(count) == ^uint32(0) || icon == 0 {
			if icon != 0 {
				user.NewProc("DestroyIcon").Call(icon)
			}
			lastError = fmt.Errorf("读取 %dpx 内嵌图标失败: %v", size, err)
			continue
		}
		data, err := iconPNG(icon) // Takes ownership, including on conversion failure.
		if err == nil {
			return data, nil
		}
		lastError = err
	}
	return nil, lastError
}

func sharedImageType(extension string) bool {
	switch extension {
	case ".png", ".apng", ".jpg", ".jpeg", ".jfif", ".gif", ".bmp", ".dib", ".tif", ".tiff", ".webp", ".avif", ".heic", ".heif", ".svg":
		return true
	}
	return false
}

func cachedIcon(key string, stamp int64) ([]byte, bool) {
	iconCache.Lock()
	cached, ok := iconCache.items[key]
	iconCache.Unlock()
	return cached.data, ok && cached.stamp == stamp && time.Since(cached.time) < time.Minute
}

func storeIcon(key string, stamp int64, data []byte) {
	iconCache.Lock()
	defer iconCache.Unlock()
	if len(iconCache.items) > 1024 {
		iconCache.items = map[string]iconRecord{}
	}
	iconCache.items[key] = iconRecord{stamp, data, time.Now()}
}

func shellIcon(path string, attributes, flags uint32) (shFileInfo, error) {
	var info shFileInfo
	r, _, e := shellDLL.NewProc("SHGetFileInfoW").Call(uintptr(unsafe.Pointer(u16(path))), uintptr(attributes), uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), uintptr(flags))
	if r == 0 || info.Icon == 0 {
		if info.Icon != 0 {
			user.NewProc("DestroyIcon").Call(info.Icon)
		}
		return shFileInfo{}, fmt.Errorf("读取文件类型图标 %s: %w", path, e)
	}
	return info, nil
}

func stockIcon(id, flags uint32) (stockIconInfo, error) {
	info := stockIconInfo{Size: uint32(unsafe.Sizeof(stockIconInfo{}))}
	r, _, _ := shellDLL.NewProc("SHGetStockIconInfo").Call(uintptr(id), uintptr(flags), uintptr(unsafe.Pointer(&info)))
	if int32(r) < 0 {
		return info, fmt.Errorf("Windows 库存图标: 0x%08X", uint32(r))
	}
	return info, nil
}

func associatedIcon(extension string, imageFile bool, pixels int) ([]byte, error) {
	key := fmt.Sprintf("\x00type:%s:%d", extension, pixels)
	if data, ok := cachedIcon(key, 0); ok {
		return data, nil
	}
	done := initCOM()
	defer done()
	// SHGFI_USEFILEATTRIBUTES bypasses per-file access and thumbnail handlers.
	info, err := shellIcon("file"+extension, 0x80, 0x100|0x10|0x4000)
	if err == nil && imageFile {
		for _, id := range []uint32{0, 1} { // SIID_DOCNOASSOC / SIID_DOCASSOC
			generic, e := stockIcon(id, 0x4000) // SHGSI_SYSICONINDEX, no HICON to free
			if e == nil && info.Index == generic.SystemIndex {
				user.NewProc("DestroyIcon").Call(info.Icon)
				info.Icon = 0
				err = fmt.Errorf("图片关联仅返回通用文档图标")
				break
			}
		}
	}
	var data []byte
	if err == nil {
		data, err = shellIconPNG(info, pixels)
	}
	if err != nil && imageFile {
		fallback, e := stockIcon(72, 0x100|0x4000) // SIID_IMAGEFILES, HICON and system index
		if e != nil {
			return nil, e
		}
		if fallback.Icon == 0 {
			return nil, fmt.Errorf("Windows 未返回图片类型图标")
		}
		data, err = shellIconPNG(shFileInfo{Icon: fallback.Icon, Index: fallback.SystemIndex}, pixels)
	}
	if err != nil {
		return nil, err
	}
	storeIcon(key, 0, data)
	return data, nil
}

// Ask Windows for its original extra-large/jumbo artwork rather than stretching
// a 32px HICON. Unsupported and non-executable shortcuts keep their own icons.
func shellIconPNG(info shFileInfo, pixels int) ([]byte, error) {
	lists := []uintptr{2} // SHIL_EXTRALARGE: 48px
	if pixels > 48 {
		lists = []uintptr{4, 2} // SHIL_JUMBO: 256px, with a 48px fallback
	}
	for _, kind := range lists {
		var list *comObject
		r, _, _ := shellDLL.NewProc("SHGetImageList").Call(kind, uintptr(unsafe.Pointer(guid("{46EB5926-582E-4017-9FDF-E8998DAA0950}"))), uintptr(unsafe.Pointer(&list)))
		if int32(r) < 0 || list == nil {
			continue
		}
		var icon uintptr
		_, err := list.call(10, uintptr(uint32(info.Index)&0x00ffffff), 1, uintptr(unsafe.Pointer(&icon))) // IImageList::GetIcon, ILD_TRANSPARENT
		list.release()
		if icon == 0 {
			continue
		}
		if err != nil {
			user.NewProc("DestroyIcon").Call(icon)
			continue
		}
		if data, err := iconPNG(icon); err == nil {
			user.NewProc("DestroyIcon").Call(info.Icon)
			return data, nil
		}
	}
	return iconPNG(info.Icon)
}

func iconDimensions(icon uintptr) (int, int) {
	info := struct {
		IsIcon, HotX, HotY uint32
		Mask, Color        uintptr
	}{}
	if r, _, _ := user.NewProc("GetIconInfo").Call(icon, uintptr(unsafe.Pointer(&info))); r == 0 {
		return 32, 32
	}
	gdi := windows.NewLazySystemDLL("gdi32.dll")
	if info.Mask != 0 {
		defer gdi.NewProc("DeleteObject").Call(info.Mask)
	}
	if info.Color != 0 {
		defer gdi.NewProc("DeleteObject").Call(info.Color)
	}
	bitmap := struct {
		Type, Width, Height, WidthBytes int32
		Planes, BitsPixel               uint16
		Bits                            uintptr
	}{}
	handle := info.Color
	if handle == 0 {
		handle = info.Mask
	}
	if r, _, _ := gdi.NewProc("GetObjectW").Call(handle, unsafe.Sizeof(bitmap), uintptr(unsafe.Pointer(&bitmap))); r == 0 {
		return 32, 32
	}
	if info.Color == 0 {
		bitmap.Height /= 2 // Monochrome icons store both masks in one bitmap.
	}
	return max(1, min(256, int(bitmap.Width))), max(1, min(256, int(bitmap.Height)))
}

// iconPNG takes ownership of the Shell HICON and converts its visible pixels.
func iconPNG(icon uintptr) ([]byte, error) {
	defer user.NewProc("DestroyIcon").Call(icon)
	width, height := iconDimensions(icon)
	gdi := windows.NewLazySystemDLL("gdi32.dll")
	dc, _, e := gdi.NewProc("CreateCompatibleDC").Call(0)
	if dc == 0 {
		return nil, fmt.Errorf("创建图标绘制上下文: %w", e)
	}
	defer gdi.NewProc("DeleteDC").Call(dc)
	header := bitmapInfo{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	bmp, _, e := gdi.NewProc("CreateDIBSection").Call(dc, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 {
		return nil, e
	}
	defer gdi.NewProc("DeleteObject").Call(bmp)
	if bits == nil {
		return nil, fmt.Errorf("Windows 未返回图标像素缓冲区")
	}
	old, _, e := gdi.NewProc("SelectObject").Call(dc, bmp)
	if old == 0 || old == ^uintptr(0) {
		return nil, fmt.Errorf("选择图标位图: %w", e)
	}
	defer gdi.NewProc("SelectObject").Call(dc, old)
	raw := unsafe.Slice((*byte)(bits), width*height*4)
	draw := func(background byte) error {
		for i := 0; i < len(raw); i += 4 {
			raw[i], raw[i+1], raw[i+2], raw[i+3] = background, background, background, 255
		}
		if r, _, err := user.NewProc("DrawIconEx").Call(dc, 0, 0, icon, uintptr(width), uintptr(height), 0, 0, 3); r == 0 {
			return fmt.Errorf("绘制文件图标: %w", err)
		}
		// Finish GDI's writes before reading the DIB pixels on this thread.
		gdi.NewProc("GdiFlush").Call()
		return nil
	}
	if e = draw(0); e != nil {
		return nil, e
	}
	black := append([]byte(nil), raw...)
	if e = draw(255); e != nil {
		return nil, e
	}
	im := image.NewNRGBA(image.Rect(0, 0, width, height))
	visible := false
	left, top, right, bottom := width, height, 0, 0
	for i := 0; i < len(raw); i += 4 {
		// Legacy mask icons can draw RGB while leaving alpha at zero. Recover
		// coverage from black/white composites instead; this also handles modern
		// premultiplied alpha icons without making their transparent pixels opaque.
		difference := max(int(raw[i])-int(black[i]), int(raw[i+1])-int(black[i+1]), int(raw[i+2])-int(black[i+2]))
		a := 255 - max(0, min(255, difference))
		if a == 0 {
			continue
		}
		visible = true
		x, y := (i/4)%width, (i/4)/width
		left, top, right, bottom = min(left, x), min(top, y), max(right, x+1), max(bottom, y+1)
		im.Pix[i] = byte(min(255, (int(black[i+2])*255+a/2)/a))
		im.Pix[i+1] = byte(min(255, (int(black[i+1])*255+a/2)/a))
		im.Pix[i+2] = byte(min(255, (int(black[i])*255+a/2)/a))
		im.Pix[i+3] = byte(a)
	}
	if !visible {
		return nil, fmt.Errorf("Windows 返回的文件图标没有可见像素")
	}
	if width >= 128 && right-left < width/3 && bottom-top < height/3 {
		return nil, fmt.Errorf("Windows 大图标仅包含小尺寸图像")
	}
	var b bytes.Buffer
	if e = png.Encode(&b, im); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}
