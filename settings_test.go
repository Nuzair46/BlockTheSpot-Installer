package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const settingsTemplate = "; Defaults\r\n[Log]\r\nLevel=0\r\n[Developer]\r\nEnable=1\r\n"

func putFile(t *testing.T, dir, name string, body []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, dir, name string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
	}
}

func TestSettingsPreservedVerbatimIncludingEmptyFile(t *testing.T) {
	for _, body := range [][]byte{{}, []byte("\xef\xbb\xbf; My preferences\r\n[Developer]\r\nEnable=0\r\n")} {
		dir := t.TempDir()
		putFile(t, dir, "settings.ini", body)
		putFile(t, dir, "config.ini", []byte("deliberately malformed"))
		got, _, err := prepareSettings(dir, []byte(settingsTemplate), false)
		if err != nil || !bytes.Equal(got, body) {
			t.Fatalf("got %q, %v", got, err)
		}
		assertFile(t, dir, "settings.ini", body)
	}
}

func TestSettingsDefaultsAndExplicitReset(t *testing.T) {
	dir := t.TempDir()
	for _, reset := range []bool{false, true} {
		if reset {
			putFile(t, dir, "settings.ini", []byte("custom"))
		}
		got, _, err := prepareSettings(dir, []byte(settingsTemplate), reset)
		if err != nil || string(got) != settingsTemplate {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	assertFile(t, dir, "settings.ini", []byte("custom")) // Planning does not reset yet.
}

func TestLegacyMigrationOnlyCopiesPreferences(t *testing.T) {
	dir := t.TempDir()
	legacy := "\ufeff;1.2.3.4\r\n[LOG]\r\nlevel=2\r\n[developer]\r\nenable=0\r\nsignature=AA BB CC\r\noffset=123\r\n[URL_block]\r\nEnable=0\r\n[Buffer_modify]\r\nEnable=0\r\n[Homepage_vbar]\r\nEnable=1\r\n[LIBCEF]\r\nBlock_crashpad=0\r\n[Compatibility]\r\nSpotify=1.2.3.4\r\n"
	putFile(t, dir, "config.ini", []byte(legacy))
	got, _, err := prepareSettings(dir, []byte(settingsTemplate), false)
	if err != nil {
		t.Fatal(err)
	}
	values, err := parseINI(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 6 || values["log"]["level"] != "2" || values["developer"]["enable"] != "0" || values["homepage_vbar"]["enable"] != "1" || values["libcef"]["block_crashpad"] != "0" {
		t.Fatalf("unexpected settings: %s", got)
	}
	if strings.Contains(string(got), "signature=") || strings.Contains(string(got), "offset") || values["compatibility"] != nil {
		t.Fatalf("migrated pack data: %s", got)
	}
	assertFile(t, dir, "config.ini", []byte(legacy))
}

func TestInvalidLegacyPreferencesRequireExplicitReset(t *testing.T) {
	for _, legacy := range []string{"[Log]\nLevel=3", "[Developer]\nEnable=yes", "[Log]\nLevel=-1", "[Log]\nLevel=0\nLevel=1", "bad ini"} {
		dir := t.TempDir()
		putFile(t, dir, "config.ini", []byte(legacy))
		if _, _, err := prepareSettings(dir, []byte(settingsTemplate), false); err == nil {
			t.Fatalf("accepted %q", legacy)
		}
		if _, _, err := prepareSettings(dir, []byte(settingsTemplate), true); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReinstallPreservesPreferencesEvenAfterSetupFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		for _, legacyOnly := range []bool{false, true} {
			dir := filepath.Join(t.TempDir(), "Spotify")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if !legacyOnly {
				putFile(t, dir, "settings.ini", []byte("; personal\r\n[Log]\r\nLevel=2\r\n"))
			}
			putFile(t, dir, "config.ini", []byte("[Developer]\nEnable=0"))
			setupErr := errors.New("setup failed")
			called := false
			err := withPreservedSettings(dir, func() error {
				called = true
				if err := os.RemoveAll(dir); err != nil {
					t.Fatal(err)
				}
				if fail {
					return setupErr
				}
				return nil
			})
			if !called || (fail && !errors.Is(err, setupErr)) || (!fail && err != nil) {
				t.Fatalf("called=%t err=%v", called, err)
			}
			if !legacyOnly {
				assertFile(t, dir, "settings.ini", []byte("; personal\r\n[Log]\r\nLevel=2\r\n"))
			}
			assertFile(t, dir, "config.ini", []byte("[Developer]\nEnable=0"))
		}
	}
}

func TestInstallReplacesPackAndKeepsPlannedSettings(t *testing.T) {
	dir := t.TempDir()
	settings := []byte("; my settings\n[Log]\nLevel=2")
	putFile(t, dir, "settings.ini", settings)
	files := map[string][]byte{"chrome_elf.dll": []byte("new proxy"), "blockthespot.dll": []byte("new hook"), "chrome_elf_required.dll": []byte("matching original"), "config.ini": []byte("new pack"), "settings.ini": settings, "settings.example.ini": []byte(settingsTemplate)}
	if err := installPatchFiles(dir, files); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		assertFile(t, dir, name, body)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != len(files) {
		t.Fatalf("unexpected files: %v, %v", entries, err)
	}
}

func TestInstallPreflightFailureLeavesFilesUntouched(t *testing.T) {
	dir := t.TempDir()
	putFile(t, dir, "blockthespot.dll", []byte("old hook"))
	if err := os.Mkdir(filepath.Join(dir, "config.ini"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := installPatchFiles(dir, map[string][]byte{"blockthespot.dll": []byte("new hook"), "config.ini": []byte("new pack")})
	if err == nil {
		t.Fatal("expected failure for config.ini directory")
	}
	assertFile(t, dir, "blockthespot.dll", []byte("old hook"))
}

func TestUninstallKeepsSettingsAndLogs(t *testing.T) {
	dir := t.TempDir()
	keep := map[string][]byte{"settings.ini": []byte("; personal settings\r\n"), "blockthespot.log": []byte("diagnostics"), "blockthespot.log.1": []byte("older diagnostics")}
	for name, body := range keep {
		putFile(t, dir, name, body)
	}
	for _, name := range []string{"chrome_elf.dll", "blockthespot.dll", "config.ini", "settings.example.ini", "blockthespot-status.txt"} {
		putFile(t, dir, name, []byte("patch data"))
	}
	original := []byte("matching original")
	putFile(t, dir, "chrome_elf_required.dll", original)
	if err := restoreSpotifyFiles(dir, filepath.Join(dir, "chrome_elf_required.dll")); err != nil {
		t.Fatal(err)
	}
	for name, body := range keep {
		assertFile(t, dir, name, body)
	}
	assertFile(t, dir, "chrome_elf.dll", original)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != len(keep)+1 {
		t.Fatalf("unexpected remaining files: %v, %v", entries, err)
	}
}
