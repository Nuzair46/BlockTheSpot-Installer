#!/usr/bin/env python3
"""Check a BlockTheSpot config.ini's Buffer_modify signatures against a real Spotify bundle.

The bundle may be an extracted directory or the Apps/xpui.spa file itself (it is a zip).

A signature that no longer matches silently does nothing: the ads it was meant to hide keep
showing. Run this whenever a kit's config.ini is written or a new Spotify build is pinned.

Get a bundle by extracting Apps/xpui.spa (a zip) from any Spotify package of that version, e.g.
the macOS one, whose web bundle is the same build as Windows:

    tar xjf spotify-autoupdate-<version>-x86_64.tbz Contents/Resources/Apps/xpui.spa
    unzip -q -d xpui Contents/Resources/Apps/xpui.spa
    python3 scripts/verify-config.py src/BlockTheSpot.Core/Patch/current/config.ini xpui

Exits non-zero if any signature does not match, so it can gate a kit update.
"""
import os
import sys
import zipfile


def parse(path):
    config, section = {}, None
    with open(path, encoding="utf-8", errors="replace") as handle:
        for line in handle:
            line = line.strip()
            if not line or line.startswith(";"):
                continue
            if line.startswith("["):
                section = line[1:-1]
                config[section] = {}
            elif "=" in line and section:
                key, value = line.split("=", 1)
                config[section][key.strip()] = value.strip()
    return config


def pattern(signature):
    return [None if token == "??" else int(token, 16) for token in signature.split()]


def find(data, pat):
    span = len(pat)
    for i in range(len(data) - span + 1):
        if all(byte is None or data[i + j] == byte for j, byte in enumerate(pat)):
            return i
    return -1


class Bundle:
    """Reads bundle members from an extracted directory or straight out of an xpui.spa zip."""

    def __init__(self, path):
        self.archive = zipfile.ZipFile(path) if os.path.isfile(path) else None
        self.path = path

    def read(self, name):
        if self.archive is None:
            full = os.path.join(self.path, name)
            return open(full, "rb").read() if os.path.exists(full) else None
        try:
            return self.archive.read(name)
        except KeyError:
            return None


def main(config_path, bundle_path):
    config = parse(config_path)
    bundle = Bundle(bundle_path)
    total = matched = 0
    for key, name in sorted(config.get("Buffer_modify", {}).items()):
        if key.lower() == "enable":
            continue
        data = bundle.read(name)
        if data is None:
            print(f"  MISSING FILE  {name}")
            total += 1
            continue
        for _, section_name in sorted(config.get(name, {}).items()):
            section = config.get(section_name)
            if section is None:
                print(f"  MISSING SECTION  [{section_name}]")
                total += 1
                continue
            for signature_key in sorted(k for k in section if k.startswith("Signature")):
                total += 1
                at = find(data, pattern(section[signature_key]))
                if at >= 0:
                    matched += 1
                print(f"  [{section_name:22}] {signature_key:12} {name:32} "
                      f"{f'match @0x{at:x}' if at >= 0 else 'NO MATCH'}")
    print(f"\n  {matched}/{total} signatures match {os.path.basename(bundle_path)}")
    return 0 if matched == total else 1


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    sys.exit(main(sys.argv[1], sys.argv[2]))
