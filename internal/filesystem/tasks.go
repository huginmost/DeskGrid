package filesystem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

type Result struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
type Task struct {
	ID        string   `json:"id"`
	Operation string   `json:"operation"`
	Status    string   `json:"status"`
	Total     int      `json:"total"`
	Completed int      `json:"completed"`
	Bytes     int64    `json:"bytes"`
	Results   []Result `json:"results"`
	Error     string   `json:"error,omitempty"`
}
type resolution struct {
	choice string
	all    bool
}
type job struct {
	task         Task
	ctx          context.Context
	cancel       context.CancelFunc
	resolve      chan resolution
	policy       string
	done         chan struct{}
	undo         []undoStep
	undoing      bool
	undoBlocked  bool
	noUndo       bool
	strictTarget bool
}
type Manager struct {
	mu   sync.Mutex
	jobs []*job
	seq  atomic.Uint64
}

func NewManager() *Manager { return &Manager{} }
func (m *Manager) Tasks() []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Task, 0, len(m.jobs))
	for _, j := range m.jobs {
		x := j.task
		x.Results = append([]Result{}, x.Results...)
		out = append(out, x)
	}
	return out
}
func (m *Manager) Active() bool {
	for _, t := range m.Tasks() {
		if t.Status == "queued" || t.Status == "running" || t.Status == "conflict" {
			return true
		}
	}
	return false
}
func (m *Manager) find(id string) *job {
	for _, j := range m.jobs {
		if j.task.ID == id {
			return j
		}
	}
	return nil
}
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.find(id)
	if j == nil {
		return errors.New("unknown task")
	}
	j.cancel()
	return nil
}
func (m *Manager) Resolve(id, choice string, all bool) error {
	if choice == "cancel" {
		return m.Cancel(id)
	}
	if choice != "skip" && choice != "replace" && choice != "keepBoth" {
		return errors.New("invalid conflict choice")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.find(id)
	if j == nil || j.task.Status != "conflict" {
		return errors.New("task has no pending conflict")
	}
	select {
	case j.resolve <- resolution{choice, all}:
		return nil
	default:
		return errors.New("conflict already resolved")
	}
}
func (m *Manager) Start(paths []string, dest, operation, conflict string) (Task, error) {
	if operation != "copy" && operation != "move" {
		return Task{}, errors.New("operation must be copy or move")
	}
	if conflict == "" {
		conflict = "ask"
	}
	if conflict != "ask" && conflict != "skip" && conflict != "replace" && conflict != "keepBoth" {
		return Task{}, errors.New("invalid conflict policy")
	}
	if len(paths) == 0 {
		return Task{}, errors.New("no source paths")
	}
	d, e := filepath.EvalSymlinks(dest)
	if e != nil {
		return Task{}, e
	}
	i, e := os.Stat(d)
	if e != nil || !i.IsDir() {
		return Task{}, errors.New("destination must be a directory")
	}
	sources := make([]string, 0, len(paths))
	for _, p := range paths {
		p, e = filepath.Abs(p)
		if e != nil {
			return Task{}, e
		}
		i, e := os.Lstat(p)
		if e != nil {
			return Task{}, e
		}
		if isLink(p, i) {
			return Task{}, errors.New("reparse point transfers are not supported")
		}
		s, e := filepath.EvalSymlinks(p)
		if e != nil {
			return Task{}, e
		}
		target := filepath.Join(d, filepath.Base(p))
		if Within(s, target) || Within(s, d) {
			return Task{}, errors.New("cannot transfer onto itself or into a descendant")
		}
		for _, other := range sources {
			if Within(other, s) || Within(s, other) {
				return Task{}, errors.New("overlapping sources")
			}
		}
		sources = append(sources, s)
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{ctx: ctx, cancel: cancel, done: make(chan struct{}), resolve: make(chan resolution, 1), policy: conflict, task: Task{ID: fmt.Sprintf("file-%d", m.seq.Add(1)), Operation: operation, Status: "queued", Total: len(paths), Results: []Result{}}}
	m.mu.Lock()
	m.jobs = append(m.jobs, j)
	v := j.task
	m.mu.Unlock()
	go m.run(j, sources, d)
	return v, nil
}
func (m *Manager) status(j *job, s string) { m.mu.Lock(); j.task.Status = s; m.mu.Unlock() }
func (m *Manager) result(j *job, s, d, status string, e error) {
	r := Result{Source: s, Target: d, Status: status}
	if e != nil {
		r.Error = e.Error()
	}
	m.mu.Lock()
	j.task.Results = append(j.task.Results, r)
	m.mu.Unlock()
}
func (m *Manager) run(j *job, paths []string, d string) {
	defer func() { j.cancel(); close(j.done) }()
	m.status(j, "running")
	failed := false
	for _, s := range paths {
		if j.ctx.Err() != nil {
			m.result(j, s, filepath.Join(d, filepath.Base(s)), "cancelled", j.ctx.Err())
			m.mu.Lock()
			j.task.Completed++
			m.mu.Unlock()
			continue
		}
		e := m.transfer(j, s, filepath.Join(d, filepath.Base(s)))
		if e != nil {
			if errors.Is(e, context.Canceled) {
				m.result(j, s, filepath.Join(d, filepath.Base(s)), "cancelled", e)
			} else {
				slog.Warn("文件转移未完成", "source", s, "destination", d, "error", e)
				failed = true
				m.result(j, s, filepath.Join(d, filepath.Base(s)), "failed", e)
				m.mu.Lock()
				j.task.Error = e.Error()
				m.mu.Unlock()
			}
		}
		m.mu.Lock()
		j.task.Completed++
		m.mu.Unlock()
	}
	if j.ctx.Err() != nil {
		m.status(j, "cancelled")
	} else if failed {
		m.status(j, "failed")
	} else {
		m.status(j, "completed")
	}
}
func (m *Manager) choice(j *job, s, d string) (string, error) {
	if j.policy != "ask" {
		return j.policy, nil
	}
	m.result(j, s, d, "conflict", nil)
	m.status(j, "conflict")
	select {
	case <-j.ctx.Done():
		return "", j.ctx.Err()
	case r := <-j.resolve:
		if r.all {
			j.policy = r.choice
		}
		m.status(j, "running")
		return r.choice, nil
	}
}
func (m *Manager) transfer(j *job, s, d string) error {
	if e := j.ctx.Err(); e != nil {
		return e
	}
	si, e := os.Lstat(s)
	if e != nil {
		return e
	}
	if isLink(s, si) {
		return errors.New("reparse point encountered; target was not traversed")
	}
	di, de := os.Lstat(d)
	exists := de == nil
	if exists && j.strictTarget {
		return fmt.Errorf("撤销目标已存在，未覆盖: %s", d)
	}
	if de != nil && !os.IsNotExist(de) {
		return de
	}
	if exists && isLink(d, di) {
		return errors.New("destination reparse point is not traversed")
	}
	replace := false
	if exists && !(si.IsDir() && di.IsDir()) {
		c, e := m.choice(j, s, d)
		if e != nil {
			return e
		}
		switch c {
		case "skip":
			m.result(j, s, d, "skipped", nil)
			return nil
		case "replace":
			if si.IsDir() || di.IsDir() {
				return errors.New("cannot replace file with directory or directory with file")
			}
			replace = true
		case "keepBoth":
			ext := filepath.Ext(d)
			base := d[:len(d)-len(ext)]
			for n := 2; ; n++ {
				candidate := fmt.Sprintf("%s (%d)%s", base, n, ext)
				if _, err := os.Lstat(candidate); os.IsNotExist(err) {
					d = candidate
					exists = false
					break
				}
			}
		}
	}
	if j.task.Operation == "move" && !exists {
		if e = renameNoReplace(s, d); e == nil {
			m.remember(j, undoStep{Kind: "move", Source: s, Target: d})
			m.result(j, s, d, "completed", nil)
			return nil
		}
		// A sharing/permission failure is not a cross-volume move. Copying in
		// that case leaves duplicates or a partly dismantled source directory.
		if !crossDevice(e) {
			return e
		}
	}
	if si.IsDir() {
		if !exists {
			if e = os.Mkdir(d, si.Mode().Perm()); e != nil {
				return e
			}
			m.remember(j, undoStep{Kind: "createdDirectory", Target: d})
		}
		children, e := os.ReadDir(s)
		if e != nil {
			return e
		}
		all := true
		for _, child := range children {
			cs, cd := filepath.Join(s, child.Name()), filepath.Join(d, child.Name())
			before := m.skipped(j)
			if e = m.transfer(j, cs, cd); e != nil {
				m.result(j, cs, cd, "failed", e)
				return e
			}
			if m.skipped(j) > before {
				all = false
			}
		}
		if j.task.Operation == "move" && all {
			if e = os.Remove(s); e != nil {
				return e
			}
			m.remember(j, undoStep{Kind: "removedDirectory", Source: s, Mode: si.Mode().Perm()})
		}
		m.result(j, s, d, "completed", nil)
		return nil
	}
	// All copies stage in the destination directory, then commit exclusively.
	stage, e := os.CreateTemp(filepath.Dir(d), ".deskgrid-copy-*")
	if e != nil {
		return e
	}
	writePath := stage.Name()
	if e = stage.Close(); e != nil {
		return e
	}
	if e = os.Remove(writePath); e != nil {
		return e
	}
	committed := false
	defer func() {
		if !committed {
			os.Remove(writePath)
		}
	}()
	if e = copyFile(j.ctx, s, writePath, func(n int64) { m.mu.Lock(); j.task.Bytes += n; m.mu.Unlock() }); e != nil {
		return e
	}
	if e = j.ctx.Err(); e != nil {
		return e
	}
	if replace {
		if current, err := os.Lstat(d); err != nil || isLink(d, current) || current.IsDir() {
			return errors.New("destination changed during copy")
		}
		e = replaceFile(writePath, d)
	} else {
		e = renameNoReplace(writePath, d)
	}
	if e != nil {
		return e
	}
	committed = true
	if j.task.Operation == "move" {
		if replace {
			m.mu.Lock()
			j.undoBlocked = true
			m.mu.Unlock()
		}
		if e = os.Remove(s); e != nil {
			if !replace {
				if cleanupErr := os.Remove(d); cleanupErr != nil {
					return errors.Join(e, cleanupErr)
				}
			}
			return e
		}
		m.remember(j, undoStep{Kind: "move", Source: s, Target: d})
	}
	m.result(j, s, d, "completed", nil)
	return nil
}
func (m *Manager) skipped(j *job) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range j.task.Results {
		if r.Status == "skipped" {
			n++
		}
	}
	return n
}
