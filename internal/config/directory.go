package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultDirectory preserves existing layouts without copying, moving or
// overwriting the previous product's configuration. Explicit --config wins.
func DefaultDirectory(appData string) (string, error) {
	current := filepath.Join(appData, "DeskGrid")
	if _, err := os.Stat(current); err == nil {
		return current, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("读取 DeskGrid 配置目录: %w", err)
	}
	legacy := filepath.Join(appData, "sticker")
	if info, err := os.Stat(legacy); err == nil {
		if info.IsDir() {
			return legacy, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("读取旧版配置目录: %w", err)
	}
	return current, nil
}
