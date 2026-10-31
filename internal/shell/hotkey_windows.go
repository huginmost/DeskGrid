//go:build windows

package shell

import (
	"fmt"
	"strconv"
	"strings"
)

var hotkeyNames = map[uint32]string{
	8: "Backspace", 9: "Tab", 13: "Enter", 19: "Pause", 20: "CapsLock", 27: "Escape",
	32: "Space", 33: "PageUp", 34: "PageDown", 35: "End", 36: "Home",
	37: "Left", 38: "Up", 39: "Right", 40: "Down", 44: "PrintScreen", 45: "Insert", 46: "Delete",
	106: "Multiply", 107: "Add", 109: "Subtract", 110: "Decimal", 111: "Divide",
	186: "Semicolon", 187: "Plus", 188: "Comma", 189: "Minus", 190: "Period", 191: "Slash",
	192: "Backquote", 219: "BracketLeft", 220: "Backslash", 221: "BracketRight", 222: "Quote",
}

func hotkeyName(key uint32) string {
	if key >= 'A' && key <= 'Z' || key >= '0' && key <= '9' {
		return string(rune(key))
	}
	if key >= 0x70 && key <= 0x87 {
		return fmt.Sprintf("F%d", key-0x70+1)
	}
	if key >= 0x60 && key <= 0x69 {
		return fmt.Sprintf("Numpad%d", key-0x60)
	}
	return hotkeyNames[key]
}

func FormatHotkey(mods, key uint32) (string, error) {
	name := hotkeyName(key)
	if name == "" || mods&^uint32(15) != 0 {
		return "", fmt.Errorf("请选择字母、数字、F1–F24 或常用功能键作为主键")
	}
	if mods == 0 && (key < 0x70 || key > 0x87) {
		return "", fmt.Errorf("请使用 Ctrl / Alt / Shift / Win 组合键，或单独的 F1–F24")
	}
	parts := []string{}
	for _, modifier := range []struct {
		bit  uint32
		name string
	}{{2, "Ctrl"}, {1, "Alt"}, {4, "Shift"}, {8, "Win"}} {
		if mods&modifier.bit != 0 {
			parts = append(parts, modifier.name)
		}
	}
	return strings.Join(append(parts, name), "+"), nil
}

func ParseHotkey(binding string) (mods, key uint32, err error) {
	if strings.TrimSpace(binding) == "" {
		return
	}
	for _, part := range strings.Split(binding, "+") {
		p := strings.ToUpper(strings.TrimSpace(part))
		switch p {
		case "CTRL", "CONTROL":
			mods |= 2
		case "ALT":
			mods |= 1
		case "SHIFT":
			mods |= 4
		case "WIN", "META", "SUPER":
			mods |= 8
		default:
			if key != 0 {
				return 0, 0, fmt.Errorf("热键只能包含一个主键")
			}
			p = strings.TrimPrefix(p, "ARROW")
			if p == "ESC" {
				p = "ESCAPE"
			}
			if p == "RETURN" {
				p = "ENTER"
			}
			if len(p) == 1 && (p[0] >= 'A' && p[0] <= 'Z' || p[0] >= '0' && p[0] <= '9') {
				key = uint32(p[0])
			} else if number, e := strconv.Atoi(strings.TrimPrefix(p, "F")); strings.HasPrefix(p, "F") && e == nil && number >= 1 && number <= 24 {
				key = uint32(0x70 + number - 1)
			} else if number, e := strconv.Atoi(strings.TrimPrefix(p, "NUMPAD")); strings.HasPrefix(p, "NUMPAD") && e == nil && number >= 0 && number <= 9 {
				key = uint32(0x60 + number)
			} else {
				for code, name := range hotkeyNames {
					if strings.EqualFold(p, name) {
						key = code
						break
					}
				}
				if key == 0 {
					return 0, 0, fmt.Errorf("不支持的快捷键主键：%s", part)
				}
			}
		}
	}
	_, err = FormatHotkey(mods, key)
	return
}
