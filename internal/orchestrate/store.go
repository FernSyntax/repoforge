package orchestrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Store is a file-backed checkpoint directory: one JSON file per run,
// written atomically (temp file plus rename, spec.md §17.4). Run ids
// are filename-safe by construction (NewRun validates them), so a
// checkpoint path can never escape the directory.
type Store struct {
	dir string
}

// NewStore creates (if needed) and binds a checkpoint directory.
func NewStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("orchestrate: checkpoint directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("orchestrate: checkpoint directory %q: %w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("orchestrate: checkpoint directory %q: %w", dir, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("orchestrate: checkpoint directory %q: %w", dir, err)
	}
	return &Store{dir: resolved}, nil
}

// Save persists the run. The document bytes stay in memory; the
// checkpoint carries the digest.
func (s *Store) Save(st *State) error {
	if !validID(st.RunID) {
		return fmt.Errorf("orchestrate: invalid run id %q", st.RunID)
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("orchestrate: encoding checkpoint: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, st.RunID+"-*.tmp")
	if err != nil {
		return fmt.Errorf("orchestrate: checkpoint temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("orchestrate: writing checkpoint: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("orchestrate: writing checkpoint: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(s.dir, st.RunID+".json")); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("orchestrate: publishing checkpoint: %w", err)
	}
	return nil
}

// Load reads a checkpoint. Unknown schema versions fail closed: a
// newer state shape is never misread as the current one.
func (s *Store) Load(runID string) (*State, error) {
	if !validID(runID) {
		return nil, fmt.Errorf("orchestrate: invalid run id %q", runID)
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, runID+".json"))
	if err != nil {
		return nil, fmt.Errorf("orchestrate: loading checkpoint %q: %w", runID, err)
	}
	st := &State{}
	if err := json.Unmarshal(raw, st); err != nil {
		return nil, fmt.Errorf("orchestrate: decoding checkpoint %q: %w", runID, err)
	}
	if st.SchemaVersion != StateVersion {
		return nil, fmt.Errorf("orchestrate: checkpoint %q declares schema %q (this tool supports %q)", runID, st.SchemaVersion, StateVersion)
	}
	return st, nil
}

// Delete removes a checkpoint. Retention policy is the operator's:
// the store keeps every run until told otherwise.
func (s *Store) Delete(runID string) error {
	if !validID(runID) {
		return fmt.Errorf("orchestrate: invalid run id %q", runID)
	}
	if err := os.Remove(filepath.Join(s.dir, runID+".json")); err != nil {
		return fmt.Errorf("orchestrate: deleting checkpoint %q: %w", runID, err)
	}
	return nil
}
