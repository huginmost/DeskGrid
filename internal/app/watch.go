package app

import (
	"deskgrid/internal/filesystem"
	"time"
)

func (s *Service) watch(id, path string) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	if s.watchers[id] == nil {
		s.watchers[id] = map[string]func(){}
	}
	if s.watchers[id][path] != nil || len(s.watchers[id]) >= 128 {
		return
	}
	cancel, e := filesystem.Watch([]string{path}, func() {
		s.watchMu.Lock()
		if old := s.changeTimers[id]; old != nil {
			old.Stop()
		}
		s.changeTimers[id] = time.AfterFunc(150*time.Millisecond, func() {
			if s.Notify != nil {
				s.Notify(id)
			}
		})
		s.watchMu.Unlock()
	})
	if e == nil {
		s.watchers[id][path] = cancel
	}
}
func (s *Service) pruneWatches(id string, keep []string) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	for path, cancel := range s.watchers[id] {
		found := false
		for _, p := range keep {
			if p == path {
				found = true
				break
			}
		}
		if !found {
			cancel()
			delete(s.watchers[id], path)
		}
	}
	if len(keep) == 0 {
		delete(s.watchers, id)
		if t := s.changeTimers[id]; t != nil {
			t.Stop()
			delete(s.changeTimers, id)
		}
	}
}
func (s *Service) stopWatches() {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	for _, paths := range s.watchers {
		for _, cancel := range paths {
			cancel()
		}
	}
	for _, t := range s.changeTimers {
		t.Stop()
	}
	s.watchers = map[string]map[string]func(){}
	s.changeTimers = map[string]*time.Timer{}
}
