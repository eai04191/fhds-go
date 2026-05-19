// store.go — atomic Settings holder + TOML loader + fsnotify hot-reload.
package settings

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/fsnotify/fsnotify"
)

//go:embed default_config.toml
var defaultConfigTOML []byte

// DefaultConfigFilename is the filename written/read alongside the executable
// when no explicit --config path is given.
const DefaultConfigFilename = "fhds-config.toml"

// Store is an atomic holder of *Settings. Loop and DualSense read the latest
// snapshot lock-free via Get(); Watch() swaps in a new snapshot when the
// config file changes.
type Store struct {
	p atomic.Pointer[Settings]
}

// NewStore wraps an initial Settings value in a Store.
func NewStore(s Settings) *Store {
	st := &Store{}
	st.p.Store(&s)
	return st
}

// Get returns the latest snapshot. Never nil after construction.
func (s *Store) Get() *Settings { return s.p.Load() }

// set swaps in a new snapshot atomically.
func (s *Store) set(v *Settings) { s.p.Store(v) }

// DefaultConfigPath returns the config path next to the running executable.
// Falls back to the current working directory if the exe path is unavailable.
func DefaultConfigPath() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), DefaultConfigFilename)
	}
	return DefaultConfigFilename
}

// LoadOrCreate reads `path`. If it doesn't exist, the embedded default
// template is written there first. Missing fields fall back to Default().
func LoadOrCreate(path string) (Settings, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, defaultConfigTOML, 0o644); err != nil {
			return Settings{}, fmt.Errorf("write default config %s: %w", path, err)
		}
		log.Printf("Config: wrote defaults to %s", path)
	} else if err != nil {
		return Settings{}, fmt.Errorf("stat %s: %w", path, err)
	}
	return load(path)
}

func load(path string) (Settings, error) {
	s := Default()
	meta, err := toml.DecodeFile(path, &s)
	if err != nil {
		return Settings{}, fmt.Errorf("parse %s: %w", path, err)
	}
	// Unknown keys are silently dropped by the decoder; warn so a typo doesn't
	// quietly revert the intended field to its Default().
	for _, key := range meta.Undecoded() {
		log.Printf("Config: unknown key %q — typo? value falls back to built-in default", key)
	}
	return s, nil
}

// ReloadCallback is invoked after a successful reload with the previous and
// new snapshots. Use it to push live changes into the DualSense, etc.
type ReloadCallback func(prev, next *Settings)

// Watch tails `path` and atomically swaps the store on every successful
// re-parse. Parse failures are logged and the old snapshot is kept.
// Returns when ctx is cancelled.
//
// Editors often emit two write events back-to-back (or remove+create on save);
// debounce so each save triggers exactly one reload.
func Watch(ctx context.Context, path string, store *Store, onReload ReloadCallback) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify new: %w", err)
	}
	// Watch the parent directory so we still see the file when editors do
	// atomic rename-on-save (which removes and recreates the original inode).
	dir := filepath.Dir(path)
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return fmt.Errorf("fsnotify add %s: %w", dir, err)
	}

	go func() {
		defer func() { _ = w.Close() }()
		var debounce *time.Timer
		fire := make(chan struct{}, 1)
		schedule := func() {
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(150*time.Millisecond, func() {
				select {
				case fire <- struct{}{}:
				default:
				}
			})
		}
		target := filepath.Clean(path)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if filepath.Clean(ev.Name) != target {
					continue
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					schedule()
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Printf("Config: watcher error: %v", err)
			case <-fire:
				next, err := load(path)
				if err != nil {
					log.Printf("Config: reload skipped — %v", err)
					continue
				}
				prev := store.Get()
				store.set(&next)
				log.Printf("Config: reloaded %s", filepath.Base(path))
				if onReload != nil {
					onReload(prev, &next)
				}
			}
		}
	}()
	return nil
}
