package infrastructure

import "sync"

type ProjectWorkspace struct {
	mu   sync.RWMutex
	root string
}

func NewProjectWorkspace(root string) *ProjectWorkspace { return &ProjectWorkspace{root: root} }
func (w *ProjectWorkspace) Root() string                { w.mu.RLock(); defer w.mu.RUnlock(); return w.root }
func (w *ProjectWorkspace) Activate(root string) error {
	w.mu.Lock()
	w.root = root
	w.mu.Unlock()
	return nil
}
