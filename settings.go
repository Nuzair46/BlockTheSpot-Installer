package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Only runtime-supported preferences may move out of a legacy signature pack.
var preferenceKeys = []struct {
	section, key string
	maximum      int
}{
	{"Log", "Level", 2},
	{"Developer", "Enable", 1},
	{"URL_block", "Enable", 1},
	{"Buffer_modify", "Enable", 1},
	{"Homepage_vbar", "Enable", 1},
	{"LIBCEF", "Block_crashpad", 1},
}

func parseINI(body []byte) (map[string]map[string]string, error) {
	sections := make(map[string]map[string]string)
	section := ""
	for n, line := range strings.Split(strings.TrimPrefix(string(body), "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if _, exists := sections[section]; !exists {
				sections[section] = make(map[string]string)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if !ok || section == "" || key == "" {
			return nil, fmt.Errorf("invalid INI entry on line %d", n+1)
		}
		if _, exists := sections[section][key]; exists {
			return nil, fmt.Errorf("duplicate [%s] %s", section, key)
		}
		sections[section][key] = strings.TrimSpace(value)
	}
	return sections, nil
}

func prepareSettings(dir string, template []byte, reset bool) ([]byte, string, error) {
	current, err := os.ReadFile(filepath.Join(dir, "settings.ini"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("read settings.ini: %w", err)
	}
	if err == nil && !reset {
		return current, "Preserving existing settings.ini.", nil
	}
	if len(template) == 0 {
		return nil, "", errors.New("release has no settings template")
	}
	if reset {
		return template, "Resetting settings.ini to release defaults.", nil
	}
	legacy, err := os.ReadFile(filepath.Join(dir, "config.ini"))
	if errors.Is(err, os.ErrNotExist) {
		return template, "Creating settings.ini from release defaults.", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("read previous config.ini: %w", err)
	}
	values, err := parseINI(legacy)
	if err != nil {
		return nil, "", fmt.Errorf("cannot migrate config.ini preferences: %w", err)
	}
	var migrated strings.Builder
	migrated.WriteString("; Preferences migrated from config.ini. Unspecified values inherit the signature pack.\r\n")
	count := 0
	for _, pref := range preferenceKeys {
		value, exists := values[strings.ToLower(pref.section)][strings.ToLower(pref.key)]
		if !exists {
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > pref.maximum {
			return nil, "", fmt.Errorf("cannot migrate [%s] %s=%s; fix config.ini or select Reset BlockTheSpot settings to defaults", pref.section, pref.key, value)
		}
		fmt.Fprintf(&migrated, "[%s]\r\n%s=%d\r\n", pref.section, pref.key, n)
		count++
	}
	if count == 0 {
		return template, "Creating settings.ini from release defaults.", nil
	}
	return []byte(migrated.String()), "Migrating feature preferences from config.ini to settings.ini.", nil
}

// Spotify setup can replace its entire directory. Keep a disk backup outside it,
// and restore the exact bytes on success or failure before proceeding. Protect
// config.ini too, since it may still contain preferences awaiting migration.
func withPreservedSettings(dir string, operation func() error) error {
	var saved []fileSnapshot
	for _, name := range []string{"settings.ini", "config.ini"} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("back up %s: %w", name, err)
		}
		saved = append(saved, fileSnapshot{name: name, body: body, exists: true})
	}
	if len(saved) == 0 {
		return operation()
	}
	backupDir, err := os.MkdirTemp("", "blockthespot-settings-")
	if err != nil {
		return err
	}
	for _, file := range saved {
		if err := os.WriteFile(filepath.Join(backupDir, file.name), file.body, 0o600); err != nil {
			_ = os.RemoveAll(backupDir)
			return err
		}
	}
	operationErr := operation()
	var restoreErr error
	for _, file := range saved {
		path := filepath.Join(dir, file.name)
		current, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(current, file.body) {
			if err := writeFileAtomically(path, file.body); err != nil {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("restore %s: %w; saved copy: %s", file.name, err, filepath.Join(backupDir, file.name)))
			}
		}
	}
	if restoreErr != nil {
		return errors.Join(operationErr, restoreErr)
	}
	_ = os.RemoveAll(backupDir)
	return operationErr
}
