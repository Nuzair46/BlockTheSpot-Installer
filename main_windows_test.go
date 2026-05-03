//go:build windows

package main

import "testing"

func TestBuildSpotifyInstallChoicesFromUptodownResponse(t *testing.T) {
	response := uptodownVersionsResponse{
		Success: 1,
		Data: []uptodownVersionEntry{
			{
				FileID:     1165561942,
				Version:    "1.2.88.483.g8aa8628e",
				LastUpdate: "Apr 25, 2026",
				KindFile:   "exe",
				VersionURL: uptodownVersionURL{
					URL:       "https://spotify.en.uptodown.com/windows",
					ExtraURL:  "download",
					VersionID: 1163456380,
				},
			},
			{
				FileID:     1162249176,
				Version:    "1.2.87.414.g4e7a1155",
				LastUpdate: "Apr 14, 2026",
				KindFile:   "exe",
				VersionURL: uptodownVersionURL{
					URL:       "https://spotify.en.uptodown.com/windows",
					ExtraURL:  "download",
					VersionID: 1162249176,
				},
			},
			{
				FileID:     1150000000,
				Version:    "1.2.86.100.gabc12345",
				LastUpdate: "Apr 1, 2026",
				KindFile:   "exe",
				VersionURL: uptodownVersionURL{
					URL:       "https://spotify.en.uptodown.com/windows",
					ExtraURL:  "download",
					VersionID: 1150000000,
				},
			},
			{
				FileID:   1169999999,
				Version:  "1.2.89.1.gignored",
				KindFile: "zip",
				VersionURL: uptodownVersionURL{
					URL:       "https://spotify.en.uptodown.com/windows",
					ExtraURL:  "download",
					VersionID: 1169999999,
				},
			},
		},
	}

	choices, recommendedIndex, err := buildSpotifyInstallChoices("1.2.87.414.g4e7a1155", response)
	if err != nil {
		t.Fatalf("buildSpotifyInstallChoices returned error: %v", err)
	}

	if len(choices) != 2 {
		t.Fatalf("expected 2 choices, got %d", len(choices))
	}
	if recommendedIndex != 1 {
		t.Fatalf("expected recommended index 1, got %d", recommendedIndex)
	}

	if choices[0].FullVersion != "1.2.88.483.g8aa8628e" {
		t.Fatalf("expected newest version first, got %q", choices[0].FullVersion)
	}
	if choices[0].DownloadPageURL != "https://spotify.en.uptodown.com/windows/download/1163456380" {
		t.Fatalf("unexpected download page URL: %q", choices[0].DownloadPageURL)
	}
	if choices[1].Display != "1.2.87.414.g4e7a1155 (recommended)" {
		t.Fatalf("unexpected recommended display: %q", choices[1].Display)
	}
}

func TestBuildSpotifyInstallChoicesUsesNumericConfigVersion(t *testing.T) {
	response := uptodownVersionsResponse{
		Success: 1,
		Data: []uptodownVersionEntry{
			{
				FileID:   1167000000,
				Version:  "1.2.90.10.glatest",
				KindFile: "exe",
				VersionURL: uptodownVersionURL{
					URL:       "https://spotify.en.uptodown.com/windows",
					ExtraURL:  "download",
					VersionID: 1167000000,
				},
			},
			{
				FileID:   1166000000,
				Version:  "1.2.89.25.gnewer",
				KindFile: "exe",
				VersionURL: uptodownVersionURL{
					URL:       "https://spotify.en.uptodown.com/windows",
					ExtraURL:  "download",
					VersionID: 1166000000,
				},
			},
			{
				FileID:   1165561942,
				Version:  "1.2.88.483.g8aa8628e",
				KindFile: "exe",
				VersionURL: uptodownVersionURL{
					URL:       "https://spotify.en.uptodown.com/windows",
					ExtraURL:  "download",
					VersionID: 1163456380,
				},
			},
			{
				FileID:   1165000000,
				Version:  "1.2.88.100.gold",
				KindFile: "exe",
				VersionURL: uptodownVersionURL{
					URL:       "https://spotify.en.uptodown.com/windows",
					ExtraURL:  "download",
					VersionID: 1165000000,
				},
			},
		},
	}

	choices, recommendedIndex, err := buildSpotifyInstallChoices("1.2.88.464", response)
	if err != nil {
		t.Fatalf("buildSpotifyInstallChoices returned error: %v", err)
	}

	if len(choices) != 3 {
		t.Fatalf("expected 3 choices, got %d", len(choices))
	}
	if recommendedIndex != 2 {
		t.Fatalf("expected closest supported version to be selected, got index %d", recommendedIndex)
	}
	if choices[2].FullVersion != "1.2.88.483.g8aa8628e" {
		t.Fatalf("unexpected closest supported version: %q", choices[2].FullVersion)
	}
	if choices[2].Display != "1.2.88.483.g8aa8628e (recommended)" {
		t.Fatalf("unexpected recommended display: %q", choices[2].Display)
	}
}

func TestParseUptodownVersionsPage(t *testing.T) {
	body := []byte(`<section class="versions">
		<div class="content">
			<div data-url="https://spotify.uptodown.com/windows" data-version-id="1163456380" data-extra-url="descargar" >
				<span class="type others" title="exe">exe</span>
				<span class="version">1.2.88.483.g8aa8628e</span>
				<span class="date">25 abr. 2026</span>
			</div>
			<div data-url="https://spotify.uptodown.com/windows" data-version-id="1162249176" data-extra-url="descargar" >
				<span class="type others" title="exe">exe</span>
				<span class="version">1.2.87.414.g4e7a1155</span>
				<span class="date">14 abr. 2026</span>
			</div>
		</div>
	</section>`)

	response := parseUptodownVersionsPage(body)
	if response.Success != 1 {
		t.Fatalf("expected success response, got %d", response.Success)
	}
	if len(response.Data) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(response.Data))
	}

	first := response.Data[0]
	if first.Version != "1.2.88.483.g8aa8628e" {
		t.Fatalf("unexpected first version: %q", first.Version)
	}
	if first.KindFile != "exe" {
		t.Fatalf("unexpected first kind file: %q", first.KindFile)
	}
	if first.VersionURL.URL != "https://spotify.uptodown.com/windows" {
		t.Fatalf("unexpected first URL: %q", first.VersionURL.URL)
	}
	if first.VersionURL.ExtraURL != "descargar" {
		t.Fatalf("unexpected first extra URL: %q", first.VersionURL.ExtraURL)
	}
	if first.VersionURL.VersionID != 1163456380 {
		t.Fatalf("unexpected first version ID: %d", first.VersionURL.VersionID)
	}
}

func TestExtractUptodownDownloadToken(t *testing.T) {
	body := []byte(`<html><body>
		<button data-download-version="1142447016" id = "detail-download-button" data-url="abc/def/ghi==/">
			<strong>Download</strong>
		</button>
	</body></html>`)

	token, err := extractUptodownDownloadToken(body)
	if err != nil {
		t.Fatalf("extractUptodownDownloadToken returned error: %v", err)
	}
	if token != "abc/def/ghi==/" {
		t.Fatalf("unexpected token: %q", token)
	}
}

func TestBuildUptodownDownloadURL(t *testing.T) {
	filename := uptodownInstallerFilename("1.2.87.414.g4e7a1155")
	if filename != "1-2-87-414-g4e7a1155.exe" {
		t.Fatalf("unexpected filename: %q", filename)
	}

	got := buildUptodownDownloadURL("/abc/def", filename)
	want := "https://dw.uptodown.net/dwn/abc/def/1-2-87-414-g4e7a1155.exe"
	if got != want {
		t.Fatalf("unexpected download URL: %q", got)
	}
}

func TestEffectiveSpotifyRecommendedVersionUsesTemporaryOverride(t *testing.T) {
	got := effectiveSpotifyRecommendedVersion("1.2.88.464")
	if got != "1.2.86.502" {
		t.Fatalf("unexpected effective recommended version: %q", got)
	}
}
