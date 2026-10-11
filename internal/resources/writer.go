package resources

import "io"

// Writer checks disk headroom at least every 4 MiB and before larger writes.
type Writer struct {
	W       io.Writer
	Path    string
	Reserve uint64
	Check   func(string, uint64) error
	since   int64
}

func (w *Writer) Write(b []byte) (int, error) {
	if w.since == 0 || w.since+int64(len(b)) >= 4<<20 {
		check := w.Check
		if check == nil {
			check = Check
		}
		headroom := uint64(4 << 20)
		if uint64(len(b)) > headroom {
			headroom = uint64(len(b))
		}
		if err := check(w.Path, w.Reserve+headroom); err != nil {
			return 0, err
		}
		w.since = 0
	}
	n, err := w.W.Write(b)
	w.since += int64(n)
	return n, err
}
