package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLiveReleaseAndCatalog(t *testing.T) {
	if os.Getenv("BTS_LIVE_DOWNLOAD_TEST") != "1" {
		t.Skip("set BTS_LIVE_DOWNLOAD_TEST=1 for real release and catalog downloads")
	}
	release, err := fetchPatchRelease(patchLatestReleaseAPI, downloadBytes)
	if err != nil {
		t.Fatal(err)
	}
	files, err := release.downloadFiles(downloadBytes)
	if err != nil {
		t.Fatal(err)
	}
	body, err := downloadBytes("https://loadspot.pages.dev/versions.json")
	if err != nil {
		t.Fatal(err)
	}
	choices, selected, err := parseSpotifyInstallChoices(release.Compatibility, body)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("release %s: %d verified/downloaded files; selected %s", release.Tag, len(files), choices[selected].FullVersion)
	if version := os.Getenv("BTS_EXACT_SPOTIFY_VERSION"); version != "" {
		choices, _, err := parseSpotifyInstallChoices(spotifyCompatibility{Version: version, Exact: true}, body)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("exact requirement: %+v", choices)
	}
}

func TestLiveSpotifyInstallerDownload(t *testing.T) {
	if os.Getenv("BTS_LIVE_INSTALLER_DOWNLOAD_TEST") != "1" {
		t.Skip("set BTS_LIVE_INSTALLER_DOWNLOAD_TEST=1 to download and validate the selected installer without running it")
	}
	release, err := fetchPatchRelease(patchLatestReleaseAPI, downloadBytes)
	if err != nil {
		t.Fatal(err)
	}
	body, err := downloadBytes("https://loadspot.pages.dev/versions.json")
	if err != nil {
		t.Fatal(err)
	}
	choices, selected, err := parseSpotifyInstallChoices(release.Compatibility, body)
	if err != nil {
		t.Fatal(err)
	}
	choice := choices[selected]
	methods := []installerDownloadMethod{httpDownloadMethod(&http.Client{Timeout: 10 * time.Minute})}
	if curlPath, err := exec.LookPath("curl"); err == nil {
		methods = append([]installerDownloadMethod{{name: "curl", run: func(url, destination string, logf downloadLogger) error {
			return downloadInstallerCurl(exec.CommandContext, curlPath, url, destination, logf)
		}}}, methods...)
	}
	if err := downloadSpotifyInstaller(choice.URL, filepath.Join(t.TempDir(), "setup.exe"), choice.Size, methods, t.Logf, time.Sleep); err != nil {
		t.Fatal(err)
	}
	t.Logf("Downloaded %s (%d bytes); executable was not run", choice.FullVersion, choice.Size)
}
