package app

import (
	"fmt"
	"deskgrid/internal/model"
	"deskgrid/internal/shortcuts"
)

func shortPin(w *model.Window, paths []string, group string) ([]string, error) {
	return shortcuts.Pin(w, paths, group)
}
func mutate(w *model.Window, p params) error {
	return shortcuts.Mutate(w, p.Operation, p.IDs, p.Name, p.GroupID, p.Index)
}
func transfer(a, b *model.Window, p params) error {
	if p.TargetEntryID == "" {
		return shortcuts.Transfer(a, b, p.IDs, p.Copy, p.GroupID, p.Index)
	}
	if p.GroupID != "" {
		return fmt.Errorf("不能同时指定应用文件夹和成组入口")
	}
	source, target := model.Clone(*a), model.Clone(*b)
	previousIDs := entryIDs(target.Items)
	if err := shortcuts.Transfer(&source, &target, p.IDs, p.Copy, "", len(target.Items)); err != nil {
		return err
	}
	if err := groupDroppedEntries(&target, newEntryIDs(target.Items, previousIDs), p.TargetEntryID); err != nil {
		return err
	}
	*a, *b = source, target
	return nil
}

func entryIDs(items []model.Entry) map[string]bool {
	ids := map[string]bool{}
	for _, item := range items {
		ids[item.ID] = true
	}
	return ids
}

func newEntryIDs(items []model.Entry, previous map[string]bool) []string {
	ids := []string{}
	for _, item := range items {
		if !previous[item.ID] {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func groupDroppedEntries(w *model.Window, ids []string, targetID string) error {
	for index, item := range w.Items {
		if item.ID != targetID {
			continue
		}
		if item.Type != "path" {
			return fmt.Errorf("成组目标必须是普通入口")
		}
		if len(ids) == 0 {
			return nil
		}
		return shortcuts.Mutate(w, "group", append([]string{targetID}, ids...), "", "", index)
	}
	return fmt.Errorf("成组目标入口不存在")
}
