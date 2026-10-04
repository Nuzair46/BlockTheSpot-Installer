//go:build windows

package main

import (
	"context"
	"strings"
	"testing"
)

func TestValidateSpotifyInstallerRejectsUnsignedWindowsExecutable(t *testing.T) {
	path := writeSpotifyInstallerTestFile(t, spotifyInstallerTestPE(t))
	err := validateSpotifyInstaller(path, spotifyInstallChoice{BaseVersion: "1.3.3.264", Size: 1024})
	if err == nil || !strings.Contains(err.Error(), "signature is not trusted") {
		t.Fatalf("unsigned executable error = %v", err)
	}
}

func TestReadSpotifyInstallerMetadataCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := readSpotifyInstallerMetadata(ctx, "does-not-exist.exe")
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("canceled signature verification error = %v", err)
	}
}
