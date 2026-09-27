package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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

func parseSpotifyInstallChoices(recommendedVersion string, body []byte) ([]spotifyInstallChoice, int, error) {
	var releases map[string]spotifyRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, -1, fmt.Errorf("invalid Spotify versions JSON: %w", err)
	}
	return buildSpotifyInstallChoices(recommendedVersion, releases)
}

func buildSpotifyInstallChoices(recommendedVersion string, releases map[string]spotifyRelease) ([]spotifyInstallChoice, int, error) {
	recommendedBase := baseSpotifyVersion(recommendedVersion)
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
		if compareVersion(version, recommendedBase) < 0 {
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
		return nil, -1, errors.New("no supported Windows x64 Spotify installers found in versions list")
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

func extractMinimumVersionFromConfig(body []byte) (string, error) {
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, ";") {
			continue
		}

		candidate := strings.TrimSpace(strings.TrimPrefix(line, ";"))
		if candidate == "" || !looksLikeSpotifyVersion(candidate) {
			continue
		}
		return candidate, nil
	}

	return "", errors.New("no Spotify version marker found")
}

func looksLikeSpotifyVersion(value string) bool {
	if !strings.HasPrefix(value, "1.") {
		return false
	}

	parts := strings.Split(value, ".")
	if len(parts) < 3 {
		return false
	}

	numericParts := 0
	for _, part := range parts {
		if leadingDigits(part) == "" {
			break
		}
		numericParts++
		if numericParts == 4 {
			break
		}
	}

	return numericParts >= 3
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
