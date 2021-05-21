package twstclient

import (
	"io"
	"sync"
)

type lockedWriter struct {
	base io.Writer
	mu   sync.Mutex
}

func newLockedWriter(w io.Writer) *lockedWriter {
	return &lockedWriter{
		base: w,
	}
}

func (w *lockedWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.base.Write(p)
}
