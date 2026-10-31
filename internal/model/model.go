package model

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
)

const SchemaVersion = 1

type Entry struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Path        string  `json:"path,omitempty"`
	DisplayName string  `json:"displayName,omitempty"`
	Name        string  `json:"name,omitempty"`
	Items       []Entry `json:"items,omitempty"`
}
type Bounds struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Width     float64 `json:"width"`
	Height    float64 `json:"height"`
	Unit      string  `json:"unit"`
	DisplayID string  `json:"displayId"`
}
type Size struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type Appearance struct {
	Opacity         float64 `json:"opacity"`
	Scale           float64 `json:"scale"`
	CornerRadius    float64 `json:"cornerRadius"`
	TitleColor      string  `json:"titleColor"`
	TitleTextColor  string  `json:"titleTextColor"`
	BackgroundColor string  `json:"backgroundColor"`
	FolderColor     string  `json:"folderColor"`
	TextColor       string  `json:"textColor"`
}
type AppearancePatch struct {
	Opacity         *float64 `json:"opacity,omitempty"`
	Scale           *float64 `json:"scale,omitempty"`
	CornerRadius    *float64 `json:"cornerRadius,omitempty"`
	TitleColor      *string  `json:"titleColor,omitempty"`
	TitleTextColor  *string  `json:"titleTextColor,omitempty"`
	BackgroundColor *string  `json:"backgroundColor,omitempty"`
	FolderColor     *string  `json:"folderColor,omitempty"`
	TextColor       *string  `json:"textColor,omitempty"`
}
type MenuAction struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Group    string `json:"group,omitempty"`
	Custom   bool   `json:"custom,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Template string `json:"template,omitempty"`
}
type MenuActionPatch struct {
	Name     *string `json:"name,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
	Custom   *bool   `json:"custom,omitempty"`
	Mode     *string `json:"mode,omitempty"`
	Template *string `json:"template,omitempty"`
}
type MenuPatch struct {
	Order   []string                   `json:"order,omitempty"`
	Actions map[string]MenuActionPatch `json:"actions,omitempty"`
}
type Window struct {
	SchemaVersion           int                  `json:"schemaVersion"`
	ID                      string               `json:"id"`
	Revision                int64                `json:"revision"`
	Type                    string               `json:"type"`
	Title                   string               `json:"title"`
	TitleMode               string               `json:"titleMode"`
	Layout                  string               `json:"layout"`
	Bounds                  Bounds               `json:"bounds"`
	ExpandedSize            Size                 `json:"expandedSize"`
	Collapsed               bool                 `json:"collapsed"`
	CollapsedHeight         float64              `json:"collapsedHeight,omitempty"`
	PositionLocked          bool                 `json:"positionLocked"`
	SkipRecycleConfirmation bool                 `json:"skipRecycleConfirmation"`
	ShowFileExtensions      bool                 `json:"showFileExtensions"`
	IconsOnly               bool                 `json:"iconsOnly"`
	Items                   []Entry              `json:"items"`
	RootPath                string               `json:"rootPath"`
	CurrentPath             string               `json:"currentPath"`
	ExpandedPaths           []string             `json:"expandedPaths"`
	AppearanceOverrides     AppearancePatch      `json:"appearanceOverrides"`
	MenuOverrides           map[string]MenuPatch `json:"menuOverrides"`
}
type Global struct {
	SchemaVersion    int                  `json:"schemaVersion"`
	Theme            string               `json:"theme"`
	Appearance       AppearancePatch      `json:"appearance"`
	Menus            map[string]MenuPatch `json:"menus"`
	HiddenFiles      bool                 `json:"hiddenFiles"`
	HideStartWindow  bool                 `json:"hideStartWindow"`
	ReducedMotion    bool                 `json:"reducedMotion"`
	DesktopMode      bool                 `json:"desktopMode"`
	AutoStart        bool                 `json:"autoStart"`
	ShellIntegration bool                 `json:"shellIntegration"`
	Hotkey           string               `json:"hotkey"`
	PeekHotkey       *string              `json:"peekHotkey,omitempty"`
}
type Session struct {
	SchemaVersion int      `json:"schemaVersion"`
	OpenWindowIDs []string `json:"openWindowIds"`
}

func NewID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func DefaultWindow(kind, title, path string) Window {
	layout := "grid"
	if kind != "collection" {
		layout = "list"
	}
	if title == "" && kind == "start" {
		title = "此电脑"
	}
	return Window{SchemaVersion: 1, ID: NewID(), Type: kind, Title: title, TitleMode: "auto", Layout: layout, Bounds: Bounds{X: 80, Y: 80, Width: 280, Height: 300, Unit: "dip"}, ExpandedSize: Size{280, 300}, Items: []Entry{}, RootPath: path, CurrentPath: path, ExpandedPaths: []string{}, MenuOverrides: map[string]MenuPatch{}}
}
func DefaultGlobal() Global {
	peek := "Win+A"
	return Global{SchemaVersion: 1, Theme: "system", Menus: map[string]MenuPatch{}, DesktopMode: true, Hotkey: "Ctrl+Alt+S", PeekHotkey: &peek}
}
func PeekBinding(g Global) string {
	if g.PeekHotkey == nil {
		return "Win+A"
	}
	return *g.PeekHotkey
}
func CollapsedWindowHeight(w Window) int {
	if w.CollapsedHeight >= 24 && w.CollapsedHeight <= 96 {
		return int(math.Ceil(w.CollapsedHeight))
	}
	return 33
}
func Clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
