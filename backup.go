package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const backupPrefix = "arena-data-"

// Backup writes a timestamped copy of every saved account into dir and keeps the newest keep
// copies. It returns the new file's path.
func (s *Store) Backup(dir string, keep int, now time.Time) (string, error) {
	s.mu.Lock()
	list := make([]*Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		if a.Name != "" {
			list = append(list, a)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	data, err := json.MarshalIndent(list, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, backupPrefix+now.UTC().Format("20060102-150405")+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return path, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), backupPrefix) && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // the timestamp format sorts oldest first
	for len(names) > keep {
		os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
	return path, nil
}

// runBackups makes a backup now and then every interval, until the process exits.
func (s *Server) runBackups(dir string, every time.Duration, keep int) {
	for {
		if path, err := s.store.Backup(dir, keep, time.Now()); err != nil {
			log.Println("backup failed:", err)
		} else {
			log.Println("backup written:", path)
		}
		time.Sleep(every)
	}
}

// backupSettings reads ARENA_BACKUP_HOURS (default 6, 0 turns backups off) and ARENA_BACKUP_KEEP
// (default 28, a week at the default interval).
func backupSettings() (time.Duration, int, error) {
	hours, keep := 6.0, 28
	if v := os.Getenv("ARENA_BACKUP_HOURS"); v != "" {
		if _, err := fmt.Sscan(v, &hours); err != nil || hours < 0 {
			return 0, 0, fmt.Errorf("ARENA_BACKUP_HOURS must be a number of hours, 0 or more")
		}
	}
	if v := os.Getenv("ARENA_BACKUP_KEEP"); v != "" {
		if _, err := fmt.Sscan(v, &keep); err != nil || keep < 1 {
			return 0, 0, fmt.Errorf("ARENA_BACKUP_KEEP must be 1 or more")
		}
	}
	return time.Duration(hours * float64(time.Hour)), keep, nil
}
