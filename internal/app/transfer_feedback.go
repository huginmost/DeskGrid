package app

import (
	"errors"
	"os"
	"path/filepath"

	"deskgrid/internal/filesystem"
)

func (s *Service) checkTransferConflicts(p params) ([]string, error) {
	w, err := s.Store.Window(p.WindowID)
	if err != nil {
		return nil, err
	}
	if w.Type != "folder" {
		return nil, errors.New("真实文件目标必须是单文件夹窗口")
	}
	if err = filesystem.Confined(w.RootPath, p.Destination); err != nil {
		return nil, err
	}
	conflicts := []string{}
	for _, source := range p.Paths {
		if _, err = os.Lstat(source); err != nil {
			return nil, err
		}
		target := filepath.Join(p.Destination, filepath.Base(source))
		if _, err = os.Lstat(target); err == nil {
			conflicts = append(conflicts, target)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return conflicts, nil
}
