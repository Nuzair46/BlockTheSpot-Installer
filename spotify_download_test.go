package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSpotifyDownloadRecovery(t *testing.T) {
	for _, tc := range []struct {
		name        string
		primary     []error
		fallback    error
		wantMethods []string
		wantWaits   int
		wantError   bool
	}{
		{"first success", []error{nil}, nil, []string{"curl"}, 0, false},
		{"transient 503", []error{downloadHTTPError(503), nil}, nil, []string{"curl", "curl"}, 1, false},
		{"DNS then fallback", []error{errors.New("DNS lookup failed"), errors.New("DNS lookup failed")}, nil, []string{"curl", "curl", "Windows .NET"}, 1, false},
		{"all fail", []error{downloadHTTPError(503), downloadHTTPError(503)}, errors.New("alternate client failed"), []string{"curl", "curl", "Windows .NET"}, 1, true},
		{"429 first", []error{downloadHTTPError(429)}, nil, []string{"curl"}, 0, true},
		{"429 second", []error{downloadHTTPError(503), downloadHTTPError(429)}, nil, []string{"curl", "curl"}, 1, true},
		{"local write failure", []error{&spotifyDownloadError{local: true, err: errors.New("disk full")}}, nil, []string{"curl"}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "SpotifySetup.exe")
			if err := os.WriteFile(target, []byte("old valid installer"), 0o600); err != nil {
				t.Fatal(err)
			}
			choice := spotifyInstallChoice{URL: "https://example.com/exact-version-x64.exe", BaseVersion: "1.2.3.4", FullVersion: "1.2.3.4.gabc", Size: 10}
			var methods, paths []string
			calls, waits, validations := 0, 0, 0
			method := func(name string, result func() error) spotifyDownloadMethod {
				return spotifyDownloadMethod{name: name, run: func(ctx context.Context, address, path string) error {
					if address != choice.URL {
						t.Errorf("substituted URL: %s", address)
					}
					if _, ok := ctx.Deadline(); !ok {
						t.Error("attempt has no timeout")
					}
					methods = append(methods, name)
					for _, previous := range paths {
						if previous == path {
							t.Error("attempt reused a partial file")
						}
						if _, err := os.Stat(previous); !os.IsNotExist(err) {
							t.Errorf("previous partial file not removed: %v", err)
						}
					}
					paths = append(paths, path)
					info, err := os.Stat(path)
					if err != nil || info.Size() != 0 {
						t.Fatalf("attempt did not start with an empty temp file: %v", err)
					}
					if err := os.WriteFile(path, []byte("downloaded"), 0o600); err != nil {
						t.Fatal(err)
					}
					return result()
				}}
			}
			fallback := method("Windows .NET", func() error { return tc.fallback })
			d := spotifyDownloader{
				primary: method("curl", func() error { result := tc.primary[calls]; calls++; return result }), fallback: &fallback,
				wait: func(ctx context.Context, delay time.Duration) error {
					waits++
					if delay != 5*time.Second {
						t.Errorf("retry delay: %s", delay)
					}
					return nil
				},
				validate: func(path string, got spotifyInstallChoice) error {
					validations++
					if got != choice || path == target {
						t.Fatal("validation must use selected metadata and the temporary file")
					}
					body, _ := os.ReadFile(target)
					if string(body) != "old valid installer" {
						t.Fatal("target changed before validation")
					}
					return nil
				},
			}
			err := d.download(context.Background(), choice, target)
			if (err != nil) != tc.wantError {
				t.Fatalf("got error %v, wantError %v", err, tc.wantError)
			}
			if !reflect.DeepEqual(methods, tc.wantMethods) || waits != tc.wantWaits {
				t.Fatalf("methods %v / waits %d; want %v / %d", methods, waits, tc.wantMethods, tc.wantWaits)
			}
			body, _ := os.ReadFile(target)
			if tc.wantError {
				if string(body) != "old valid installer" || validations != 0 {
					t.Fatal("failed request replaced target or was validated")
				}
			} else if string(body) != "downloaded" || validations != 1 {
				t.Fatal("successful download was not validated and promoted")
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Fatalf("temporary files leaked: %v", entries)
			}
		})
	}
}

func TestSpotifyDownloadRejectsInvalidFilesAndRecovers(t *testing.T) {
	for _, recover := range []bool{false, true} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "setup.exe")
			calls := 0
			d := spotifyDownloader{
				primary: spotifyDownloadMethod{name: "Windows .NET", run: func(ctx context.Context, address, path string) error {
					calls++
					return os.WriteFile(path, []byte("HTTP 200 with HTML or corrupt bytes"), 0o600)
				}},
				validate: func(path string, choice spotifyInstallChoice) error {
					if recover && calls == 2 {
						return nil
					}
					return errors.New("not a valid installer")
				},
				wait: func(context.Context, time.Duration) error { return nil },
			}
			err := d.download(context.Background(), spotifyInstallChoice{URL: "https://example.com/exact.exe"}, target)
			if (err == nil) != recover || calls != 2 {
				t.Fatalf("error %v, calls %d", err, calls)
			}
			entries, _ := os.ReadDir(dir)
			if (!recover && len(entries) != 0) || (recover && len(entries) != 1) {
				t.Fatalf("invalid/partial file survived: %v", entries)
			}
		})
	}
}

func TestSpotifyDownloadLocalPromotionFailureStops(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "setup.exe")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	calls := 0
	d := spotifyDownloader{
		primary:  spotifyDownloadMethod{name: "test", run: func(context.Context, string, string) error { calls++; return nil }},
		validate: func(string, spotifyInstallChoice) error { return nil },
	}
	err := d.download(context.Background(), spotifyInstallChoice{URL: "https://example.com/exact.exe"}, target)
	if err == nil || calls != 1 || !strings.Contains(err.Error(), "save validated installer") {
		t.Fatalf("got %v, calls %d", err, calls)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatal("destination changed or temporary file leaked")
	}
}

func TestSpotifyDownloadCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()
	calls := 0
	d := spotifyDownloader{
		primary: spotifyDownloadMethod{name: "test", run: func(context.Context, string, string) error {
			calls++
			cancel()
			return errors.New("request interrupted")
		}},
		validate: func(string, spotifyInstallChoice) error { t.Fatal("validated canceled request"); return nil },
	}
	err := d.download(ctx, spotifyInstallChoice{URL: "https://example.com/exact.exe"}, filepath.Join(dir, "setup.exe"))
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("got %v, calls %d", err, calls)
	}
	if err := waitForSpotifyDownloadRetry(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("retry wait ignored cancellation: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("canceled download leaked temporary file")
	}
}

func TestSpotifyDownloadURL(t *testing.T) {
	for _, address := range []string{"http://example.com/a.exe", "file:///tmp/a.exe", "https://user:password@example.com/a.exe", "https:///a.exe", "not a URL"} {
		if err := validateSpotifyDownloadURL(address); err == nil {
			t.Errorf("accepted %s", address)
		}
	}
	if err := validateSpotifyDownloadURL("https://example.com/exact.exe?download=1"); err != nil {
		t.Fatal(err)
	}
}

func TestSpotifyRateLimitDiagnostic(t *testing.T) {
	err := &spotifyDownloadError{status: http.StatusTooManyRequests, retryAfter: "120", err: errors.New("Too Many Requests")}
	for _, want := range []string{"HTTP 429", "try again later", "Retry-After: 120"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %s", want, err)
		}
	}
}

func downloadHTTPError(status int) error {
	return &spotifyDownloadError{status: status, err: errors.New(http.StatusText(status))}
}

func TestSpotifyDownloadProgressEnforcesStreamingLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.exe")
	if err := os.WriteFile(path, []byte("beyond the limit"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	transferCtx, abort := context.WithCancelCause(ctx)
	defer abort(nil)
	d := spotifyDownloader{} // Limit enforcement also runs with logging disabled.
	stop := d.trackProgress(transferCtx, abort, path, 0, 10)
	defer stop()
	<-transferCtx.Done()
	if cause := context.Cause(transferCtx); cause == nil || !strings.Contains(cause.Error(), "download size limit") {
		t.Fatalf("streaming limit did not abort transfer: %v", cause)
	}
}
