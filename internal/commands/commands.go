package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"deskgrid/internal/model"
	"strings"
)

type Command struct {
	Program   string   `json:"program"`
	Args      []string `json:"args"`
	Directory string   `json:"directory"`
}

// split implements the CommandLineToArgvW backslash/quote rules without a shell.
func split(s string) ([]string, error) {
	out := []string{}
	for i := 0; i < len(s); {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i == len(s) {
			break
		}
		var b strings.Builder
		quoted := false
		for i < len(s) {
			if !quoted && (s[i] == ' ' || s[i] == '\t') {
				break
			}
			slashes := 0
			for i < len(s) && s[i] == '\\' {
				slashes++
				i++
			}
			if i < len(s) && s[i] == '"' {
				b.WriteString(strings.Repeat("\\", slashes/2))
				if slashes%2 == 1 {
					b.WriteByte('"')
					i++
				} else {
					if quoted && i+1 < len(s) && s[i+1] == '"' {
						b.WriteByte('"')
						i += 2
					} else {
						quoted = !quoted
						i++
					}
				}
				continue
			}
			b.WriteString(strings.Repeat("\\", slashes))
			if i == len(s) {
				break
			}
			b.WriteByte(s[i])
			i++
		}
		if quoted {
			return nil, fmt.Errorf("unclosed quote")
		}
		out = append(out, b.String())
	}
	return out, nil
}

var variable = regexp.MustCompile(`%([^%]+)%`)

func winBase(p string) string {
	p = strings.TrimRight(p, "\\/")
	if i := strings.LastIndexAny(p, "\\/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
func directory(p string) string {
	if st, e := os.Stat(p); e == nil && st.IsDir() {
		return p
	}
	i := strings.LastIndexAny(p, "\\/")
	if i < 0 {
		return "."
	}
	if i == 2 && len(p) > 2 && p[1] == ':' {
		return p[:3]
	}
	if i == 0 {
		return p[:1]
	}
	return p[:i]
}
func expand(s, p, root string) (string, error) {
	var err error
	s = variable.ReplaceAllStringFunc(s, func(token string) string {
		switch token {
		case "%path%", "%fileName%":
			if p == "" {
				err = fmt.Errorf("target path unavailable")
			}
			return p
		case "%name%":
			if p == "" {
				err = fmt.Errorf("target path unavailable")
			}
			return winBase(p)
		case "%directory%":
			if p == "" {
				err = fmt.Errorf("target path unavailable")
			}
			return directory(p)
		case "%root%":
			if root == "" {
				err = fmt.Errorf("root unavailable outside a folder window")
			}
			return root
		default:
			err = fmt.Errorf("unknown or misplaced variable %s", token)
			return ""
		}
	})
	return s, err
}
func Parse(template, mode string, paths []string, root string) ([]Command, error) {
	if mode == "" {
		mode = "program"
	}
	if mode != "program" && mode != "cmd" && mode != "powershell" {
		return nil, fmt.Errorf("unknown command mode")
	}
	tokens, e := split(template)
	if mode != "program" {
		tokens, e = shellTokens(template, mode)
		for i, t := range tokens {
			if v := wholeVariable(t, mode); v != "" {
				tokens[i] = v
			}
		}
	}
	if e != nil {
		return nil, e
	}
	if len(tokens) == 0 || tokens[0] == "" {
		return nil, fmt.Errorf("executable required")
	}
	multi := false
	for _, t := range tokens {
		if strings.Contains(t, "%paths%") && t != "%paths%" {
			return nil, fmt.Errorf("%%paths%% must occupy a complete argument")
		}
		if t == "%paths%" {
			multi = true
		}
	}
	if tokens[0] == "%paths%" {
		return nil, fmt.Errorf("executable cannot be %%paths%%")
	}
	if mode == "program" {
		if strings.ContainsAny(template, "|&<>^\r\n") {
			return nil, fmt.Errorf("shell operators require explicit cmd or PowerShell mode")
		}
		ext := strings.ToLower(filepath.Ext(tokens[0]))
		if ext == ".cmd" || ext == ".bat" {
			return nil, fmt.Errorf("batch programs require explicit cmd/PowerShell mode or select the real .exe")
		}
	}
	targets := paths
	if multi || len(targets) == 0 {
		targets = []string{""}
		if len(paths) > 0 {
			targets[0] = paths[0]
		}
	}
	out := []Command{}
	for _, p := range targets {
		args := []string{}
		for _, t := range tokens {
			if t == "%paths%" {
				if len(paths) == 0 {
					return nil, fmt.Errorf("%%paths%% requires selected paths")
				}
				args = append(args, paths...)
			} else {
				x, e := expand(t, p, root)
				if e != nil {
					return nil, e
				}
				args = append(args, x)
			}
		}
		cwd := root
		if p != "" {
			cwd = directory(p)
		}
		if mode == "program" {
			out = append(out, Command{Program: args[0], Args: args[1:], Directory: cwd})
			continue
		}
		line, e := shellExpand(template, paths, p, root, mode)
		if e != nil {
			return nil, e
		}
		if mode == "cmd" {
			out = append(out, Command{Program: "cmd.exe", Args: []string{"/D", "/V:OFF", "/S", "/C", line}, Directory: cwd})
		} else {
			out = append(out, Command{Program: "powershell.exe", Args: []string{"-NoProfile", "-NonInteractive", "-Command", line}, Directory: cwd})
		}
	}
	return out, nil
}
func shellExpand(template string, paths []string, p, root, mode string) (string, error) {
	var err error
	quote := func(s string) string {
		if mode == "powershell" {
			return "'" + strings.ReplaceAll(s, "'", "''") + "'"
		}
		if strings.ContainsAny(s, "\r\n\x00\"%") {
			err = fmt.Errorf("cmd substitution cannot safely represent quotes, percent signs or newlines; use program or PowerShell mode")
			return ""
		}
		return `"` + s + `"`
	}
	tokens, e := shellTokens(template, mode)
	if e != nil {
		return "", e
	}
	var normalized strings.Builder
	cursor := 0
	for _, token := range tokens {
		offset := strings.Index(template[cursor:], token) + cursor
		normalized.WriteString(template[cursor:offset])
		replacement := token
		if variable.MatchString(token) {
			v := wholeVariable(token, mode)
			if v == "" {
				return "", fmt.Errorf("shell variables must occupy a complete argument; use program mode for embedded substitutions")
			}
			replacement = v
		}
		normalized.WriteString(replacement)
		cursor = offset + len(token)
	}
	normalized.WriteString(template[cursor:])
	template = normalized.String()
	line := variable.ReplaceAllStringFunc(template, func(v string) string {
		if v == "%paths%" {
			parts := []string{}
			for _, x := range paths {
				parts = append(parts, quote(x))
			}
			return strings.Join(parts, " ")
		}
		x, e := expand(v, p, root)
		if e != nil {
			err = e
		}
		return quote(x)
	})
	return line, err
}
func patch(a model.MenuAction, p model.MenuActionPatch) model.MenuAction {
	if p.Name != nil {
		a.Name = *p.Name
	}
	if p.Enabled != nil {
		a.Enabled = *p.Enabled
	}
	if p.Custom != nil {
		a.Custom = *p.Custom
	}
	if p.Mode != nil {
		a.Mode = *p.Mode
	}
	if p.Template != nil {
		a.Template = *p.Template
	}
	return a
}
func ResolveMenu(g model.Global, w model.Window, context string) []model.MenuAction {
	builtin := []model.MenuAction{}
	section := "actions"
	add := func(id, name string) {
		builtin = append(builtin, model.MenuAction{ID: id, Name: name, Enabled: true, Group: section})
	}
	directoryActions := func() {
		add("newFile", "新建文件")
		add("newDirectory", "新建文件夹")
		if context == "folder" {
			add("pin", "固定到窗口")
		} else {
			add("pin", "固定当前目录到窗口")
		}
		add("openNewWindow", "在新窗口打开")
		add("reveal", "在文件资源管理器中显示")
		add("cmd", "在 cmd 中打开")
		add("powershell", "在 PowerShell 中打开")
	}
	paths := func() {
		section = "paths"
		add("copyPath", "复制路径")
		add("copyRelativePath", "复制相对路径")
	}
	switch context {
	case "blank":
		if w.Type == "folder" {
			directoryActions()
			section = "clipboard"
			add("paste", "粘贴")
			paths()
			break
		}
		add("createCollection", "新建应用窗口")
		if w.Type != "start" {
			add("createFolder", "打开文件夹窗口")
		}
		add("renameWindow", "重命名窗口")
		if w.Type != "start" {
			add("deleteWindow", "删除窗口")
		}
		if w.Type != "folder" {
			add("toggleLayout", "切换排布")
		}
	case "group":
		if w.Type != "folder" {
			add("openGroup", "打开应用文件夹")
			add("rename", "重命名")
			add("ungroup", "解散应用文件夹")
			add("remove", "移除分组及入口")
		}
	case "file", "folder":
		add("open", "打开")
		if w.Type == "folder" {
			if context == "folder" {
				directoryActions()
			} else {
				add("openWith", "打开方式")
				add("pin", "固定到窗口")
				add("reveal", "在文件资源管理器中显示")
			}
			section = "clipboard"
			add("cut", "剪切")
			add("copy", "复制")
			if context == "folder" {
				add("paste", "粘贴")
			}
			paths()
			section = "modify"
			add("rename", "重命名")
			add("recycle", "删除")
		} else {
			add("moveFirst", "移到前面")
			add("movePrevious", "向前移动")
			add("moveNext", "向后移动")
			add("createGroup", "创建应用文件夹")
			add("moveToGroup", "移入应用文件夹")
			add("remove", "取消固定")
			section = "shell"
			add("runas", "以管理员身份运行")
			add("reveal", "打开文件位置")
		}
	default:
		return []model.MenuAction{}
	}
	section = "settings"
	add("settings", "设置")
	actions := map[string]model.MenuAction{}
	order := []string{}
	for _, a := range builtin {
		actions[a.ID] = a
		order = append(order, a.ID)
	}
	key := w.Type + ":" + context
	gp := g.Menus[key]
	wp := w.MenuOverrides[context]
	if p, ok := w.MenuOverrides[key]; ok {
		wp = p
	}
	for _, p := range []model.MenuPatch{gp, wp} {
		keys := []string{}
		for id := range p.Actions {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		for _, id := range keys {
			v := p.Actions[id]
			a, ok := actions[id]
			if !ok {
				if v.Custom == nil || !*v.Custom || v.Template == nil || v.Name == nil {
					continue
				}
				a = model.MenuAction{ID: id, Enabled: true, Custom: true, Group: "custom"}
				order = append(order, id)
			}
			actions[id] = patch(a, v)
		}
		if p.Order != nil {
			seen := map[string]bool{}
			next := []string{}
			for _, id := range p.Order {
				if _, ok := actions[id]; ok && !seen[id] {
					next = append(next, id)
					seen[id] = true
				}
			}
			for _, id := range order {
				if !seen[id] {
					next = append(next, id)
					seen[id] = true
				}
			}
			order = next
		}
	}
	result := []model.MenuAction{}
	for _, id := range order {
		a := actions[id]
		if a.Custom && !a.Enabled && a.Template == "" {
			continue
		}
		result = append(result, a)
	}
	return result
}

// shellTokens retains quoting and escaped characters while identifying complete
// arguments. Substitutions are allowed only when one whole token is a variable,
// optionally surrounded by one matching pair of quotes.
func shellTokens(s, mode string) ([]string, error) {
	out := []string{}
	start := -1
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if start < 0 {
			if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
				continue
			}
			start = i
		}
		if mode == "powershell" {
			if c == '`' && quote != '\'' {
				if i+1 >= len(s) {
					return nil, fmt.Errorf("trailing PowerShell escape")
				}
				i++
				continue
			}
			if quote == '\'' {
				if c == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						i++
						continue
					}
					quote = 0
				}
				continue
			}
			if c == '\'' && quote == 0 {
				quote = c
				continue
			}
		} else if c == '^' && quote == 0 {
			if i+1 >= len(s) {
				return nil, fmt.Errorf("trailing cmd escape")
			}
			i++
			continue
		}
		if c == '"' {
			if quote == '"' {
				quote = 0
			} else if quote == 0 {
				quote = c
			}
			continue
		}
		if quote == 0 && (c == ' ' || c == '\t' || c == '\r' || c == '\n') {
			out = append(out, s[start:i])
			start = -1
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed shell quote")
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out, nil
}
func wholeVariable(token, mode string) string {
	if len(token) >= 2 && ((token[0] == '"' && token[len(token)-1] == '"') || (mode == "powershell" && token[0] == '\'' && token[len(token)-1] == '\'')) {
		token = token[1 : len(token)-1]
	}
	switch token {
	case "%path%", "%fileName%", "%name%", "%directory%", "%root%", "%paths%":
		return token
	}
	return ""
}
