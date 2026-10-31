package app

import (
	"errors"
	"testing"
)

func TestSystemSettingFailureRollsBackAppliedSteps(t *testing.T) {
	value := 0
	err := applyChanges([]settingChange{{apply: func() error { value = 1; return nil }, undo: func() error { value = 0; return nil }}, {apply: func() error { return errors.New("registration denied") }, undo: func() error { return nil }}})
	if err == nil || value != 0 {
		t.Fatalf("partial settings persisted: %d %v", value, err)
	}
}
