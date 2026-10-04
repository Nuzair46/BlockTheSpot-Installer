package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A minimal PE with one section, independent of Windows or real Spotify files.
func installerPE(t *testing.T) []byte {
	t.Helper()
	dos := make([]byte, 128)
	copy(dos, "MZ")
	binary.LittleEndian.PutUint32(dos[60:], 128)
	buf := bytes.NewBuffer(dos)
	buf.WriteString("PE\x00\x00")
	optional := pe.OptionalHeader32{Magic: 0x10b, SectionAlignment: 4096, FileAlignment: 512, SizeOfImage: 8192, SizeOfHeaders: 512, NumberOfRvaAndSizes: 16}
	for _, header := range []any{
		pe.FileHeader{Machine: pe.IMAGE_FILE_MACHINE_I386, NumberOfSections: 1, SizeOfOptionalHeader: uint16(binary.Size(optional)), Characteristics: pe.IMAGE_FILE_EXECUTABLE_IMAGE | pe.IMAGE_FILE_32BIT_MACHINE},
		optional,
		pe.SectionHeader32{Name: [8]uint8{'.', 't', 'e', 'x', 't'}, VirtualSize: 512, VirtualAddress: 4096, SizeOfRawData: 512, PointerToRawData: 512},
	} {
		if err := binary.Write(buf, binary.LittleEndian, header); err != nil {
			t.Fatal(err)
		}
	}
	return append(buf.Bytes(), make([]byte, 1024-buf.Len())...)
}

func httpDownloadMethod(client *http.Client) installerDownloadMethod {
	return installerDownloadMethod{name: "HTTP", run: func(address, destination string, logf downloadLogger) error {
		return downloadInstallerHTTP(client, address, destination, logf)
	}}
}

func unexpectedDownload(t *testing.T) installerDownloadMethod {
	return installerDownloadMethod{name: "unexpected fallback", run: func(string, string, downloadLogger) error {
		t.Fatal("fallback must not run")
		return nil
	}}
}

func assertDownloadDirectory(t *testing.T, destination string, want []byte) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil {
		t.Fatal(err)
	}
	if want == nil {
		if len(entries) != 0 {
			t.Fatalf("failed download left files: %v", entries)
		}
		return
	}
	got, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(got, want) || len(entries) != 1 {
		t.Fatalf("download contents or cleanup incorrect: %d bytes, files %v, error %v", len(got), entries, err)
	}
}

func TestInstallerRetries503(t *testing.T) {
	body := installerPE(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/spotify_installer-1.3.1.234.g59d6bf59-x64.exe" {
			t.Errorf("download changed version: %s", r.URL)
		}
		if requests == 1 {
			w.Header().Set("Retry-After", "7")
			http.Error(w, "temporary upstream failure", http.StatusServiceUnavailable)
			return
		}
		w.Write(body)
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "setup.exe")
	var delays []time.Duration
	var logs []string
	err := downloadSpotifyInstaller(server.URL+"/spotify_installer-1.3.1.234.g59d6bf59-x64.exe", destination, int64(len(body)),
		[]installerDownloadMethod{httpDownloadMethod(server.Client()), unexpectedDownload(t)},
		func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
		func(delay time.Duration) { delays = append(delays, delay) })
	if err != nil || requests != 2 || !reflect.DeepEqual(delays, []time.Duration{7 * time.Second}) {
		t.Fatalf("requests=%d delays=%v err=%v", requests, delays, err)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "HTTP 503 Service Unavailable: server response: temporary upstream failure") {
		t.Fatalf("missing server diagnostic: %v", logs)
	}
	assertDownloadDirectory(t, destination, body)
}

func TestInstallerFallsBackWithSameURLAndFreshFile(t *testing.T) {
	for _, failure := range []error{
		&installerDownloadError{status: 403, err: errors.New("denied")},
		&installerDownloadError{status: 404, err: errors.New("not found")},
		&installerDownloadError{status: 503, err: errors.New("unavailable")},
		&installerDownloadError{err: &net.DNSError{Err: "database lookup failed", Name: "download.example"}},
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			body := installerPE(t)
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Write(body)
			}))
			defer server.Close()
			address := server.URL + "/exact-version.exe"
			primaryCalls := 0
			primary := installerDownloadMethod{name: "primary", run: func(url, destination string, _ downloadLogger) error {
				primaryCalls++
				info, err := os.Stat(destination)
				if err != nil || info.Size() != 0 || url != address {
					t.Fatalf("retry did not use a fresh file and the same URL: %s, %v, %v", url, info, err)
				}
				if err := os.WriteFile(destination, bytes.Repeat([]byte("bad"), 2048), 0o600); err != nil {
					t.Fatal(err)
				}
				return failure
			}}
			destination := filepath.Join(t.TempDir(), "setup.exe")
			err := downloadSpotifyInstaller(address, destination, int64(len(body)), []installerDownloadMethod{primary, httpDownloadMethod(server.Client())}, nil, func(time.Duration) {})
			if err != nil || primaryCalls != 2 || requests != 1 {
				t.Fatalf("primary=%d fallback=%d err=%v", primaryCalls, requests, err)
			}
			assertDownloadDirectory(t, destination, body)
		})
	}
}

func TestInstallerRateLimitsAndRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		status, calls int
		retryAfter    string
		delays        []time.Duration
	}{
		{429, 2, "", []time.Duration{5 * time.Second}},
		{429, 2, "12", []time.Duration{12 * time.Second}},
		{429, 1, "120", nil},
		{503, 1, "120", nil},
	} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.retryAfter), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", tc.retryAfter)
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			var delays []time.Duration
			destination := filepath.Join(t.TempDir(), "setup.exe")
			err := downloadSpotifyInstaller(server.URL, destination, 0, []installerDownloadMethod{httpDownloadMethod(server.Client()), unexpectedDownload(t)}, nil,
				func(delay time.Duration) { delays = append(delays, delay) })
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", tc.status)) || calls != tc.calls || !reflect.DeepEqual(delays, tc.delays) {
				t.Fatalf("calls=%d delays=%v err=%v", calls, delays, err)
			}
			assertDownloadDirectory(t, destination, nil)
		})
	}
	now := time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC)
	if delay := installerRetryDelay(now.Add(20*time.Second).Format(http.TimeFormat), now); delay != 20*time.Second {
		t.Fatalf("HTTP-date Retry-After: %s", delay)
	}
}

func TestInstallerRejectsBadPayloadsAndPreservesDestination(t *testing.T) {
	valid := installerPE(t)
	for _, tc := range []struct {
		name         string
		body         []byte
		declaredSize int
		expectedSize int64
		status       int
	}{
		{"empty", nil, 0, 0, 200},
		{"HTML with status 200", []byte("<html>Access denied</html>"), 0, 0, 200},
		{"truncated transfer", valid[:100], len(valid), 0, 200},
		{"truncated executable", valid[:512], 0, 0, 200},
		{"catalog size mismatch", valid, 0, int64(len(valid) + 1), 200},
		{"partial response", valid, 0, 0, 206},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.declaredSize > 0 {
					w.Header().Set("Content-Length", fmt.Sprint(tc.declaredSize))
				}
				w.WriteHeader(tc.status)
				w.Write(tc.body)
			}))
			defer server.Close()
			destination := filepath.Join(t.TempDir(), "setup.exe")
			original := []byte("existing file")
			if err := os.WriteFile(destination, original, 0o600); err != nil {
				t.Fatal(err)
			}
			err := downloadSpotifyInstaller(server.URL, destination, tc.expectedSize, []installerDownloadMethod{httpDownloadMethod(server.Client())}, nil, func(time.Duration) {})
			if err == nil {
				t.Fatal("accepted an invalid download")
			}
			assertDownloadDirectory(t, destination, original)
		})
	}
}

func TestInstallerLocalFileErrorStopsRecovery(t *testing.T) {
	calls := 0
	method := installerDownloadMethod{name: "disk failure", run: func(string, string, downloadLogger) error {
		calls++
		return &os.PathError{Op: "write", Path: "setup.exe", Err: os.ErrPermission}
	}}
	destination := filepath.Join(t.TempDir(), "setup.exe")
	err := downloadSpotifyInstaller("https://example.com/exact-version.exe", destination, 0,
		[]installerDownloadMethod{method, unexpectedDownload(t)}, nil, func(time.Duration) { t.Fatal("must not retry a local file error") })
	if !errors.Is(err, os.ErrPermission) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	assertDownloadDirectory(t, destination, nil)
}

func TestCurlInstallerDownloads(t *testing.T) {
	curlPath, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl unavailable")
	}
	body := installerPE(t)
	for _, tc := range []struct {
		name, retryAfter string
		status           int
		truncate         bool
	}{
		{"redirect and success", "", 200, false},
		{"server error", "", 503, false},
		{"rate limit", "19", 429, false},
		{"truncated transfer", "", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/redirect" {
					w.Header().Set("Retry-After", "999") // Only the final response counts.
					http.Redirect(w, r, "/setup", http.StatusFound)
					return
				}
				w.Header().Set("Retry-After", tc.retryAfter)
				w.Header().Set("Content-Length", fmt.Sprint(len(body)))
				w.WriteHeader(tc.status)
				if tc.truncate {
					w.Write(body[:100])
				} else {
					w.Write(body)
				}
			}))
			defer server.Close()
			method := installerDownloadMethod{name: "curl", run: func(address, destination string, logf downloadLogger) error {
				return downloadInstallerCurl(exec.CommandContext, curlPath, address, destination, logf)
			}}
			destination := filepath.Join(t.TempDir(), "setup.exe")
			err := attemptSpotifyDownload(server.URL+"/redirect", destination, int64(len(body)), method, nil)
			if tc.status == 200 && !tc.truncate {
				if err != nil {
					t.Fatal(err)
				}
				assertDownloadDirectory(t, destination, body)
				return
			}
			var failure *installerDownloadError
			if !errors.As(err, &failure) || failure.retryAfter != tc.retryAfter || (!tc.truncate && failure.status != tc.status) {
				t.Fatalf("wrong curl failure: %+v", err)
			}
			assertDownloadDirectory(t, destination, nil)
		})
	}
}
