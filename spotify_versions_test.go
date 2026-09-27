package main

import (
	"strings"
	"testing"
)

// Keep this fixture in the shape of versions.json, including platforms we must ignore.
const versionsFixture = `{
  "1.2.100.10": {"fullversion":"1.2.100.10.gnewest","win":{"x64":{"url":"https://example.com/newest-x64.exe","date":"16.09.2026","size":148153000}}},
  "1.2.99.317": {"fullversion":"1.2.99.317.gnewer","win":{"x64":{"url":"https://example.com/newer-x64.exe"}}},
  "1.2.88.483": {"fullversion":"1.2.88.483.grecommended","win":{"x64":{"url":"https://example.com/recommended-x64.exe"}}},
  "1.2.88.100": {"fullversion":"1.2.88.100.golder","win":{"x64":{"url":"https://example.com/older-x64.exe"}}},
  "1.3.0.1": {"fullversion":"1.3.0.1.garm","win":{"arm64":{"url":"https://example.com/arm64.exe"}}},
  "1.3.0.2": {"fullversion":"1.3.0.2.gmac","mac":{"intel":{"url":"https://example.com/mac.tbz"}}},
  "1.3.0.3": {"fullversion":"1.3.0.3.gx86","win":{"x86":{"url":"https://example.com/x86.exe"}}}
}`

func TestLoadspotChoicesPreserveRecommendationRules(t *testing.T) {
	for _, recommendation := range []string{"1.2.88.483.grecommended", "1.2.88.483", "1.2.88.464"} {
		t.Run(recommendation, func(t *testing.T) {
			choices, selected, err := parseSpotifyInstallChoices(spotifyCompatibility{Version: recommendation}, []byte(versionsFixture))
			if err != nil {
				t.Fatal(err)
			}
			if len(choices) != 3 || selected != 2 {
				t.Fatalf("got %d choices, selection %d; want 3 choices, selection 2", len(choices), selected)
			}
			want := []string{"1.2.100.10", "1.2.99.317", "1.2.88.483"}
			for i, choice := range choices {
				if choice.BaseVersion != want[i] {
					t.Fatalf("choice %d: got %s, want %s", i, choice.BaseVersion, want[i])
				}
				if choice.Recommended != (i == selected) {
					t.Fatalf("incorrect recommendation: %+v", choice)
				}
			}
			if choices[selected].Display != "1.2.88.483.grecommended (recommended)" {
				t.Fatal(choices[selected].Display)
			}
			if choices[selected].URL != "https://example.com/recommended-x64.exe" {
				t.Fatal(choices[selected].URL)
			}
			if choices[0].Date != "16.09.2026" || choices[0].Size != 148153000 {
				t.Fatalf("missing metadata: %+v", choices[0])
			}
		})
	}
}

func TestLoadspotRejectsUnusableCatalogs(t *testing.T) {
	for name, body := range map[string]string{
		"invalid JSON":         `<html>Unavailable</html>`,
		"wrong shape":          `[]`,
		"null":                 `null`,
		"empty":                `{}`,
		"missing asset":        `{"1.3.0.1":{"fullversion":"1.3.0.1.ghash"}}`,
		"missing full version": `{"1.3.0.1":{"win":{"x64":{"url":"https://example.com/setup.exe"}}}}`,
		"mismatched version":   `{"1.3.0.1":{"fullversion":"1.2.0.1.ghash","win":{"x64":{"url":"https://example.com/setup.exe"}}}}`,
		"invalid version":      `{"1.3.bad.1":{"fullversion":"1.3.bad.1","win":{"x64":{"url":"https://example.com/setup.exe"}}}}`,
		"insecure URL":         `{"1.3.0.1":{"fullversion":"1.3.0.1.ghash","win":{"x64":{"url":"http://example.com/setup.exe"}}}}`,
		"relative URL":         `{"1.3.0.1":{"fullversion":"1.3.0.1.ghash","win":{"x64":{"url":"/setup.exe"}}}}`,
		"not installer":        `{"1.3.0.1":{"fullversion":"1.3.0.1.ghash","win":{"x64":{"url":"https://example.com/setup.tbz"}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			choices, selected, err := parseSpotifyInstallChoices(spotifyCompatibility{Version: "1.2.88.483"}, []byte(body))
			if err == nil || len(choices) != 0 || selected != -1 {
				t.Fatalf("got choices=%v selected=%d err=%v", choices, selected, err)
			}
		})
	}
}

func TestLoadspotNoSupportedVersion(t *testing.T) {
	for _, recommendation := range []string{"1.4.0.0", "", "invalid"} {
		if _, _, err := parseSpotifyInstallChoices(spotifyCompatibility{Version: recommendation}, []byte(versionsFixture)); err == nil {
			t.Fatalf("expected error for recommendation %q", recommendation)
		}
	}
}

func TestLoadspotSkipsInvalidEntriesAlongsideValidOnes(t *testing.T) {
	body := strings.Replace(versionsFixture, "https://example.com/newest-x64.exe", "https://example.com/not-an-installer", 1)
	choices, selected, err := parseSpotifyInstallChoices(spotifyCompatibility{Version: "1.2.88.483"}, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 2 || selected != 1 || choices[0].BaseVersion != "1.2.99.317" {
		t.Fatalf("unexpected choices: %+v, selection %d", choices, selected)
	}
}

func TestRecommendationComesFromConfig(t *testing.T) {
	got, err := compatibilityFromConfig([]byte("; BlockTheSpot\r\n; 1.2.88.464\r\n[settings]\r\n"))
	if err != nil || got.Version != "1.2.88.464" || got.Exact {
		t.Fatalf("version=%+v err=%v", got, err)
	}
	if _, err := compatibilityFromConfig([]byte("[settings]\n")); err == nil {
		t.Fatal("expected missing marker error")
	}
}

func TestExactCompatibilityRejectsNewerFallback(t *testing.T) {
	compatibility := spotifyCompatibility{Version: "1.2.88.483", Exact: true}
	choices, selected, err := parseSpotifyInstallChoices(compatibility, []byte(versionsFixture))
	if err != nil || len(choices) != 1 || selected != 0 || choices[0].BaseVersion != compatibility.Version {
		t.Fatalf("choices=%+v selected=%d err=%v", choices, selected, err)
	}
	compatibility.Version = "1.2.88.464"
	if _, _, err := parseSpotifyInstallChoices(compatibility, []byte(versionsFixture)); err == nil {
		t.Fatal("must not substitute a newer version")
	}
}

func TestCompatibilitySectionOverridesLegacyComment(t *testing.T) {
	got, err := compatibilityFromConfig([]byte(";1.2.3.4\n[Compatibility]\nSpotify=1.3.1.234\n"))
	if err != nil || !got.Exact || got.Version != "1.3.1.234" {
		t.Fatalf("got %+v, %v", got, err)
	}
	for version, want := range map[string]bool{"1.3.1.234": true, "1.3.1.234.ghash": true, "1.3.1.235": false, "1.2.3.4": false, "": false, "unknown": false} {
		if actual := got.supports(version); actual != want {
			t.Fatalf("supports(%q)=%t", version, actual)
		}
	}
	for _, section := range []string{"[Compatibility]", "[Compatibility]\nSpotify=", "[Compatibility]\nSpotify=1.3.1", "[Compatibility]\nSpotify=1.3.1.234.ghash", "[Compatibility]\nSpotify=1.3.1.234\nSpotify=1.3.1.235"} {
		if _, err := compatibilityFromConfig([]byte(";1.2.3.4\n" + section)); err == nil {
			t.Fatalf("invalid compatibility fell back to comment: %s", section)
		}
	}
}

func TestOriginalDLLSelectionAfterSpotifyUpdate(t *testing.T) {
	for _, tc := range []struct{ stock, backup, want string }{
		{"140.0.1.2", "139.0.1.2", "chrome_elf.dll"}, // Updated stock takes precedence over stale backup.
		{"140.0.1.2", "140.0.1.2", "chrome_elf.dll"},
		{"0.0.0.0", "140.0.1.2", "chrome_elf_required.dll"}, // Proxy stays out of the backup.
		{"0.0.0.0", "139.0.1.2", ""},
		{"", "", ""},
	} {
		name, err := originalDLLName(map[string]string{"libcef.dll": "140.0.2+gexample+chromium-140.0.1.2", "chrome_elf.dll": tc.stock, "chrome_elf_required.dll": tc.backup})
		if name != tc.want || (err == nil) != (tc.want != "") {
			t.Fatalf("%+v -> %q, %v", tc, name, err)
		}
	}
	if _, err := originalDLLName(map[string]string{"libcef.dll": "unknown"}); err == nil {
		t.Fatal("accepted unknown CEF version")
	}
}
