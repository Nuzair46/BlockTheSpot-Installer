package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Loadspot indexes releases by numeric version and separates platform/architecture assets.
type spotifyRelease struct {
	FullVersion string `json:"fullversion"`
	Windows     struct {
		X64 spotifyDownload `json:"x64"`
	} `json:"win"`
}

type spotifyDownload struct {
	URL  string `json:"url"`
	Date string `json:"date"`
	Size int64  `json:"size"`
}

type spotifyInstallChoice struct {
	Display     string
	BaseVersion string
	FullVersion string
	URL         string
	Date        string
	Size        int64
	Recommended bool
}

type spotifyCompatibility struct {
	Version string
	Exact   bool
}

func (c spotifyCompatibility) supports(version string) bool {
	if !validSpotifyBaseVersion(baseSpotifyVersion(version)) {
		return false
	}
	if c.Exact {
		return baseSpotifyVersion(version) == c.Version
	}
	return compareVersion(version, c.Version) >= 0
}

func parseSpotifyInstallChoices(compatibility spotifyCompatibility, body []byte) ([]spotifyInstallChoice, int, error) {
	var releases map[string]spotifyRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, -1, fmt.Errorf("invalid Spotify versions JSON: %w", err)
	}
	return buildSpotifyInstallChoices(compatibility, releases)
}

func buildSpotifyInstallChoices(compatibility spotifyCompatibility, releases map[string]spotifyRelease) ([]spotifyInstallChoice, int, error) {
	recommendedBase := baseSpotifyVersion(compatibility.Version)
	if !validSpotifyBaseVersion(recommendedBase) {
		return nil, -1, errors.New("invalid recommended Spotify version")
	}
	choices := make([]spotifyInstallChoice, 0, len(releases))
	for version, release := range releases {
		version = strings.TrimSpace(version)
		fullVersion := strings.TrimSpace(release.FullVersion)
		if !validSpotifyBaseVersion(version) || baseSpotifyVersion(fullVersion) != version {
			continue
		}
		if !compatibility.supports(version) {
			continue
		}
		asset := release.Windows.X64
		downloadURL := strings.TrimSpace(asset.URL)
		parsed, err := url.Parse(downloadURL)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || !strings.HasSuffix(strings.ToLower(parsed.Path), ".exe") {
			continue
		}
		choices = append(choices, spotifyInstallChoice{
			Display:     fullVersion,
			BaseVersion: version,
			FullVersion: fullVersion,
			URL:         downloadURL,
			Date:        strings.TrimSpace(asset.Date),
			Size:        asset.Size,
		})
	}
	if len(choices) == 0 {
		return nil, -1, fmt.Errorf("no compatible Windows x64 Spotify installer found for %s; retry later or install that version yourself", recommendedBase)
	}
	sort.Slice(choices, func(i, j int) bool {
		if cmp := compareVersion(choices[i].BaseVersion, choices[j].BaseVersion); cmp != 0 {
			return cmp > 0
		}
		return choices[i].FullVersion < choices[j].FullVersion
	})
	// The oldest eligible release is the exact recommendation, or its nearest newer release.
	recommendedIndex := len(choices) - 1
	choices[recommendedIndex].Recommended = true
	choices[recommendedIndex].Display += " (recommended)"
	return choices, recommendedIndex, nil
}

func validSpotifyBaseVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 4 || parts[0] != "1" {
		return false
	}
	for _, part := range parts {
		if !isAllDigits(part) {
			return false
		}
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

func compatibilityFromConfig(body []byte) (spotifyCompatibility, error) {
	values, err := parseINI(body)
	if err != nil {
		return spotifyCompatibility{}, fmt.Errorf("invalid config.ini: %w", err)
	}
	if section, exists := values["compatibility"]; exists {
		version := section["spotify"]
		if !validSpotifyBaseVersion(version) {
			return spotifyCompatibility{}, errors.New("invalid or missing [Compatibility] Spotify version")
		}
		return spotifyCompatibility{Version: version, Exact: true}, nil
	}
	// Preserve the minimum-version contract for old releases without Compatibility.
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, ";") {
			continue
		}

		candidate := strings.TrimSpace(strings.TrimPrefix(line, ";"))
		if !validSpotifyBaseVersion(baseSpotifyVersion(candidate)) {
			continue
		}
		return spotifyCompatibility{Version: baseSpotifyVersion(candidate)}, nil
	}

	return spotifyCompatibility{}, errors.New("no Spotify version marker found")
}

func compareVersion(a, b string) int {
	av := normalizeVersion(a)
	bv := normalizeVersion(b)
	for idx := 0; idx < len(av) && idx < len(bv); idx++ {
		if av[idx] < bv[idx] {
			return -1
		}
		if av[idx] > bv[idx] {
			return 1
		}
	}
	return 0
}

func normalizeVersion(value string) []int {
	parts := strings.Split(value, ".")
	parsed := make([]int, 0, 4)
	for _, part := range parts {
		digits := leadingDigits(part)
		if digits == "" {
			break
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			break
		}
		parsed = append(parsed, n)
		if len(parsed) == 4 {
			break
		}
	}
	for len(parsed) < 4 {
		parsed = append(parsed, 0)
	}
	return parsed
}

func leadingDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

func normalizeVersionString(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// Most systems return a dotted version directly.
	for _, token := range strings.Fields(raw) {
		if strings.Count(token, ".") >= 2 && leadingDigits(token) != "" {
			return token
		}
	}

	// Some PowerShell setups format ProductVersionRaw as a table (Major Minor Build Revision).
	numbers := make([]string, 0, 4)
	for _, token := range strings.Fields(raw) {
		if !isAllDigits(token) {
			continue
		}
		numbers = append(numbers, token)
		if len(numbers) == 4 {
			break
		}
	}
	if len(numbers) >= 3 {
		return strings.Join(numbers, ".")
	}

	return raw
}

func baseSpotifyVersion(value string) string {
	parts := strings.Split(value, ".")
	base := make([]string, 0, 4)
	for _, part := range parts {
		digits := leadingDigits(part)
		if digits == "" {
			break
		}
		base = append(base, digits)
		if len(base) == 4 {
			break
		}
	}
	return strings.Join(base, ".")
}

func isAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func originalDLLName(versions map[string]string) (string, error) {
	chromium := regexp.MustCompile(`chromium-(\d+\.\d+\.\d+\.\d+)`).FindStringSubmatch(versions["libcef.dll"])
	if len(chromium) != 2 {
		return "", errors.New("cannot determine Spotify's Chromium version; enable Update or reinstall Spotify before patching")
	}
	// Spotify updates can restore stock chrome_elf.dll while leaving an old backup.
	for _, name := range []string{"chrome_elf.dll", "chrome_elf_required.dll"} {
		if versions[name] == chromium[1] {
			return name, nil
		}
	}
	return "", fmt.Errorf("original chrome_elf.dll for Chromium %s is missing or outdated; enable Update or reinstall Spotify before patching", chromium[1])
}
