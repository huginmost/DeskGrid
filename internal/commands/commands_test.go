package commands

import (
	"deskgrid/internal/model"
	"testing"
)

func TestStructuredExpansion(t *testing.T) {
	c, e := Parse(`code "" %path%`, "program", []string{`C:\a b\中文.txt`}, "")
	if e != nil || len(c) != 1 || len(c[0].Args) != 2 || c[0].Args[0] != "" || c[0].Args[1] != `C:\a b\中文.txt` {
		t.Fatalf("%+v %v", c, e)
	}
	if _, e = Parse(`echo %unknown%`, "program", nil, ""); e == nil {
		t.Fatal("unknown accepted")
	}
	if _, e = Parse(`x | y`, "program", nil, ""); e == nil {
		t.Fatal("shell accepted")
	}
}

func TestMenuFieldsContinueInheritingAfterOrderOverride(t *testing.T) {
	name := "global open"
	off := false
	on := true
	custom := true
	template := "code %path%"
	g := model.DefaultGlobal()
	g.Menus["collection:file"] = model.MenuPatch{Actions: map[string]model.MenuActionPatch{"open": {Name: &name, Enabled: &off}, "custom-x": {Custom: &custom, Name: &name, Template: &template}}}
	w := model.DefaultWindow("collection", "x", "")
	w.MenuOverrides["file"] = model.MenuPatch{Order: []string{"settings", "open"}, Actions: map[string]model.MenuActionPatch{"open": {Enabled: &on}}}
	a := ResolveMenu(g, w, "file")
	if a[0].ID != "settings" || a[1].Name != name || !a[1].Enabled || a[len(a)-1].ID != "custom-x" {
		t.Fatal(a)
	}
}
func TestMultiplePathsRemainSeparateAndQuotedBackslashes(t *testing.T) {
	c, e := Parse(`"C:\Program Files\app.exe" %paths% "C:\ending\\"`, "program", []string{`C:\one a`, `C:\two&b`}, "")
	if e != nil || len(c) != 1 || len(c[0].Args) != 3 || c[0].Args[2] != `C:\ending\` {
		t.Fatal(c, e)
	}
	c, e = Parse(`code %path%`, "program", []string{"one", "two"}, "")
	if e != nil || len(c) != 2 || c[1].Args[0] != "two" {
		t.Fatal(c, e)
	}
}
func TestPowerShellSubstitutionIsLiteral(t *testing.T) {
	c, e := Parse(`Write-Output "%path%"`, "powershell", []string{`C:\O'Brien & $(evil).txt`}, "")
	if e != nil || c[0].Args[3] != `Write-Output 'C:\O''Brien & $(evil).txt'` {
		t.Fatal(c, e)
	}
}
func TestShellEmbeddedVariableFailsBeforeUnsafeExpansion(t *testing.T) {
	if _, e := Parse(`echo "prefix %path%"`, "cmd", []string{`C:\a&b`}, ""); e == nil {
		t.Fatal("embedded cmd substitution accepted")
	}
}
func TestFolderBlankMenuUsesRealDirectoryActions(t *testing.T) {
	w := model.DefaultWindow("folder", "dir", `C:\dir`)
	a := ResolveMenu(model.DefaultGlobal(), w, "blank")
	found := map[string]bool{}
	for _, v := range a {
		found[v.ID] = true
	}
	for _, id := range []string{"newFile", "newDirectory", "pin", "openNewWindow", "reveal", "cmd", "powershell", "paste", "copyPath", "copyRelativePath", "settings"} {
		if !found[id] {
			t.Errorf("missing %s", id)
		}
	}
	if found["createCollection"] || found["deleteWindow"] {
		t.Fatal("virtual-window actions in real directory context")
	}
}
func TestCustomTombstoneHidesInheritedCustomOnly(t *testing.T) {
	name := "Custom"
	template := "code %path%"
	yes := true
	no := false
	empty := ""
	g := model.DefaultGlobal()
	g.Menus["collection:file"] = model.MenuPatch{Actions: map[string]model.MenuActionPatch{"custom-x": {Custom: &yes, Name: &name, Template: &template}}}
	w := model.DefaultWindow("collection", "x", "")
	w.MenuOverrides["file"] = model.MenuPatch{Actions: map[string]model.MenuActionPatch{"custom-x": {Enabled: &no, Template: &empty}, "open": {Enabled: &no}}}
	a := ResolveMenu(g, w, "file")
	hasOpen := false
	for _, v := range a {
		if v.ID == "custom-x" {
			t.Fatal("deleted custom still present")
		}
		if v.ID == "open" {
			hasOpen = true
			if v.Enabled {
				t.Fatal("builtin hiding ignored")
			}
		}
	}
	if !hasOpen {
		t.Fatal("builtin definition disappeared")
	}
}
func TestVirtualMenusExposeLocalOrganizationActions(t *testing.T) {
	a := ResolveMenu(model.DefaultGlobal(), model.DefaultWindow("collection", "x", ""), "file")
	ids := map[string]bool{}
	for _, v := range a {
		ids[v.ID] = true
	}
	for _, id := range []string{"moveFirst", "movePrevious", "moveNext", "createGroup", "openWith"} {
		if !ids[id] {
			t.Errorf("missing %s", id)
		}
	}
}
func TestShellRejectsVariablesInsideQuotedStrings(t *testing.T) {
	for _, tc := range []struct{ mode, template string }{
		{"powershell", `Write-Output "prefix %path% suffix"`},
		{"powershell", `Write-Output 'prefix %path% suffix'`},
		{"powershell", "Write-Output \"prefix `\" %path% suffix\""},
		{"powershell", `Write-Output ""%path%`},
		{"cmd", `echo "prefix %path% suffix"`},
		{"cmd", `echo prefix^ %path%`},
		{"cmd", `echo ""%path%`},
	} {
		t.Run(tc.mode+tc.template, func(t *testing.T) {
			if _, e := Parse(tc.template, tc.mode, []string{`C:\$(Start-Process calc) & echo unsafe.txt`}, ""); e == nil {
				t.Fatal("unsafe embedded substitution accepted")
			}
		})
	}
}
func TestShellAllowsOnlyWholeVariableArguments(t *testing.T) {
	for _, template := range []string{`Write-Output %path%`, `Write-Output "%path%"`, `Write-Output '%path%'`} {
		c, e := Parse(template, "powershell", []string{`C:\$(not-executed) O'Brien.txt`}, "")
		if e != nil || c[0].Args[3] != `Write-Output 'C:\$(not-executed) O''Brien.txt'` {
			t.Fatal(c, e)
		}
	}
	c, e := Parse(`echo "" "%path%"`, "cmd", []string{`C:\a&b.txt`}, "")
	if e != nil || c[0].Args[4] != `echo "" "C:\a&b.txt"` {
		t.Fatal(c, e)
	}
}
