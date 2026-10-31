package filesystem

type TaskState struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// States returns only the requested task summaries, without serializing all
// per-file results on each short-lived transfer feedback poll.
func (m *Manager) States(ids []string) []TaskState {
	requested := make(map[string]bool, len(ids))
	for _, id := range ids {
		requested[id] = true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []TaskState{}
	for _, j := range m.jobs {
		if requested[j.task.ID] {
			out = append(out, TaskState{ID: j.task.ID, Status: j.task.Status, Error: j.task.Error})
		}
	}
	return out
}
