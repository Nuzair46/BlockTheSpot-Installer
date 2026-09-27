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
			choices, selected, err := parseSpotifyInstallChoices(recommendation, []byte(versionsFixture))
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
			choices, selected, err := parseSpotifyInstallChoices("1.2.88.483", []byte(body))
			if err == nil || len(choices) != 0 || selected != -1 {
				t.Fatalf("got choices=%v selected=%d err=%v", choices, selected, err)
			}
		})
	}
}

func TestLoadspotNoSupportedVersion(t *testing.T) {
	for _, recommendation := range []string{"1.4.0.0", "", "invalid"} {
		if _, _, err := parseSpotifyInstallChoices(recommendation, []byte(versionsFixture)); err == nil {
			t.Fatalf("expected error for recommendation %q", recommendation)
		}
	}
}

func TestLoadspotSkipsInvalidEntriesAlongsideValidOnes(t *testing.T) {
	body := strings.Replace(versionsFixture, "https://example.com/newest-x64.exe", "https://example.com/not-an-installer", 1)
	choices, selected, err := parseSpotifyInstallChoices("1.2.88.483", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 2 || selected != 1 || choices[0].BaseVersion != "1.2.99.317" {
		t.Fatalf("unexpected choices: %+v, selection %d", choices, selected)
	}
}

func TestRecommendationComesFromConfig(t *testing.T) {
	got, err := extractMinimumVersionFromConfig([]byte("; BlockTheSpot\r\n; 1.2.88.464\r\n[settings]\r\n"))
	if err != nil || got != "1.2.88.464" {
		t.Fatalf("version=%q err=%v", got, err)
	}
	if _, err := extractMinimumVersionFromConfig([]byte("[settings]\n")); err == nil {
		t.Fatal("expected missing marker error")
	}
}
