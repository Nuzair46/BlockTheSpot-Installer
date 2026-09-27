package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Rename into place without deleting the destination first. On Windows os.Rename
// uses MoveFileEx with REPLACE_EXISTING; an error leaves the old file intact.
func writeFileAtomically(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".bts-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(body)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

type fileSnapshot struct {
	name   string
	body   []byte
	exists bool
}

// Save all destinations before writing anything. On failure restore every file
// already written, so a locked DLL cannot leave a mixed-release installation.
func installPatchFiles(dir string, files map[string][]byte) error {
	names := []string{"chrome_elf_required.dll", "blockthespot.dll", "config.ini", "settings.example.ini", "settings.ini", "chrome_elf.dll"}
	var snapshots []fileSnapshot
	for _, name := range names {
		if _, included := files[name]; !included {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("back up %s: %w", name, err)
		}
		snapshots = append(snapshots, fileSnapshot{name, body, err == nil})
	}
	for index, previous := range snapshots {
		if err := writeFileAtomically(filepath.Join(dir, previous.name), files[previous.name]); err != nil {
			result := fmt.Errorf("install %s: %w", previous.name, err)
			for n := index - 1; n >= 0; n-- {
				old := snapshots[n]
				path := filepath.Join(dir, old.name)
				var restoreErr error
				if old.exists {
					restoreErr = writeFileAtomically(path, old.body)
				} else {
					restoreErr = os.Remove(path)
				}
				if restoreErr != nil {
					result = errors.Join(result, fmt.Errorf("restore %s: %w", old.name, restoreErr))
				}
			}
			return result
		}
	}
	return nil
}

func restoreSpotifyFiles(dir, originalPath string) error {
	original, err := os.ReadFile(originalPath)
	if err != nil {
		return err
	}
	if err := writeFileAtomically(filepath.Join(dir, "chrome_elf.dll"), original); err != nil {
		return fmt.Errorf("restore original chrome_elf.dll: %w", err)
	}
	for _, name := range []string{"blockthespot.dll", "config.ini", "settings.example.ini", "chrome_elf_required.dll", "blockthespot-status.txt"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}
	return nil
}
