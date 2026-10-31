package filesystem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
)

type undoStep struct {
	Kind   string
	Source string
	Target string
	Mode   os.FileMode
}

func (m *Manager) remember(j *job, step undoStep) {
	if j.task.Operation != "move" || j.noUndo {
		return
	}
	m.mu.Lock()
	j.undo = append(j.undo, step)
	m.mu.Unlock()
}

// The journal describes completed filesystem changes, including a partially
// finished move. Undo never replaces a path created by another operation.
func (m *Manager) UndoLast() (Task, error) {
	m.mu.Lock()
	var original *job
	for i := len(m.jobs) - 1; i >= 0; i-- {
		j := m.jobs[i]
		if j.task.Operation != "move" {
			continue
		}
		if j.undoing {
			m.mu.Unlock()
			return Task{}, errors.New("正在撤销上一次转移")
		}
		active := j.task.Status == "queued" || j.task.Status == "running" || j.task.Status == "conflict"
		if len(j.undo) == 0 && !active {
			continue
		}
		if j.undoBlocked {
			m.mu.Unlock()
			return Task{}, errors.New("这次转移包含显式覆盖，无法恢复被覆盖内容")
		}
		original = j
		break
	}
	if original == nil {
		m.mu.Unlock()
		return Task{}, errors.New("没有可撤销的文件转移")
	}
	original.undoing = true
	original.cancel()
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{ctx: ctx, cancel: cancel, done: make(chan struct{}), task: Task{ID: fmt.Sprintf("undo-%d", m.seq.Add(1)), Operation: "undo", Status: "queued", Results: []Result{}}}
	m.jobs = append(m.jobs, j)
	task := j.task
	m.mu.Unlock()
	go m.runUndo(j, original)
	return task, nil
}

func (m *Manager) runUndo(j, original *job) {
	defer func() { j.cancel(); close(j.done); m.mu.Lock(); original.undoing = false; m.mu.Unlock() }()
	<-original.done
	m.mu.Lock()
	blocked := original.undoBlocked
	m.mu.Unlock()
	if blocked {
		m.mu.Lock()
		j.task.Status = "failed"
		j.task.Error = "这次转移包含显式覆盖，无法恢复被覆盖内容"
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	j.task.Total = len(original.undo)
	j.task.Status = "running"
	m.mu.Unlock()
	for {
		if j.ctx.Err() != nil {
			m.status(j, "cancelled")
			return
		}
		m.mu.Lock()
		n := len(original.undo)
		var step undoStep
		if n > 0 {
			step = original.undo[n-1]
		}
		m.mu.Unlock()
		if n == 0 {
			m.status(j, "completed")
			return
		}
		var err error
		switch step.Kind {
		case "createdDirectory":
			err = os.Remove(step.Target)
			if os.IsNotExist(err) {
				err = nil
			} // Only removes an empty directory.
		case "removedDirectory":
			err = os.Mkdir(step.Source, step.Mode)
			if os.IsExist(err) {
				if info, e := os.Lstat(step.Source); e == nil && info.IsDir() && !isLink(step.Source, info) {
					err = nil
				}
			}
		case "move":
			back := &job{ctx: j.ctx, noUndo: true, strictTarget: true, policy: "keepBoth", task: Task{Operation: "move"}}
			err = m.transfer(back, step.Target, step.Source)
		}
		if err != nil {
			m.result(j, step.Target, step.Source, "failed", err)
			m.mu.Lock()
			j.task.Error = err.Error()
			j.task.Status = "failed"
			m.mu.Unlock()
			slog.Warn("撤销文件转移未完成", "error", err)
			return
		}
		m.mu.Lock()
		original.undo = original.undo[:n-1]
		j.task.Completed++
		m.mu.Unlock()
		m.result(j, step.Target, step.Source, "completed", nil)
	}
}
