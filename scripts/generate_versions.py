#!/usr/bin/env python3

import json
import os
import re
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from typing import Dict, Iterable, Optional, Tuple

import requests
import yaml
from packaging.version import Version


REPO = "microsoft/winget-pkgs"
SPOTIFY_MANIFEST_DIR_API = (
    f"https://api.github.com/repos/{REPO}/contents/manifests/s/Spotify/Spotify"
)
RAW_INSTALLER_YAML = (
    f"https://raw.githubusercontent.com/{REPO}/master/"
    "manifests/s/Spotify/Spotify/{version}/Spotify.Spotify.installer.yaml"
)

OUTPUT_FILE = Path("versions.json")

# Inclusive lower bound requested.
MIN_FULL_VERSION = "1.2.88.464.g75c3c6a2"

BASE_URL = "https://upgrade.scdn.co/upgrade/client/"

PLATFORM_TEMPLATES = {
    "win_x86": "win32-x86/spotify_installer-{version}-{number}.exe",
    "win_x64": "win32-x86_64/spotify_installer-{version}-{number}.exe",
    "win_arm64": "win32-arm64/spotify_installer-{version}-{number}.exe",
    "mac_intel": "osx-x86_64/spotify-autoupdate-{version}-{number}.tbz",
    "mac_arm64": "osx-arm64/spotify-autoupdate-{version}-{number}.tbz",
}

URL_VERSION_NUMBER_RE = re.compile(r"-(\d+)\.(?:exe|tbz)$")
FULL_VERSION_RE = re.compile(r"^(\d+\.\d+\.\d+\.\d+)\.(g[0-9a-f]+)$", re.I)


def request_headers() -> Dict[str, str]:
    headers = {
        "Accept": "application/vnd.github+json",
        "User-Agent": "spotify-versions-json-generator",
    }

    token = os.environ.get("GITHUB_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"

    return headers


def short_version(full_version: str) -> str:
    match = FULL_VERSION_RE.match(full_version)
    if not match:
        raise ValueError(f"Unexpected Spotify version format: {full_version}")
    return match.group(1)


def numeric_version(full_version: str) -> Version:
    return Version(short_version(full_version))


def min_numeric_version() -> Version:
    return numeric_version(MIN_FULL_VERSION)


def fetch_winget_versions() -> list[str]:
    response = requests.get(
        SPOTIFY_MANIFEST_DIR_API,
        headers=request_headers(),
        timeout=30,
    )
    response.raise_for_status()

    versions = []
    for item in response.json():
        if item.get("type") != "dir":
            continue

        name = item["name"]

        try:
            if numeric_version(name) >= min_numeric_version():
                versions.append(name)
        except Exception:
            continue

    # Newest first.
    return sorted(versions, key=numeric_version, reverse=True)


def fetch_installer_manifest(full_version: str) -> dict:
    url = RAW_INSTALLER_YAML.format(version=full_version)
    response = requests.get(url, headers=request_headers(), timeout=30)
    response.raise_for_status()
    return yaml.safe_load(response.text) or {}


def extract_number_from_url(url: str) -> Optional[int]:
    match = URL_VERSION_NUMBER_RE.search(url)
    if not match:
        return None
    return int(match.group(1))


def classify_windows_url(url: str) -> Optional[Tuple[str, str]]:
    lower = url.lower()

    if "/win32-x86_64/" in lower:
        return ("x64", url)

    if "/win32-arm64/" in lower:
        return ("arm64", url)

    if "/win32-x86/" in lower:
        return ("x86", url)

    return None


def extract_windows_links(manifest: dict) -> Tuple[Dict[str, str], list[int]]:
    win = {
        "x86": "",
        "x64": "",
        "arm64": "",
    }
    numbers = []

    for installer in manifest.get("Installers", []) or []:
        url = installer.get("InstallerUrl")
        if not url:
            continue

        classified = classify_windows_url(url)
        if classified:
            arch, installer_url = classified
            win[arch] = installer_url

            number = extract_number_from_url(installer_url)
            if number is not None:
                numbers.append(number)

    return win, numbers


def make_url(template_key: str, full_version: str, number: int) -> str:
    path = PLATFORM_TEMPLATES[template_key].format(
        version=full_version,
        number=number,
    )
    return BASE_URL + path


def head_exists(session: requests.Session, url: str) -> bool:
    try:
        response = session.head(url, allow_redirects=True, timeout=12)
        return 200 <= response.status_code < 300
    except requests.RequestException:
        return False


def candidate_numbers(seed_numbers: Iterable[int]) -> list[int]:
    seeds = sorted(set(seed_numbers))

    if not seeds:
        return list(range(0, 8001))

    low = max(0, min(seeds) - 250)
    high = max(seeds) + 250

    # Spotify mac build numbers are usually very close to the Windows
    # build numbers for the same fullversion, so search locally first.
    return list(range(low, high + 1))


def find_latest_url_for_template(
    full_version: str,
    template_key: str,
    numbers: Iterable[int],
) -> str:
    urls = [
        make_url(template_key, full_version, number)
        for number in numbers
    ]

    found: list[Tuple[int, str]] = []

    with requests.Session() as session:
        with ThreadPoolExecutor(max_workers=80) as executor:
            futures = {
                executor.submit(head_exists, session, url): url
                for url in urls
            }

            for future in as_completed(futures):
                url = futures[future]
                if future.result():
                    number = extract_number_from_url(url)
                    if number is not None:
                        found.append((number, url))

    if not found:
        return ""

    # Match the Rust behavior: keep the highest build number URL.
    return sorted(found, key=lambda pair: pair[0], reverse=True)[0][1]


def find_mac_links(full_version: str, seed_numbers: list[int]) -> Dict[str, str]:
    numbers = candidate_numbers(seed_numbers)

    return {
        "intel": find_latest_url_for_template(full_version, "mac_intel", numbers),
        "arm64": find_latest_url_for_template(full_version, "mac_arm64", numbers),
    }


def load_existing_versions() -> dict:
    if not OUTPUT_FILE.exists():
        return {}

    with OUTPUT_FILE.open("r", encoding="utf-8") as file:
        return json.load(file)


def sort_versions(data: dict) -> dict:
    return {
        key: data[key]
        for key in sorted(data.keys(), key=Version, reverse=True)
    }


def build_entry(full_version: str) -> dict:
    manifest = fetch_installer_manifest(full_version)
    win_links, seed_numbers = extract_windows_links(manifest)
    mac_links = find_mac_links(full_version, seed_numbers)

    return {
        "buildType": "Release",
        "fullversion": full_version,
        "links": {
            "win": win_links,
            "mac": mac_links,
        },
    }


def main() -> None:
    existing = load_existing_versions()
    discovered_versions = fetch_winget_versions()

    changed = False

    for full_version in discovered_versions:
        key = short_version(full_version)

        if key in existing:
            continue

        print(f"Adding Spotify {full_version}")
        existing[key] = build_entry(full_version)
        changed = True

        # Be polite to GitHub/CDN.
        time.sleep(1)

    if not changed and OUTPUT_FILE.exists():
        print("No new versions.")
        return

    sorted_data = sort_versions(existing)

    with OUTPUT_FILE.open("w", encoding="utf-8") as file:
        json.dump(sorted_data, file, indent=2, ensure_ascii=False)
        file.write("\n")

    print(f"Wrote {OUTPUT_FILE}")


if __name__ == "__main__":
    main()
