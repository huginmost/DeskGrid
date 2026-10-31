package app

import (
	"errors"
	"fmt"
)

type settingChange struct {
	apply func() error
	undo  func() error
}

func applyChanges(steps []settingChange) error {
	for i, step := range steps {
		if e := step.apply(); e != nil {
			out := []error{e}
			for j := i; j >= 0; j-- {
				if rollback := steps[j].undo(); rollback != nil {
					out = append(out, fmt.Errorf("恢复系统设置失败: %w", rollback))
				}
			}
			return errors.Join(out...)
		}
	}
	return nil
}
