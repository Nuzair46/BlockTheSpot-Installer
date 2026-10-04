//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const spotifyInstallerValidationTimeout = 60 * time.Second

// No downloaded executable is loaded or run here. Windows verifies the embedded
// signature (including its trust chain) before we accept its publisher/version.
func validateSpotifyInstaller(path string, choice spotifyInstallChoice) error {
	if err := validateSpotifyInstallerFile(path, choice); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), spotifyInstallerValidationTimeout)
	defer cancel()
	metadata, err := readSpotifyInstallerMetadata(ctx, path)
	if err != nil {
		return err
	}
	return validateSpotifyInstallerMetadata(metadata, choice)
}

func readSpotifyInstallerMetadata(ctx context.Context, path string) (spotifyInstallerMetadata, error) {
	// Passing the literal path through an environment variable avoids interpreting
	// quotes, wildcard characters, or PowerShell expressions in a temporary path.
	const script = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$path = $env:BLOCKTHESPOT_VALIDATION_PATH
$signature = Get-AuthenticodeSignature -LiteralPath $path -ErrorAction Stop
$subject = $null
if ($null -ne $signature.SignerCertificate) {
    $subject = [Convert]::ToBase64String($signature.SignerCertificate.SubjectName.RawData)
}
$version = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($path)
[PSCustomObject]@{
    status = [string]$signature.Status
    signatureType = [string]$signature.SignatureType
    signerSubject = $subject
    fileVersion = ('{0}.{1}.{2}.{3}' -f $version.FileMajorPart, $version.FileMinorPart, $version.FileBuildPart, $version.FilePrivatePart)
} | ConvertTo-Json -Compress`
	// Resolve the OS binary through Windows, not PATH or a caller-controlled
	// SystemRoot environment variable, because its output is our trust boundary.
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		return spotifyInstallerMetadata{}, fmt.Errorf("cannot locate Windows signature verifier: %w", err)
	}
	powershell := filepath.Join(systemDir, "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Env = append(os.Environ(), "BLOCKTHESPOT_VALIDATION_PATH="+path)
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return spotifyInstallerMetadata{}, fmt.Errorf("Spotify installer signature verification timed out or was canceled: %w", ctx.Err())
	}
	if err != nil {
		return spotifyInstallerMetadata{}, fmt.Errorf("cannot verify Spotify installer signature and version with Windows PowerShell: %w", err)
	}
	var metadata spotifyInstallerMetadata
	if err := json.Unmarshal([]byte(strings.TrimPrefix(string(out), "\ufeff")), &metadata); err != nil {
		return spotifyInstallerMetadata{}, fmt.Errorf("cannot read Spotify installer signature and version metadata: %w", err)
	}
	return metadata, nil
}
