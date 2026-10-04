//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func downloadSpotifyInstaller(choice spotifyInstallChoice, target string, logf func(string, ...any)) error {
	fallback := spotifyDownloadMethod{name: "Windows .NET", run: downloadSpotifyWithWindowsHTTP}
	d := spotifyDownloader{primary: fallback, validate: validateSpotifyInstaller, logf: logf}
	// Prefer the Windows-provided executable, never an executable from the
	// current directory or a curl alias/function supplied by a shell profile.
	curlPath, err := spotifyWindowsSystemPath("curl.exe")
	if err != nil {
		return err
	}
	if info, err := os.Stat(curlPath); err == nil && !info.IsDir() {
		d.primary = spotifyDownloadMethod{name: "curl", run: func(ctx context.Context, address, path string) error {
			return downloadSpotifyWithCurl(ctx, curlPath, address, path)
		}}
		d.fallback = &fallback
	} else {
		d.log("Windows curl.exe is unavailable; using Windows .NET for both download attempts.")
	}
	return d.download(context.Background(), choice, target)
}

func spotifyCurlArgs(address, path, headers string) []string {
	return []string{
		"-q", // Must be first: ignore .curlrc, including unsafe user defaults.
		"--globoff", "--location", "--max-redirs", "5", "--fail", "--silent", "--show-error",
		"--proto", "=https", "--proto-redir", "=https",
		"--connect-timeout", "15", "--max-time", "600", "--retry", "0",
		"--max-filesize", strconv.FormatInt(spotifyInstallerMaxBytes, 10),
		"--user-agent", "BlockTheSpotInstaller/" + installerVersion,
		"--output", path, "--dump-header", headers, "--write-out", "%{http_code}",
		"--url", address,
	}
}

func downloadSpotifyWithCurl(ctx context.Context, executable, address, path string) error {
	headers := path + ".headers"
	defer os.Remove(headers)
	out, stderr, err := runSpotifyCommand(ctx, executable, spotifyCurlArgs(address, path, headers), nil)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	status, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	if err == nil && status == http.StatusOK {
		return nil
	}
	detail := strings.TrimSpace(string(stderr))
	if detail == "" {
		if err != nil {
			detail = err.Error()
		} else {
			detail = "download did not return HTTP 200"
		}
	}
	var exitErr *exec.ExitError
	local := errors.As(err, &exitErr) && exitErr.ExitCode() == 23 // Write error.
	return &spotifyDownloadError{status: status, retryAfter: readCurlRetryAfter(headers), local: local, err: errors.New(detail)}
}

func readCurlRetryAfter(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	value := ""
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "HTTP/") {
			value = "" // Only use headers from the last response, not a redirect.
		}
		key, text, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), "Retry-After") {
			value = strings.TrimSpace(text)
		}
	}
	return value
}

func downloadSpotifyWithWindowsHTTP(ctx context.Context, address, path string) error {
	executable, err := spotifyWindowsSystemPath("WindowsPowerShell", "v1.0", "powershell.exe")
	if err != nil {
		return &spotifyDownloadError{local: true, err: err}
	}
	out, stderr, err := runSpotifyCommand(ctx, executable,
		[]string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", spotifyWindowsDownloadScript},
		[]string{"BTS_DOWNLOAD_URL=" + address, "BTS_DOWNLOAD_PATH=" + path,
			"BTS_DOWNLOAD_LIMIT=" + strconv.FormatInt(spotifyInstallerMaxBytes, 10),
			"BTS_DOWNLOAD_AGENT=BlockTheSpotInstaller/" + installerVersion})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var result struct {
		OK         bool   `json:"ok"`
		Status     int    `json:"status"`
		RetryAfter string `json:"retryAfter"`
		Local      bool   `json:"local"`
		Message    string `json:"message"`
	}
	if decodeErr := json.Unmarshal(bytes.TrimSpace(out), &result); decodeErr != nil {
		return fmt.Errorf("Windows HTTP client failed: %v (%s)", errors.Join(err, decodeErr), strings.TrimSpace(string(stderr)))
	}
	if err == nil && result.OK && result.Status == http.StatusOK {
		return nil
	}
	if result.Message == "" {
		result.Message = "Windows HTTP client did not complete the download"
	}
	return &spotifyDownloadError{status: result.Status, retryAfter: result.RetryAfter, local: result.Local, err: errors.New(result.Message)}
}

func spotifyWindowsSystemPath(parts ...string) (string, error) {
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		return "", fmt.Errorf("locate trusted Windows system directory: %w", err)
	}
	return filepath.Join(append([]string{systemDir}, parts...)...), nil
}

func runSpotifyCommand(ctx context.Context, executable string, args, extraEnv []string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.WaitDelay = 5 * time.Second
	cmd.Env = append(os.Environ(), extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// Use the Windows/.NET HTTP stack as SpotX does, without embedding a downloader
// script fetched from the network. HttpWebRequest lets us validate every redirect
// before following it (WebClient follows HTTPS-to-HTTP redirects automatically).
// Environment parameters keep catalog URLs and user paths out of script syntax.
const spotifyWindowsDownloadScript = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
[System.Net.ServicePointManager]::SecurityProtocol = [System.Net.SecurityProtocolType]::Tls12
[System.Net.ServicePointManager]::CheckCertificateRevocationList = $true
$result = @{ ok = $false; status = 0; retryAfter = ''; local = $false; message = '' }
$response = $null; $inputStream = $null; $outputStream = $null
try {
    $uri = [Uri]$env:BTS_DOWNLOAD_URL
    $limit = [long]$env:BTS_DOWNLOAD_LIMIT
    for ($redirect = 0; $redirect -le 5; $redirect++) {
        $result.status = 0; $result.retryAfter = ''
        if ($uri.Scheme -ne 'https' -or $uri.UserInfo) { throw 'Only HTTPS URLs without credentials are allowed' }
        $request = [System.Net.HttpWebRequest]::Create($uri)
        $request.AllowAutoRedirect = $false
        $request.Timeout = 15000
        $request.ReadWriteTimeout = 30000
        $request.UserAgent = $env:BTS_DOWNLOAD_AGENT
        $response = $request.GetResponse()
        $result.status = [int]$response.StatusCode
        $result.retryAfter = [string]$response.Headers['Retry-After']
        if ($result.status -in @(301, 302, 303, 307, 308)) {
            if ($redirect -eq 5) { throw 'Too many download redirects' }
            $location = $response.Headers['Location']
            if (-not $location) { throw 'Download redirect has no Location header' }
            $uri = [Uri]::new($uri, $location)
            $response.Close(); $response = $null
            continue
        }
        if ($result.status -ne 200) { throw "Download returned HTTP $($result.status)" }
        if ($response.ContentLength -gt $limit) { throw 'Installer exceeds download size limit' }
        $inputStream = $response.GetResponseStream()
        try { $outputStream = [IO.File]::Open($env:BTS_DOWNLOAD_PATH, [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::Read) }
        catch { $result.local = $true; throw }
        $buffer = New-Object byte[] 262144
        $written = [long]0
        while (($count = $inputStream.Read($buffer, 0, $buffer.Length)) -gt 0) {
            $written += $count
            if ($written -gt $limit) { throw 'Installer exceeds download size limit' }
            try { $outputStream.Write($buffer, 0, $count) }
            catch { $result.local = $true; throw }
        }
        try { $outputStream.Dispose(); $outputStream = $null }
        catch { $result.local = $true; throw }
        if ($response.ContentLength -ge 0 -and $written -ne $response.ContentLength) { throw 'Incomplete installer response' }
        $result.ok = $true
        break
    }
} catch {
    $failure = $_.Exception
    $result.message = $failure.Message
    while ($failure -and -not ($failure -is [System.Net.WebException])) { $failure = $failure.InnerException }
    if ($failure -and $failure.Response) {
        $result.status = [int]$failure.Response.StatusCode
        $result.retryAfter = [string]$failure.Response.Headers['Retry-After']
        $failure.Response.Close()
    }
} finally {
    if ($outputStream) { $outputStream.Dispose() }
    if ($inputStream) { $inputStream.Dispose() }
    if ($response) { $response.Close() }
}
ConvertTo-Json -Compress -InputObject $result
if (-not $result.ok) { exit 1 }
`
