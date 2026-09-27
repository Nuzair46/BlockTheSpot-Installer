<center>
	<h1 align="center">BlockTheSpot Installer</h1> 
   <h4 align="center">Official installer for a multi-purpose adblocker and skip-bypass for the <strong>Spotify for Windows (64 bit)</strong> </h4>
   <h5 align="center">Please support Spotify by purchasing premium</h5>
   <p align="center">
     <a href="https://github.com/Nuzair46/BlockTheSpot-Installer/releases"><img src="https://github.com/Nuzair46/BlockTheSpot-Installer/blob/main/assets/blockthespot.png" /></a>
   </p>
</center>

[![Build status](https://github.com/Nuzair46/BlockTheSpot-Installer/actions/workflows/ci-release.yml/badge.svg?branch=main)](https://github.com/Nuzair46/BlockTheSpot-Installer/actions/workflows/ci-release.yml)  [![Discord](https://discord.com/api/guilds/807273906872123412/widget.png)](https://discord.gg/eYudMwgYtY) <img src="https://img.shields.io/github/downloads/Nuzair46/blockthespot-installer/total.svg" />

Official installer for [BlockTheSpot](https://github.com/Nuzair46/BlockTheSpot).

## Install

1. Download [BlockTheSpotInstaller.exe](https://github.com/Nuzair46/BlockTheSpot-Installer/releases/latest/download/BlockTheSpotInstaller.exe) and run it.
2. Keep the recommended Spotify Windows x64 version selected. Downloads come from [Loadspot](https://loadspot.pages.dev/versions.json).
3. Click **Install / Patch**. Spotify is closed automatically. If the selected version differs from your installed version, the installer reinstalls Spotify before applying the patch.
4. Leave **Launch Spotify and close installer after completion** enabled to start Spotify when finished.

Use **Update or reinstall Spotify before patching** to repair the selected Spotify version, even when it is already installed. Use **Uninstall / Restore** to remove the patch and restore Spotify's matching original DLL. Your settings and diagnostic logs are kept.

### Preferences

`config.ini` contains the release's signatures. Your preferences live in
`%APPDATA%\Spotify\settings.ini`; restart Spotify after editing them. See
[settings.example.ini](https://github.com/Nuzair46/BlockTheSpot/blob/master/settings.example.ini)
for supported options.

- Updates preserve an existing `settings.ini` byte for byte, including comments.
- On the first upgrade, supported preferences in an older `config.ini` migrate into `settings.ini`. Signatures and offsets never migrate.
- Fresh installations create `settings.ini` from the release's defaults and include `settings.example.ini` for reference.
- **Reset BlockTheSpot settings to defaults** is off by default and only applies when installing. Uninstall always keeps settings.
- Spotify reinstalls protect both settings and legacy preferences, including if setup fails. If restoration is blocked, the activity log identifies the saved backup.

### Compatibility and recovery

New packs declare an exact `[Compatibility] Spotify` version. Only that version
is offered; a newer version is never substituted. Older releases that only carry
a version comment retain their original minimum-version rule and do not create or
reset settings they cannot read.

If the catalog is unavailable, use **Retry**. You can still patch an already
compatible installation, but the installer will not fall back to an unsupported
latest Spotify download. If the patch release itself cannot be loaded, installation
stays disabled until Retry succeeds. Uninstall remains available offline.

Both DLLs, the signature pack, and the settings template come from one pinned
BlockTheSpot release. New releases require `SHA256SUMS.txt`; downloads are checked
before installed files change. Spotify's executable version and original Chromium
DLL are checked before patching. A fresh stock DLL replaces an outdated backup
left behind by a Spotify update. Failed file replacements roll back earlier writes.

Use **Copy log** when reporting an installer error. After Spotify starts, runtime
results are in `%APPDATA%\Spotify\blockthespot-status.txt` and `blockthespot.log`.
The status report is created by the patch, so check its timestamp when diagnosing
a new launch.

## Development

### Prerequisite (before local build)

Generate the Windows app resources object (required for `walk`):

```bash
go run github.com/akavel/rsrc@v0.10.2 -manifest assets/app.manifest -ico assets/blockthespot.ico -arch amd64 -o app_resources_windows_amd64.syso
```

### Build locally (on Windows)

```powershell
go build -trimpath -ldflags="-H=windowsgui -X main.installerVersion=v1.0.0" -o BlockTheSpotInstaller.exe .
```

### Cross-build from Linux/macOS

```bash
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-H=windowsgui -X main.installerVersion=v1.0.0" -o BlockTheSpotInstaller.exe .
```

### If you update the icon PNG

Rebuild the `.ico`, then regenerate app resources:

```bash
convert assets/blockthespot.png -define icon:auto-resize=256,128,64,48,32,16 assets/blockthespot.ico
go run github.com/akavel/rsrc@v0.10.2 -manifest assets/app.manifest -ico assets/blockthespot.ico -arch amd64 -o app_resources_windows_amd64.syso
```

### Tests

```bash
go test ./...
go vet ./...
```

Release pinning/checksums, version selection, preference migration, and installation
file tests run on Linux/macOS. Windows CI also exercises rollback with a locked DLL,
builds the installer, and runs the full package checks. Fixtures need no Spotify files
or network access.

For an optional read-only check against a local patched installation on Windows:

```powershell
$env:BTS_SPOTIFY_TEST_DIR = "$env:APPDATA\Spotify"
go test -run TestInstalledSpotifyCompatibility -v .
```

To check release downloads and the live version catalog without installing anything:

```powershell
$env:BTS_LIVE_DOWNLOAD_TEST = '1'
go test -run TestLiveReleaseAndCatalog -v .
```

The settings allowlist in `settings.go` must stay aligned with BlockTheSpot's runtime
parser. Add new user preferences there when the patch introduces them; keep signature
fields in `config.ini`.
