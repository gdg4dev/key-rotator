package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// The request log (the dashboard's Activity page) is kept in activity.json
// next to the config file, so it survives restarts and updates. It is saved
// together with the config: every minute and on shutdown. Entries hold the
// key's alias, never its value.

func activityPath(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), "activity.json")
}

// snapshot returns the entries oldest first, as they are stored.
func (l *LogBuffer) snapshot() []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]LogEntry(nil), l.items...)
}

// restore replaces the entries, keeping only the newest l.max.
func (l *LogBuffer) restore(items []LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(items) > l.max {
		items = items[len(items)-l.max:]
	}
	l.items = append([]LogEntry(nil), items...)
}

// loadActivity reads the saved request log. A missing or damaged file just
// means an empty log; it never stops the server from starting.
func (a *App) loadActivity() {
	data, err := os.ReadFile(activityPath(a.store.Path()))
	if err != nil {
		return
	}
	var items []LogEntry
	if err := json.Unmarshal(data, &items); err != nil {
		log.Printf("activity log unreadable, starting empty: %v", err)
		return
	}
	a.logs.restore(items)
}

// saveActivity writes the request log atomically, readable by the owner only.
func (a *App) saveActivity() error {
	data, err := json.Marshal(a.logs.snapshot())
	if err != nil {
		return err
	}
	path := activityPath(a.store.Path())
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// persist saves the config and the request log, logging rather than failing.
func (a *App) persist(when string) {
	if err := a.store.Save(); err != nil {
		log.Printf("save config%s: %v", when, err)
	}
	if err := a.saveActivity(); err != nil {
		log.Printf("save activity log%s: %v", when, err)
	}
}
