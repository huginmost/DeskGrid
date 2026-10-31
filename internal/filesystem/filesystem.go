package filesystem

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Entry struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	IsDir    bool   `json:"isDir"`
	IsLink   bool   `json:"isLink"`
	Hidden   bool   `json:"hidden"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Icon     string `json:"icon,omitempty"`
	Error    string `json:"error,omitempty"`
}
type Listing struct {
	Entries []Entry `json:"entries"`
	Total   int     `json:"total"`
}

func entry(p string) Entry {
	e := Entry{Path: p, Name: filepath.Base(p)}
	i, err := os.Lstat(p)
	if err != nil {
		e.Error = err.Error()
		return e
	}
	return entryInfo(p, i)
}
func entryInfo(p string, i os.FileInfo) Entry {
	e := Entry{Path: p, Name: filepath.Base(p)}
	e.IsLink = isLink(p, i)
	e.IsDir = i.IsDir()
	e.Hidden = isHiddenInfo(p, i)
	e.Size = i.Size()
	e.Modified = i.ModTime().Format(time.RFC3339Nano)
	return e
}
func Stat(paths []string) []Entry {
	out := make([]Entry, 0, len(paths))
	for _, p := range paths {
		out = append(out, entry(p))
	}
	return out
}
func List(path string, showHidden bool, offset, limit int) (Listing, error) {
	out := Listing{Entries: []Entry{}}
	fi, e := os.Lstat(path)
	if e != nil {
		return out, e
	}
	if isLink(path, fi) {
		return out, errors.New("automatic expansion of reparse points is disabled")
	}
	ds, e := os.ReadDir(path)
	if e != nil {
		return out, e
	}
	for _, d := range ds {
		p := filepath.Join(path, d.Name())
		info, err := d.Info()
		var x Entry
		if err != nil {
			x = Entry{Path: p, Name: d.Name(), Error: err.Error()}
		} else {
			x = entryInfo(p, info)
		}
		if showHidden || !x.Hidden {
			out.Entries = append(out.Entries, x)
		}
	}
	sort.SliceStable(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return natural(a.Name, b.Name)
	})
	out.Total = len(out.Entries)
	if offset < 0 {
		offset = 0
	}
	if offset > out.Total {
		offset = out.Total
	}
	end := out.Total
	if limit > 0 && limit < end-offset {
		end = offset + limit
	}
	out.Entries = out.Entries[offset:end]
	return out, nil
}
func natural(a, b string) bool {
	a = strings.ToLower(a)
	b = strings.ToLower(b)
	for len(a) > 0 && len(b) > 0 {
		if a[0] >= '0' && a[0] <= '9' && b[0] >= '0' && b[0] <= '9' {
			i, j := 0, 0
			for i < len(a) && a[i] >= '0' && a[i] <= '9' {
				i++
			}
			for j < len(b) && b[j] >= '0' && b[j] <= '9' {
				j++
			}
			aa, bb := strings.TrimLeft(a[:i], "0"), strings.TrimLeft(b[:j], "0")
			if len(aa) != len(bb) {
				return len(aa) < len(bb)
			}
			if aa != bb {
				return aa < bb
			}
			a, b = a[i:], b[j:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return a < b
}
func Within(root, path string) bool {
	r, e := filepath.Abs(root)
	if e != nil {
		return false
	}
	p, e := filepath.Abs(path)
	if e != nil {
		return false
	}
	rel, e := filepath.Rel(r, p)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func ValidateName(n string) error {
	if n == "" || n == "." || n == ".." || strings.TrimRight(n, " .") != n {
		return errors.New("invalid Windows file name")
	}
	for _, r := range n {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return errors.New("invalid Windows file name")
		}
	}
	base := strings.ToUpper(strings.Split(n, ".")[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" {
		return errors.New("reserved Windows file name")
	}
	for _, prefix := range []string{"COM", "LPT"} {
		if strings.HasPrefix(base, prefix) {
			x := strings.TrimPrefix(base, prefix)
			v, e := strconv.Atoi(x)
			if e == nil && v >= 1 && v <= 9 {
				return errors.New("reserved Windows device name")
			}
			if x == "¹" || x == "²" || x == "³" {
				return errors.New("reserved Windows device name")
			}
		}
	}
	return nil
}

// Confined checks lexical bounds and the resolved parent, including junctions.
func Confined(root, path string) error {
	if !Within(root, path) {
		return errors.New("path is outside the window root")
	}
	r, e := filepath.EvalSymlinks(root)
	if e != nil {
		return e
	}
	p, e := filepath.EvalSymlinks(path)
	if e != nil {
		return e
	}
	if !Within(r, p) {
		return errors.New("reparse point escapes the window root")
	}
	return nil
}
func Rename(root, path, name string) error {
	if e := ValidateName(name); e != nil {
		return e
	}
	if filepath.Clean(root) == filepath.Clean(path) {
		return errors.New("cannot rename bound root")
	}
	if e := Confined(root, filepath.Dir(path)); e != nil {
		return e
	}
	target := filepath.Join(filepath.Dir(path), name)
	if _, e := os.Lstat(target); !os.IsNotExist(e) {
		return fmt.Errorf("target exists or cannot be checked: %s", target)
	}
	return renameNoReplace(path, target)
}
func Create(root, parent, name string, directory bool) error {
	if e := ValidateName(name); e != nil {
		return e
	}
	if e := Confined(root, parent); e != nil {
		return e
	}
	p := filepath.Join(parent, name)
	if directory {
		return os.Mkdir(p, 0755)
	}
	f, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if e != nil {
		return e
	}
	return f.Close()
}
