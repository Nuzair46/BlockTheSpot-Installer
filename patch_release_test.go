package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func releaseFixture(t *testing.T, modern bool) map[string][]byte {
	t.Helper()
	config := []byte(";1.2.3.4\n[Log]\nLevel=0\n")
	if modern {
		config = []byte(";1.2.3.4\n[Compatibility]\nSpotify=1.3.1.234\n")
	}
	files := map[string][]byte{"config.ini": config, "chrome_elf.dll": []byte("proxy"), "blockthespot.dll": []byte("hook")}
	if modern {
		files["settings.example.ini"] = []byte(settingsTemplate)
		var checksums strings.Builder
		for name, body := range files {
			fmt.Fprintf(&checksums, "%x  %s\r\n", sha256.Sum256(body), name)
		}
		files["SHA256SUMS.txt"] = []byte(checksums.String())
	}
	release := githubRelease{TagName: "v1"}
	responses := map[string][]byte{}
	for name, body := range files {
		address := "https://github.com/Nuzair46/BlockTheSpot/releases/download/v1/" + name
		release.Assets = append(release.Assets, struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}{name, address})
		responses[address] = body
	}
	body, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	responses[patchLatestReleaseAPI] = body
	return responses
}

func fixtureDownloader(responses map[string][]byte) downloadFunc {
	return func(address string) ([]byte, error) {
		body, exists := responses[address]
		if !exists {
			return nil, fmt.Errorf("unexpected URL: %s", address)
		}
		return body, nil
	}
}

func TestReleaseDownloadsStayPinnedWhenLatestChanges(t *testing.T) {
	for _, modern := range []bool{false, true} {
		responses := releaseFixture(t, modern)
		release, err := fetchPatchRelease(patchLatestReleaseAPI, fixtureDownloader(responses))
		if err != nil {
			t.Fatal(err)
		}
		responses[patchLatestReleaseAPI] = []byte(`{"tag_name":"v2","assets":[]}`)
		files, err := release.downloadFiles(fixtureDownloader(responses))
		if err != nil {
			t.Fatal(err)
		}
		if string(files["chrome_elf.dll"]) != "proxy" || string(files["blockthespot.dll"]) != "hook" || release.Tag != "v1" || release.Compatibility.Exact != modern {
			t.Fatalf("incorrect release: %+v", release)
		}
	}
}

func TestReleaseRejectsMissingOrMixedAssets(t *testing.T) {
	for _, mutation := range []string{"missing settings", "missing checksum", "different tag", "duplicate", "bad config"} {
		t.Run(mutation, func(t *testing.T) {
			responses := releaseFixture(t, true)
			var metadata githubRelease
			if err := json.Unmarshal(responses[patchLatestReleaseAPI], &metadata); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "missing settings", "missing checksum":
				name := "settings.example.ini"
				if mutation == "missing checksum" {
					name = "SHA256SUMS.txt"
				}
				for n, asset := range metadata.Assets {
					if asset.Name == name {
						metadata.Assets = append(metadata.Assets[:n], metadata.Assets[n+1:]...)
						break
					}
				}
			case "different tag":
				metadata.Assets[0].URL = strings.Replace(metadata.Assets[0].URL, "/v1/", "/v2/", 1)
			case "duplicate":
				metadata.Assets = append(metadata.Assets, metadata.Assets[0])
			case "bad config":
				responses["https://github.com/Nuzair46/BlockTheSpot/releases/download/v1/config.ini"] = []byte(";1.2.3.4\n[Compatibility]\nSpotify=bad")
			}
			body, err := json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			responses[patchLatestReleaseAPI] = body
			if _, err := fetchPatchRelease(patchLatestReleaseAPI, fixtureDownloader(responses)); err == nil {
				t.Fatal("accepted incomplete or inconsistent release")
			}
		})
	}
}

func TestReleaseDetectsCorruptionAndDownloadFailure(t *testing.T) {
	for _, name := range []string{"config.ini", "chrome_elf.dll", "blockthespot.dll", "settings.example.ini"} {
		responses := releaseFixture(t, true)
		address := "https://github.com/Nuzair46/BlockTheSpot/releases/download/v1/" + name
		responses[address] = append(responses[address], []byte("\n; corruption")...)
		release, err := fetchPatchRelease(patchLatestReleaseAPI, fixtureDownloader(responses))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := release.downloadFiles(fixtureDownloader(responses)); err == nil {
			t.Fatalf("accepted corrupt %s", name)
		}
	}
	responses := releaseFixture(t, true)
	release, err := fetchPatchRelease(patchLatestReleaseAPI, fixtureDownloader(responses))
	if err != nil {
		t.Fatal(err)
	}
	fail := errors.New("download failed")
	if _, err := release.downloadFiles(func(string) ([]byte, error) { return nil, fail }); !errors.Is(err, fail) {
		t.Fatal(err)
	}
}

func TestChecksumManifestRequiresEveryFileOnce(t *testing.T) {
	files := map[string][]byte{"config.ini": []byte("pack")}
	valid := fmt.Sprintf("%x  config.ini\n", sha256.Sum256(files["config.ini"]))
	if err := verifyChecksums(files, []byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, manifest := range []string{"", "bad hash\n", valid + valid, strings.Replace(valid, "config.ini", "other.ini", 1)} {
		if err := verifyChecksums(files, []byte(manifest)); err == nil {
			t.Fatalf("accepted %q", manifest)
		}
	}
}
