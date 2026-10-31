package shortcuts

import (
	"fmt"
	"os"
	"path/filepath"
	"deskgrid/internal/model"
	"strings"
)

func same(a, b string) bool {
	aa, _ := filepath.Abs(filepath.Clean(a))
	bb, _ := filepath.Abs(filepath.Clean(b))
	if aa == bb {
		return true
	}
	x, e := os.Stat(a)
	y, f := os.Stat(b)
	return e == nil && f == nil && os.SameFile(x, y)
}
func container(w *model.Window, id string) (*[]model.Entry, error) {
	if w.Type == "folder" {
		return nil, fmt.Errorf("folder windows contain real files")
	}
	if id == "" {
		return &w.Items, nil
	}
	for i := range w.Items {
		if w.Items[i].ID == id && w.Items[i].Type == "group" {
			return &w.Items[i].Items, nil
		}
	}
	return nil, fmt.Errorf("group not found")
}
func cleanup(w *model.Window) {
	out := []model.Entry{}
	for _, e := range w.Items {
		if e.Type != "group" || len(e.Items) > 0 {
			out = append(out, e)
		}
	}
	w.Items = out
}
func Pin(w *model.Window, paths []string, groupID string) ([]string, error) {
	v := model.Clone(*w)
	dst, e := container(&v, groupID)
	if e != nil {
		return nil, e
	}
	dups := []string{}
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			return nil, fmt.Errorf("empty path")
		}
		duplicate := false
		for _, item := range *dst {
			if item.Type == "path" && same(item.Path, p) {
				duplicate = true
				break
			}
		}
		if duplicate {
			dups = append(dups, p)
		} else {
			*dst = append(*dst, model.Entry{ID: model.NewID(), Type: "path", Path: p})
		}
	}
	*w = v
	return dups, nil
}
func selected(w *model.Window, ids []string) ([]model.Entry, error) {
	found := map[string]model.Entry{}
	for _, e := range w.Items {
		found[e.ID] = e
		for _, c := range e.Items {
			found[c.ID] = c
		}
	}
	out := []model.Entry{}
	seen := map[string]bool{}
	for _, id := range ids {
		e, ok := found[id]
		if !ok {
			return nil, fmt.Errorf("entry %s not found", id)
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate selection")
		}
		seen[id] = true
		out = append(out, e)
	}
	for _, e := range out {
		if e.Type == "group" {
			for _, c := range e.Items {
				if seen[c.ID] {
					return nil, fmt.Errorf("cannot select a group and its member")
				}
			}
		}
	}
	return out, nil
}
func remove(w *model.Window, ids []string) {
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	out := []model.Entry{}
	for _, e := range w.Items {
		if set[e.ID] {
			continue
		}
		if e.Type == "group" {
			children := []model.Entry{}
			for _, c := range e.Items {
				if !set[c.ID] {
					children = append(children, c)
				}
			}
			e.Items = children
		}
		out = append(out, e)
	}
	w.Items = out
}
func insert(dst *[]model.Entry, items []model.Entry, index int) {
	if index < 0 || index > len(*dst) {
		index = len(*dst)
	}
	tail := append([]model.Entry{}, (*dst)[index:]...)
	*dst = append(append((*dst)[:index], items...), tail...)
}
func mutate(w *model.Window, op string, ids []string, name, groupID string, index int) error {
	if w.Type == "folder" {
		return fmt.Errorf("virtual operations unavailable for folder windows")
	}
	items, e := selected(w, ids)
	if e != nil {
		return e
	}
	if len(items) == 0 {
		return fmt.Errorf("select an entry")
	}
	switch op {
	case "remove":
		remove(w, ids)
	case "rename":
		if len(ids) != 1 || strings.TrimSpace(name) == "" {
			return fmt.Errorf("rename requires one entry and a nonempty name")
		}
		for i := range w.Items {
			if w.Items[i].ID == ids[0] {
				if w.Items[i].Type == "group" {
					w.Items[i].Name = name
				} else {
					w.Items[i].DisplayName = name
				}
			}
			for j := range w.Items[i].Items {
				if w.Items[i].Items[j].ID == ids[0] {
					w.Items[i].Items[j].DisplayName = name
				}
			}
		}
	case "group":
		for _, v := range items {
			if v.Type != "path" {
				return fmt.Errorf("groups cannot nest")
			}
		}
		if name == "" {
			for n := 1; ; n++ {
				candidate := fmt.Sprintf("应用文件夹%d", n)
				exists := false
				for _, v := range w.Items {
					if v.Type == "group" && v.Name == candidate {
						exists = true
					}
				}
				if !exists {
					name = candidate
					break
				}
			}
		}
		remove(w, ids)
		insert(&w.Items, []model.Entry{{ID: model.NewID(), Type: "group", Name: name, Items: items}}, index)
	case "ungroup":
		for _, v := range items {
			if v.Type != "group" {
				return fmt.Errorf("select groups to dissolve")
			}
		}
		out := []model.Entry{}
		set := map[string]bool{}
		for _, id := range ids {
			set[id] = true
		}
		for _, v := range w.Items {
			if set[v.ID] {
				out = append(out, v.Items...)
			} else {
				out = append(out, v)
			}
		}
		for i, a := range out {
			if a.Type != "path" {
				continue
			}
			for _, b := range out[:i] {
				if b.Type == "path" && same(a.Path, b.Path) {
					return fmt.Errorf("dissolving group would create a duplicate target")
				}
			}
		}
		w.Items = out
	case "moveToGroup", "reorder":
		if _, e = container(w, groupID); e != nil {
			return e
		}
		for _, v := range items {
			if groupID != "" && v.Type == "group" {
				return fmt.Errorf("groups cannot nest")
			}
		}
		remove(w, ids)
		dst, e := container(w, groupID)
		if e != nil {
			return e
		}
		for _, v := range items {
			for _, existing := range *dst {
				if v.Type == "path" && existing.Type == "path" && same(v.Path, existing.Path) {
					return fmt.Errorf("target already pinned")
				}
			}
		}
		insert(dst, items, index)
	default:
		return fmt.Errorf("unknown operation %q", op)
	}
	cleanup(w)
	return nil
}
func Mutate(w *model.Window, operation string, ids []string, name, groupID string, index int) error {
	v := model.Clone(*w)
	if e := mutate(&v, operation, ids, name, groupID, index); e != nil {
		return e
	}
	*w = v
	return nil
}
func Transfer(source, target *model.Window, ids []string, copy bool, groupID string, index int) error {
	if source == target || source.ID == target.ID {
		return Mutate(source, "moveToGroup", ids, "", groupID, index)
	}
	s, t := model.Clone(*source), model.Clone(*target)
	if s.Type == "folder" {
		return fmt.Errorf("source must be virtual")
	}
	items, e := selected(&s, ids)
	if e != nil {
		return e
	}
	dst, e := container(&t, groupID)
	if e != nil {
		return e
	}
	for i := range items {
		if groupID != "" && items[i].Type == "group" {
			return fmt.Errorf("groups cannot nest")
		}
		for _, existing := range *dst {
			if items[i].Type == "path" && existing.Type == "path" && same(items[i].Path, existing.Path) {
				return fmt.Errorf("target already pinned")
			}
		}
		if copy {
			items[i].ID = model.NewID()
			for j := range items[i].Items {
				items[i].Items[j].ID = model.NewID()
			}
		}
	}
	insert(dst, items, index)
	if !copy {
		remove(&s, ids)
		cleanup(&s)
	}
	*source = s
	*target = t
	return nil
}
