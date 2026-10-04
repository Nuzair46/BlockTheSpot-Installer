package main

import (
	"bytes"
	"context"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type downloadLogger func(string, ...any)

type installerDownloadMethod struct {
	name string
	run  func(address, destination string, logf downloadLogger) error
}

// Only transfer and payload failures are retried. Local file errors stop immediately.
type installerDownloadError struct {
	status     int
	retryAfter string
	err        error
}

func (e *installerDownloadError) Error() string {
	if e.status != 0 {
		return fmt.Sprintf("HTTP %d %s: %v", e.status, http.StatusText(e.status), e.err)
	}
	return e.err.Error()
}

func (e *installerDownloadError) Unwrap() error { return e.err }

// Like SpotX, try the preferred client twice, then the alternate client once.
// All attempts keep the same version-specific URL and start with a fresh file.
func downloadSpotifyInstaller(address, target string, expectedSize int64, methods []installerDownloadMethod, logf downloadLogger, sleep func(time.Duration)) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if len(methods) == 0 {
		return errors.New("no download method available")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	logf("Download URL: %s", address)
	var lastErr error
	var failure *installerDownloadError
	for methodIndex, method := range methods {
		attempts := 2
		if methodIndex > 0 {
			attempts = 1
			logf("Switching to %s download fallback.", method.name)
		}
		for attempt := 1; attempt <= attempts; attempt++ {
			logf("Download method: %s (attempt %d/%d).", method.name, attempt, attempts)
			lastErr = attemptSpotifyDownload(address, target, expectedSize, method, logf)
			if lastErr == nil {
				logf("Spotify installer download complete; size and executable format checked.")
				return nil
			}
			logf("%s download failed: %v", method.name, lastErr)
			if !errors.As(lastErr, &failure) {
				return fmt.Errorf("%s download for %s: %w", method.name, address, lastErr)
			}
			delay := installerRetryDelay(failure.retryAfter, time.Now())
			if delay > 30*time.Second {
				return fmt.Errorf("download from %s: %w; server requested Retry-After %q; try again later", address, lastErr, failure.retryAfter)
			}
			if attempt < attempts {
				logf("Retrying in %s.", delay)
				sleep(delay)
			}
		}
		if failure.status == http.StatusTooManyRequests {
			logf("Skipping download fallback because the server returned HTTP 429.")
			break
		}
		// A Retry-After response also applies before switching clients.
		if methodIndex+1 < len(methods) && failure.retryAfter != "" {
			delay := installerRetryDelay(failure.retryAfter, time.Now())
			logf("Waiting %s before trying the fallback.", delay)
			sleep(delay)
		}
	}
	advice := "Try again later, or manually install the selected Spotify version and rerun Install / Patch."
	switch failure.status {
	case http.StatusTooManyRequests:
		advice = "The download server is rate-limiting requests. Wait before trying again."
	case http.StatusNotFound:
		advice = "The selected installer was not found. Reload the version list with Retry, or manually install that exact version."
	case http.StatusForbidden:
		advice = "Access to the download was denied. Try the logged URL in your browser or use another network."
	}
	var dnsErr *net.DNSError
	if errors.As(lastErr, &dnsErr) {
		advice = "The download host could not be resolved. Check your DNS settings or try another network."
	}
	return fmt.Errorf("download from %s: %w. %s", address, lastErr, advice)
}

func installerRetryDelay(value string, now time.Time) time.Duration {
	delay := 5 * time.Second
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		if seconds > 30 {
			return time.Minute // Stop rather than wait indefinitely or ignore Retry-After.
		}
		if seconds > 5 {
			delay = time.Duration(seconds) * time.Second
		}
	} else if when, err := http.ParseTime(value); err == nil && when.Sub(now) > delay {
		delay = when.Sub(now)
	}
	return delay
}

func attemptSpotifyDownload(address, target string, expectedSize int64, method installerDownloadMethod, logf downloadLogger) error {
	file, err := os.CreateTemp(filepath.Dir(target), ".spotify-*.download")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Close(); err != nil {
		return err
	}
	if err := method.run(address, tempPath, logf); err != nil {
		return err
	}
	if err := validateSpotifyDownload(tempPath, expectedSize); err != nil {
		return err
	}
	return os.Rename(tempPath, target)
}

func validateSpotifyDownload(filename string, expectedSize int64) error {
	info, err := os.Stat(filename)
	if err != nil {
		return err
	}
	if info.Size() == 0 || (expectedSize > 0 && info.Size() != expectedSize) {
		return &installerDownloadError{err: fmt.Errorf("incomplete installer: downloaded %d bytes, catalog size %d bytes", info.Size(), expectedSize)}
	}
	file, err := pe.Open(filename)
	if err != nil {
		var fileErr *os.PathError
		if errors.As(err, &fileErr) {
			return err
		}
		return &installerDownloadError{err: fmt.Errorf("download is not a valid Windows installer: %w", err)}
	}
	defer file.Close()
	if file.OptionalHeader == nil || len(file.Sections) == 0 || file.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 || file.Characteristics&pe.IMAGE_FILE_DLL != 0 {
		return &installerDownloadError{err: errors.New("download is not a Windows executable")}
	}
	for _, section := range file.Sections {
		if int64(section.Offset)+int64(section.Size) > info.Size() {
			return &installerDownloadError{err: errors.New("downloaded executable has a truncated section")}
		}
	}
	return nil
}

func downloadInstallerHTTP(client *http.Client, address, destination string, logf downloadLogger) error {
	req, err := newDownloadRequest(address)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return &installerDownloadError{err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &installerDownloadError{status: resp.StatusCode, retryAfter: resp.Header.Get("Retry-After"), err: fmt.Errorf("server response: %s", downloadDiagnostic(string(body)))}
	}
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()
	progress := &installerDownloadProgress{Writer: file, total: resp.ContentLength, logf: logf}
	_, err = io.CopyBuffer(progress, resp.Body, make([]byte, 256*1024))
	if err != nil {
		var fileErr *os.PathError
		if errors.As(err, &fileErr) {
			return err
		}
		return &installerDownloadError{err: fmt.Errorf("interrupted download: %w", err)}
	}
	return file.Close()
}

type installerDownloadProgress struct {
	io.Writer
	total, written, nextLog int64
	logf                    downloadLogger
}

func (p *installerDownloadProgress) Write(body []byte) (int, error) {
	n, err := p.Writer.Write(body)
	p.report(p.written + int64(n))
	return n, err
}

func (p *installerDownloadProgress) report(written int64) {
	p.written = written
	if p.logf != nil && written >= p.nextLog+5*1024*1024 {
		if p.total > 0 {
			p.logf("Downloaded Spotify installer: %.1f MiB / %.1f MiB.", bytesToMiB(written), bytesToMiB(p.total))
		} else {
			p.logf("Downloaded Spotify installer: %.1f MiB.", bytesToMiB(written))
		}
		p.nextLog = written
	}
}

type downloadCommand func(context.Context, string, ...string) *exec.Cmd

func downloadInstallerCurl(command downloadCommand, curlPath, address, destination string, logf downloadLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	headerPath := destination + ".headers"
	defer os.Remove(headerPath)
	// Disable curl configuration files; keep certificate validation enabled.
	cmd := command(ctx, curlPath, "-q", "--location", "--fail", "--silent", "--show-error",
		"--connect-timeout", "15", "--max-time", "600", "--proto", "=http,https",
		"--dump-header", headerPath, "--output", destination, "--write-out", "%{http_code}", "--url", address)
	var stdout bytes.Buffer
	stderr := &downloadOutput{}
	cmd.Stdout, cmd.Stderr = &stdout, stderr
	if err := cmd.Start(); err != nil {
		return &installerDownloadError{err: fmt.Errorf("start curl: %w", err)}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	progress := &installerDownloadProgress{logf: logf}
	var runErr error
waiting:
	for {
		select {
		case runErr = <-done:
			break waiting
		case <-ticker.C:
			if info, err := os.Stat(destination); err == nil {
				progress.report(info.Size())
			}
		}
	}
	status, _ := strconv.Atoi(strings.TrimSpace(stdout.String()))
	if runErr == nil && status == http.StatusOK {
		return nil
	}
	details := downloadDiagnostic(stderr.String())
	if strings.TrimSpace(stderr.String()) == "" {
		details = fmt.Sprintf("curl result: %v", runErr)
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		details = fmt.Sprintf("curl exit code %d: %s", exitErr.ExitCode(), details)
		if exitErr.ExitCode() == 23 || exitErr.ExitCode() == 26 {
			return errors.New(details) // Local write/read failure; another client cannot fix it.
		}
	}
	// Read the final response headers, after any proxy CONNECT or redirects.
	headers := make(http.Header)
	if file, err := os.Open(headerPath); err == nil {
		body, _ := io.ReadAll(io.LimitReader(file, 64*1024))
		file.Close()
		for _, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(line, "HTTP/") {
				headers = make(http.Header)
			} else if key, value, ok := strings.Cut(line, ":"); ok {
				headers.Set(strings.TrimSpace(key), strings.TrimSpace(value))
			}
		}
	}
	if status == http.StatusOK {
		status = 0 // The request succeeded, but transferring its body failed.
	}
	return &installerDownloadError{status: status, retryAfter: headers.Get("Retry-After"), err: errors.New(details)}
}

// Keep server/process diagnostics bounded and on a single activity-log line.
type downloadOutput struct{ bytes.Buffer }

func (b *downloadOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := 4096 - b.Len(); remaining > 0 {
		b.Buffer.Write(p[:min(n, remaining)])
	}
	return n, nil
}

func downloadDiagnostic(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 1024 {
		value = value[:1024] + "..."
	}
	if value == "" {
		return "no details returned"
	}
	return value
}

func bytesToMiB(value int64) float64 {
	return float64(value) / 1024 / 1024
}
