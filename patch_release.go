package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const patchLatestReleaseAPI = "https://api.github.com/repos/Nuzair46/BlockTheSpot/releases/latest"

var installerVersion = "dev"

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

type patchRelease struct {
	Tag           string
	Compatibility spotifyCompatibility
	Config        []byte
	Assets        map[string]string
}

type downloadFunc func(string) ([]byte, error)

// Resolve latest once. All subsequent downloads use this release's asset URLs.
func fetchPatchRelease(apiURL string, download downloadFunc) (*patchRelease, error) {
	body, err := download(apiURL)
	if err != nil {
		return nil, fmt.Errorf("load BlockTheSpot release: %w", err)
	}
	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("invalid BlockTheSpot release: %w", err)
	}
	if release.TagName == "" {
		return nil, errors.New("BlockTheSpot release has no tag")
	}
	assets := make(map[string]string)
	for _, asset := range release.Assets {
		if _, exists := assets[asset.Name]; exists {
			return nil, fmt.Errorf("duplicate release asset %s", asset.Name)
		}
		parsed, err := url.Parse(asset.URL)
		prefix := "/Nuzair46/BlockTheSpot/releases/download/" + release.TagName + "/"
		if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.Path != prefix+asset.Name {
			return nil, fmt.Errorf("invalid release URL for %s", asset.Name)
		}
		assets[asset.Name] = asset.URL
	}
	for _, name := range []string{"config.ini", "chrome_elf.dll", "blockthespot.dll"} {
		if assets[name] == "" {
			return nil, fmt.Errorf("release %s is missing %s", release.TagName, name)
		}
	}
	config, err := download(assets["config.ini"])
	if err != nil {
		return nil, fmt.Errorf("download config.ini: %w", err)
	}
	compatibility, err := compatibilityFromConfig(config)
	if err != nil {
		return nil, err
	}
	if compatibility.Exact {
		for _, name := range []string{"settings.example.ini", "SHA256SUMS.txt"} {
			if assets[name] == "" {
				return nil, fmt.Errorf("release %s is missing %s", release.TagName, name)
			}
		}
	}
	return &patchRelease{Tag: release.TagName, Compatibility: compatibility, Config: config, Assets: assets}, nil
}

// Download and verify everything before changing the installed files.
func (r *patchRelease) downloadFiles(download downloadFunc) (map[string][]byte, error) {
	files := map[string][]byte{"config.ini": r.Config}
	for _, name := range []string{"chrome_elf.dll", "blockthespot.dll", "settings.example.ini"} {
		// Older releases do not have settings.
		if r.Assets[name] == "" {
			continue
		}
		body, err := download(r.Assets[name])
		if err != nil {
			return nil, fmt.Errorf("download %s: %w", name, err)
		}
		files[name] = body
	}
	if checksumURL := r.Assets["SHA256SUMS.txt"]; checksumURL != "" {
		body, err := download(checksumURL)
		if err != nil {
			return nil, fmt.Errorf("download checksums: %w", err)
		}
		if err := verifyChecksums(files, body); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func verifyChecksums(files map[string][]byte, manifest []byte) error {
	hashes := make(map[string]string)
	for _, line := range strings.Split(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return errors.New("invalid SHA256SUMS.txt line")
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size {
			return errors.New("invalid SHA256 digest")
		}
		name := strings.TrimPrefix(fields[1], "*")
		if _, exists := hashes[name]; exists {
			return fmt.Errorf("duplicate checksum for %s", name)
		}
		hashes[name] = strings.ToLower(fields[0])
	}
	for name, body := range files {
		actual := fmt.Sprintf("%x", sha256.Sum256(body))
		if hashes[name] != actual {
			return fmt.Errorf("SHA256 verification failed for %s; installation was not changed", name)
		}
	}
	return nil
}

func downloadBytes(address string) ([]byte, error) {
	req, err := newDownloadRequest(address)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	const limit = 32 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > limit {
		return nil, errors.New("download exceeds 32 MiB limit")
	}
	return body, nil
}

func newDownloadRequest(address string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,application/json;q=0.8,*/*;q=0.7")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	return req, nil
}
