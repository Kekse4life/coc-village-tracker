// Package file is a Store backed by one JSON file per snapshot, for the
// local -history <dir> flag. Nothing is written unless that flag is set.
package file

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/you/coc-progress/internal/store"
)

var _ store.Store = (*Store)(nil)

type Store struct {
	dir string
}

// New opens (creating if needed) a directory of snapshots.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// villageDir keeps a plain <dir>/<tag> layout for the common single-user
// case, and only introduces a user segment when one is actually in play.
func (s *Store) villageDir(userID int64, tag string) string {
	safe := sanitize(tag)
	if userID == 0 {
		return filepath.Join(s.dir, safe)
	}
	return filepath.Join(s.dir, fmt.Sprintf("u%d", userID), safe)
}

func sanitize(tag string) string {
	tag = strings.TrimPrefix(tag, "#")
	if tag == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range tag {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func (s *Store) Save(_ context.Context, userID int64, tag string, capturedAt time.Time, raw []byte) error {
	dir := s.villageDir(userID, tag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fp := filepath.Join(dir, fmt.Sprintf("%d.json", capturedAt.Unix()))
	if _, err := os.Stat(fp); err == nil {
		return nil // already have this exact snapshot
	}
	tmp := fp + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, fp)
}

func (s *Store) Recent(_ context.Context, userID int64, tag string, n int) ([]store.Snapshot, error) {
	dir := s.villageDir(userID, tag)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	type file struct {
		ts   int64
		name string
	}
	var files []file
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		ts, err := strconv.ParseInt(strings.TrimSuffix(e.Name(), ".json"), 10, 64)
		if err != nil {
			continue
		}
		files = append(files, file{ts, e.Name()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ts > files[j].ts })
	if len(files) > n {
		files = files[:n]
	}

	out := make([]store.Snapshot, 0, len(files))
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil {
			continue // one unreadable file should not sink the whole list
		}
		out = append(out, store.Snapshot{Tag: tag, CapturedAt: time.Unix(f.ts, 0).UTC(), Raw: raw})
	}
	return out, nil
}

// Villages walks userID's snapshot directories and reports every one that
// has at least one snapshot file, newest capture first. The sanitized names
// it returns (missing a tag's leading '#', if any) round-trip fine as input
// to Save and Recent, since sanitizing an already-sanitized tag is a no-op -
// but are not necessarily what the export itself called the village, since
// sanitizing is lossy. A caller wanting the real tag for display should
// read it back off the newest snapshot it already has to fetch.
func (s *Store) Villages(_ context.Context, userID int64) ([]store.Village, error) {
	root := s.dir
	if userID != 0 {
		root = filepath.Join(s.dir, fmt.Sprintf("u%d", userID))
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []store.Village
	for _, v := range dirs {
		if !v.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, v.Name()))
		if err != nil {
			continue
		}
		var count int
		var newest int64
		for _, f := range files {
			if f.IsDir() || filepath.Ext(f.Name()) != ".json" {
				continue
			}
			ts, err := strconv.ParseInt(strings.TrimSuffix(f.Name(), ".json"), 10, 64)
			if err != nil {
				continue
			}
			count++
			if ts > newest {
				newest = ts
			}
		}
		if count == 0 {
			continue
		}
		out = append(out, store.Village{Tag: v.Name(), LastCapturedAt: time.Unix(newest, 0).UTC(), Snapshots: count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastCapturedAt.After(out[j].LastCapturedAt) })
	return out, nil
}

// Forget deletes every snapshot held for (userID, tag) - a "stop tracking
// this village" action available locally, where -history is an opt-in
// convenience rather than the durability guarantee Postgres provides.
func (s *Store) Forget(_ context.Context, userID int64, tag string) error {
	if err := os.RemoveAll(s.villageDir(userID, tag)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Prune walks every village directory independently, deleting snapshots
// older than cutoff but always keeping the newest one in each.
func (s *Store) Prune(_ context.Context, cutoff time.Time) (int, error) {
	removed := 0
	err := filepath.WalkDir(s.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		type file struct {
			ts   int64
			full string
		}
		var files []file
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			ts, perr := strconv.ParseInt(strings.TrimSuffix(e.Name(), ".json"), 10, 64)
			if perr != nil {
				continue
			}
			files = append(files, file{ts, filepath.Join(path, e.Name())})
		}
		if len(files) <= 1 {
			return nil
		}
		sort.Slice(files, func(i, j int) bool { return files[i].ts > files[j].ts }) // newest first
		for _, f := range files[1:] {                                               // never the newest
			if time.Unix(f.ts, 0).Before(cutoff) {
				if err := os.Remove(f.full); err == nil {
					removed++
				}
			}
		}
		return nil
	})
	return removed, err
}
