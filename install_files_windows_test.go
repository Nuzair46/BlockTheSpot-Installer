//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestLockedProxyRollsBackAllPatchFiles(t *testing.T) {
	dir := t.TempDir()
	originals := map[string][]byte{"chrome_elf.dll": []byte("old proxy"), "blockthespot.dll": []byte("old hook"), "config.ini": []byte("old pack"), "settings.ini": []byte("personal settings")}
	for name, body := range originals {
		putFile(t, dir, name, body)
	}
	path, err := windows.UTF16PtrFromString(filepath.Join(dir, "chrome_elf.dll"))
	if err != nil {
		t.Fatal(err)
	}
	// Spotify loading the proxy prevents replacing it, even after other writes succeed.
	handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	err = installPatchFiles(dir, map[string][]byte{"chrome_elf_required.dll": []byte("new original"), "blockthespot.dll": []byte("new hook"), "config.ini": []byte("new pack"), "settings.ini": []byte("reset settings"), "settings.example.ini": []byte("new defaults"), "chrome_elf.dll": []byte("new proxy")})
	if err == nil {
		t.Fatal("expected sharing violation")
	}
	for name, body := range originals {
		assertFile(t, dir, name, body)
	}
	for _, name := range []string{"chrome_elf_required.dll", "settings.example.ini"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rollback left %s: %v", name, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != len(originals) {
		t.Fatalf("rollback leaked temporary files: %v, %v", entries, err)
	}
}

func TestInstalledSpotifyCompatibility(t *testing.T) {
	dir := os.Getenv("BTS_SPOTIFY_TEST_DIR")
	if dir == "" {
		t.Skip("set BTS_SPOTIFY_TEST_DIR for a read-only installed-client check")
	}
	body, err := os.ReadFile(filepath.Join(dir, "config.ini"))
	if err != nil {
		t.Fatal(err)
	}
	compatibility, err := compatibilityFromConfig(body)
	if err != nil {
		t.Fatal(err)
	}
	version, err := getSpotifyVersion(filepath.Join(dir, "Spotify.exe"))
	if err != nil || !compatibility.supports(version) {
		t.Fatalf("Spotify %s vs %+v: %v", version, compatibility, err)
	}
	path, err := matchingOriginalDLL(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Spotify %s; matching original: %s", version, path)
}
