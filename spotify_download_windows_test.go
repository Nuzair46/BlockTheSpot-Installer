//go:build windows

package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpotifyCurlArgsKeepTLSVerification(t *testing.T) {
	args := spotifyCurlArgs("https://example.com/1.2.3.4.exe", `C:\a 'quoted' path\setup.exe`, "headers")
	if args[0] != "-q" {
		t.Fatal("curl must ignore curlrc before parsing other options")
	}
	if !strings.Contains(strings.Join(args, " "), "--globoff") {
		t.Fatal("curl must not expand braces or brackets into multiple requests")
	}
	for _, unsafe := range []string{"-k", "--insecure", "--ssl-no-revoke", "--ssl-revoke-best-effort"} {
		for _, arg := range args {
			if arg == unsafe {
				t.Fatalf("unsafe curl option: %s", arg)
			}
		}
	}
	for _, pair := range [][2]string{{"--proto", "=https"}, {"--proto-redir", "=https"}, {"--connect-timeout", "15"}, {"--max-time", "600"}, {"--retry", "0"}, {"--url", "https://example.com/1.2.3.4.exe"}} {
		found := false
		for n := 0; n+1 < len(args); n++ {
			if args[n] == pair[0] && args[n+1] == pair[1] {
				found = true
			}
		}
		if !found {
			t.Errorf("missing option %v", pair)
		}
	}
}

func TestCurlRetryAfterUsesFinalResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "headers")
	for _, tc := range []struct{ headers, want string }{
		{"HTTP/1.1 301 Moved\r\nRetry-After: 99\r\n\r\nHTTP/2 429\r\nretry-after: 120\r\n\r\n", "120"},
		{"HTTP/1.1 301 Moved\r\nRetry-After: 99\r\n\r\nHTTP/2 200\r\n\r\n", ""},
	} {
		if err := os.WriteFile(path, []byte(tc.headers), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := readCurlRetryAfter(path); got != tc.want {
			t.Fatalf("got %q, want %q", got, tc.want)
		}
	}
}

func TestWindowsHTTPDownloadScriptParses(t *testing.T) {
	executable := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	script := `$tokens=$null; $parseErrors=$null; [void][System.Management.Automation.Language.Parser]::ParseInput($env:BTS_SCRIPT, [ref]$tokens, [ref]$parseErrors); @($parseErrors | ForEach-Object { $_.Message }) | ConvertTo-Json -Compress; if ($parseErrors.Count -gt 0) { exit 1 }`
	out, stderr, err := runSpotifyCommand(ctx, executable, []string{"-NoProfile", "-NonInteractive", "-Command", script}, []string{"BTS_SCRIPT=" + spotifyWindowsDownloadScript})
	if err != nil {
		t.Fatalf("download script parse failed: %v (%s %s)", err, out, stderr)
	}
}

func TestWindowsHTTPRejectsPlaintextWithoutDownloading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := downloadSpotifyWithWindowsHTTP(ctx, "http://127.0.0.1:1/not-allowed.exe", path)
	if err == nil || !strings.Contains(err.Error(), "Only HTTPS") {
		t.Fatalf("plaintext download was not rejected: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("plaintext rejection created an installer")
	}
}

// This makes no external request and never changes the certificate trust store.
// Each real Windows transport must reject the local server's untrusted cert.
func TestWindowsDownloadClientsRejectUntrustedTLS(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("download reached HTTP handler despite an untrusted certificate")
		_, _ = io.WriteString(w, "must not be downloaded")
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	curl := filepath.Join(os.Getenv("SystemRoot"), "System32", "curl.exe")
	methods := []spotifyDownloadMethod{{name: "Windows .NET", run: downloadSpotifyWithWindowsHTTP}}
	if _, err := os.Stat(curl); err == nil {
		methods = append(methods, spotifyDownloadMethod{name: "curl", run: func(ctx context.Context, address, path string) error {
			return downloadSpotifyWithCurl(ctx, curl, address, path)
		}})
	}
	for _, method := range methods {
		t.Run(method.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "setup.exe")
			if err := method.run(ctx, server.URL+"/setup.exe", path); err == nil {
				t.Fatal("untrusted TLS was accepted")
			} else if ctx.Err() != nil {
				t.Fatalf("TLS test timed out instead of rejecting the certificate: %v", err)
			}
			if body, err := os.ReadFile(path); err == nil && len(body) != 0 {
				t.Fatalf("received untrusted content: %s", body)
			}
		})
	}
}

func TestWindowsDownloadParametersAreLiteral(t *testing.T) {
	executable := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	value := `C:\路径 with 'quotes' $(throw 'not executable')\setup.exe`
	script := `[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false); ConvertTo-Json -Compress -InputObject $env:BTS_TEST_PATH`
	out, stderr, err := runSpotifyCommand(ctx, executable, []string{"-NoProfile", "-NonInteractive", "-Command", script}, []string{"BTS_TEST_PATH=" + value})
	if err != nil {
		t.Fatalf("literal parameter test failed: %v (%s)", err, stderr)
	}
	var got string
	if err := json.Unmarshal(out, &got); err != nil || got != value {
		t.Fatalf("parameter was not preserved: %q (%v)", out, err)
	}
}

func TestWindowsDownloadCommandCancellation(t *testing.T) {
	executable := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, _, err := runSpotifyCommand(ctx, executable, []string{"-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 60"}, nil)
	if err == nil || ctx.Err() == nil {
		t.Fatalf("command did not stop on timeout: %v", err)
	}
}

func TestWindowsHTTPResponseHandling(t *testing.T) {
	payload := "installer transfer fixture"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/success":
			_, _ = io.WriteString(w, payload)
		case "/redirect":
			http.Redirect(w, r, "/success", http.StatusFound)
		case "/limited":
			w.Header().Set("Retry-After", "120")
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		case "/unavailable":
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		case "/partial":
			w.Header().Set("Content-Length", "100")
			_, _ = io.WriteString(w, "partial")
		}
	}))
	defer server.Close()
	executable, err := spotifyWindowsSystemPath("WindowsPowerShell", "v1.0", "powershell.exe")
	if err != nil {
		t.Fatal(err)
	}
	// Only this test copy permits loopback HTTP, so we can cover real .NET HTTP
	// status/stream handling without installing a certificate or weakening the
	// production helper. Separate tests enforce production HTTPS/TLS rejection.
	guard := "$uri.Scheme -ne 'https' -or $uri.UserInfo"
	if strings.Count(spotifyWindowsDownloadScript, guard) != 1 {
		t.Fatal("production HTTPS guard changed; update this explicit test seam")
	}
	script := strings.Replace(spotifyWindowsDownloadScript, guard, "($uri.Scheme -ne 'https' -and $uri.Host -ne '127.0.0.1') -or $uri.UserInfo", 1)
	for _, tc := range []struct {
		path       string
		wantStatus int
		wantOK     bool
	}{
		{"/success", 200, true}, {"/redirect", 200, true},
		{"/limited", 429, false}, {"/unavailable", 503, false}, {"/partial", 200, false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "setup 'quoted'.exe")
			out, stderr, runErr := runSpotifyCommand(ctx, executable,
				[]string{"-NoProfile", "-NonInteractive", "-Command", script},
				[]string{"BTS_DOWNLOAD_URL=" + server.URL + tc.path, "BTS_DOWNLOAD_PATH=" + path,
					"BTS_DOWNLOAD_LIMIT=1024", "BTS_DOWNLOAD_AGENT=BlockTheSpotTest"})
			var got struct {
				OK         bool   `json:"ok"`
				Status     int    `json:"status"`
				RetryAfter string `json:"retryAfter"`
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("cannot decode result: %v / %v (%s %s)", err, runErr, out, stderr)
			}
			if got.Status != tc.wantStatus || got.OK != tc.wantOK || (runErr == nil) != tc.wantOK {
				t.Fatalf("got %+v, error %v (%s)", got, runErr, stderr)
			}
			if got.Status == 429 && got.RetryAfter != "120" {
				t.Fatalf("lost rate-limit status or Retry-After: %+v", got)
			}
			if tc.wantOK {
				body, err := os.ReadFile(path)
				if err != nil || string(body) != payload {
					t.Fatalf("incorrect transfer: %q (%v)", body, err)
				}
			}
		})
	}
}
