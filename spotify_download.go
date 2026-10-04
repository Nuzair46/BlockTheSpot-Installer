package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const spotifyDownloadTimeout = 10 * time.Minute

type spotifyDownloadMethod struct {
	name string
	run  func(context.Context, string, string) error
}

// Keep the status separate from diagnostic text: a rate limit must never cause
// an immediate request through the other HTTP client.
type spotifyDownloadError struct {
	status     int
	retryAfter string
	local      bool
	err        error
}

func (e *spotifyDownloadError) Error() string {
	detail := e.err.Error()
	if e.status > 0 {
		detail = fmt.Sprintf("HTTP %d: %s", e.status, detail)
	}
	if e.status == http.StatusTooManyRequests {
		detail += "; the download server is rate limiting requests; try again later"
		if e.retryAfter != "" {
			detail += " (Retry-After: " + e.retryAfter + ")"
		}
	}
	return detail
}

func (e *spotifyDownloadError) Unwrap() error { return e.err }

type spotifyDownloader struct {
	primary  spotifyDownloadMethod
	fallback *spotifyDownloadMethod
	validate func(string, spotifyInstallChoice) error
	wait     func(context.Context, time.Duration) error
	logf     func(string, ...any)
}

// Follow SpotX's bounded recovery policy: two primary-client attempts, then
// one attempt using the alternate Windows client. Both use the very same URL
// and version. A 429 stops immediately instead of bypassing a server rate limit.
func (d spotifyDownloader) download(ctx context.Context, choice spotifyInstallChoice, target string) error {
	if err := validateSpotifyDownloadURL(choice.URL); err != nil {
		return err
	}
	if d.validate == nil || d.primary.run == nil {
		return errors.New("Spotify downloader is missing a transport or validator")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create download directory: %w", err)
	}
	methods := []spotifyDownloadMethod{d.primary}
	if d.fallback != nil {
		methods = append(methods, *d.fallback)
	}
	var failures []error
	for index, method := range methods {
		attempts := 2
		if index > 0 {
			attempts = 1
			d.log("Switching to %s fallback for the same Spotify version and URL.", method.name)
		}
		for attempt := 1; attempt <= attempts; attempt++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			d.log("Download method: %s (attempt %d/%d).", method.name, attempt, attempts)
			err := d.attempt(ctx, method, choice, target)
			if err == nil {
				d.log("Spotify installer download and validation complete.")
				return nil
			}
			failure := fmt.Errorf("%s attempt %d: %w", method.name, attempt, err)
			failures = append(failures, failure)
			d.log("Download failed: %v", failure)
			var downloadErr *spotifyDownloadError
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.As(err, &downloadErr) && (downloadErr.local || downloadErr.status == http.StatusTooManyRequests) {
				return errors.Join(failures...)
			}
			if attempt < attempts {
				d.log("Retrying in 5 seconds.")
				wait := d.wait
				if wait == nil {
					wait = waitForSpotifyDownloadRetry
				}
				if err := wait(ctx, 5*time.Second); err != nil {
					return err
				}
			}
		}
	}
	return fmt.Errorf("Spotify download failed; no installer was run: %w", errors.Join(failures...))
}

func (d spotifyDownloader) attempt(ctx context.Context, method spotifyDownloadMethod, choice spotifyInstallChoice, target string) error {
	// Each attempt gets a fresh file, including when switching clients. Failed
	// downloads can never reuse partial bytes or destroy a previously valid target.
	file, err := os.CreateTemp(filepath.Dir(target), ".spotify-*.exe")
	if err != nil {
		return &spotifyDownloadError{local: true, err: fmt.Errorf("create temporary installer: %w", err)}
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Close(); err != nil {
		return &spotifyDownloadError{local: true, err: err}
	}
	attemptCtx, cancel := context.WithTimeout(ctx, spotifyDownloadTimeout)
	defer cancel()
	transferCtx, abort := context.WithCancelCause(attemptCtx)
	defer abort(nil)
	stopProgress := d.trackProgress(transferCtx, abort, temporary, choice.Size, spotifyInstallerMaxBytes)
	err = method.run(transferCtx, choice.URL, temporary)
	stopProgress()
	if cause := context.Cause(transferCtx); cause != nil {
		return cause
	}
	if err != nil {
		return err
	}
	d.log("Validating downloaded Spotify installer.")
	if err := d.validate(temporary, choice); err != nil {
		return fmt.Errorf("installer validation failed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// On Windows os.Rename replaces the target without deleting it first.
	if err := os.Rename(temporary, target); err != nil {
		return &spotifyDownloadError{local: true, err: fmt.Errorf("save validated installer: %w", err)}
	}
	return nil
}

func validateSpotifyDownloadURL(address string) error {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return errors.New("Spotify download requires an HTTPS URL without credentials")
	}
	return nil
}

func waitForSpotifyDownloadRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (d spotifyDownloader) log(format string, args ...any) {
	if d.logf != nil {
		d.logf(format, args...)
	}
}

func (d spotifyDownloader) trackProgress(ctx context.Context, abort context.CancelCauseFunc, path string, total, limit int64) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		next := int64(5 << 20)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				info, err := os.Stat(path)
				// Older Windows curl versions enforce --max-filesize only when
				// Content-Length is known. Also stop unknown-length transfers
				// that grow beyond the cap; final validation checks it exactly.
				if err == nil && info.Size() > limit {
					abort(fmt.Errorf("Spotify installer exceeds download size limit (%d bytes)", limit))
					return
				}
				if err == nil && info.Size() >= next {
					if total > 0 {
						d.log("Downloaded Spotify installer: %.1f MiB / %.1f MiB.", bytesToMiB(info.Size()), bytesToMiB(total))
					} else {
						d.log("Downloaded Spotify installer: %.1f MiB.", bytesToMiB(info.Size()))
					}
					next = info.Size() + 5<<20
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}

func bytesToMiB(value int64) float64 { return float64(value) / 1024 / 1024 }
