# BlockTheSpot Installer

![BlockTheSpot icon](assets/blockthespot.png)

[![Build status](https://github.com/Nuzair46/BlockTheSpot-Installer/actions/workflows/ci-release.yml/badge.svg?branch=main)](https://github.com/Nuzair46/BlockTheSpot-Installer/actions/workflows/ci-release.yml)  [![Discord](https://discord.com/api/guilds/807273906872123412/widget.png)](https://discord.gg/eYudMwgYtY) <img src="https://img.shields.io/github/downloads/Nuzair46/blockthespot-installer/total.svg" />

Official installer for [BlockTheSpot](https://github.com/mrpond/BlockTheSpot).

## Install

1. Download latest [BlockTheSpotInstaller.exe](https://github.com/Nuzair46/BlockTheSpot-installer/releases/latest/download/BlockTheSpotInstaller.exe).
2. Close Spotify if it is running.
3. Run `BlockTheSpotInstaller.exe`.
4. In the installer window, keep defaults unless you need to change them:
   - `Uninstall Microsoft Store Spotify if detected` should usually stay enabled.
   - Enable `Update or reinstall Spotify before patching` if your Spotify install is missing/outdated.
5. Click `Install / Patch`.
6. Wait for completion and launch Spotify.

## Development

### Prerequisite (before local build)

Generate the Windows icon resource object:

```bash
go run github.com/akavel/rsrc@v0.10.2 -ico assets/blockthespot.ico -arch amd64 -o windows_icon_resource_amd64.syso
```

### Build locally (on Windows)

```powershell
go build -o BlockTheSpotInstaller.exe .
```

### Cross-build from Linux/macOS

```bash
GOOS=windows GOARCH=amd64 go build -o BlockTheSpotInstaller.exe .
```
