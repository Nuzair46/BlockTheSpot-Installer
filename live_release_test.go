package main

import (
	"os"
	"testing"
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
