package index

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceDelay = 2 * time.Second

// Watcher incrementally keeps the index in sync with filesystem changes
// under libraryPath, debouncing bursts of events per file.
type Watcher struct {
	fsw         *fsnotify.Watcher
	libraryPath string
	db          *DB
	saver       CoverSaver

	mu     sync.Mutex
	timers map[string]*time.Timer
}

// NewWatcher creates a watcher and registers all existing subdirectories of libraryPath.
// fsnotify does not watch recursively, so each directory must be added explicitly.
func NewWatcher(libraryPath string, db *DB, saver CoverSaver) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		fsw:         fsw,
		libraryPath: libraryPath,
		db:          db,
		saver:       saver,
		timers:      make(map[string]*time.Timer),
	}

	err = filepath.WalkDir(libraryPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if werr := fsw.Add(path); werr != nil {
				log.Printf("watch: failed to watch %s: %v", path, werr)
			}
		}
		return nil
	})
	if err != nil {
		fsw.Close()
		return nil, err
	}

	return w, nil
}

// Run processes filesystem events until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	defer w.fsw.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("watch: error: %v", err)
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	// New directories need to be watched too, so files added inside them are seen.
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			if err := w.fsw.Add(event.Name); err != nil {
				log.Printf("watch: failed to watch new dir %s: %v", event.Name, err)
			}
			return
		}
	}

	if !strings.EqualFold(filepath.Ext(event.Name), ".epub") {
		return
	}

	rel, err := filepath.Rel(w.libraryPath, event.Name)
	if err != nil {
		return
	}

	w.debounced(rel, func() {
		if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
			if err := w.db.DeleteByPath(rel); err != nil {
				log.Printf("watch: delete %s: %v", rel, err)
			}
			return
		}
		if err := w.db.UpsertPath(w.libraryPath, rel, w.saver); err != nil {
			// File may have been removed between the event firing and this running
			// (e.g. a rename shows up as create+delete); treat as non-fatal.
			log.Printf("watch: upsert %s: %v", rel, err)
		}
	})
}

func (w *Watcher) debounced(key string, fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if t, ok := w.timers[key]; ok {
		t.Stop()
	}
	w.timers[key] = time.AfterFunc(debounceDelay, func() {
		fn()
		w.mu.Lock()
		delete(w.timers, key)
		w.mu.Unlock()
	})
}
