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

1. Download latest [BlockTheSpotInstaller.exe](https://github.com/Nuzair46/BlockTheSpot-Installer/releases/latest/download/BlockTheSpotInstaller.exe).
2. Close Spotify if it is running.
3. Run `BlockTheSpotInstaller.exe`.
4. Choose one action:
   - `Install / Patch` to install or update BlockTheSpot.
   - `Uninstall / Restore` to remove BlockTheSpot and restore original `chrome_elf.dll` when backup exists.
5. Choose the Spotify Windows x64 version you want to install. The list only shows the recommended version from `config.ini` and newer supported versions, and the recommended one is preselected.
6. Enable `Update or reinstall Spotify before patching` when you want to install the selected Spotify version before patching.
7. If `Launch Spotify and close installer after completion` is enabled, Spotify starts and the installer closes automatically.

## Development

### Prerequisite (before local build)

Generate the Windows app resources object (required for `walk`):

```bash
go run github.com/akavel/rsrc@v0.10.2 -manifest assets/app.manifest -ico assets/blockthespot.ico -arch amd64 -o windows_app_resources_amd64.syso
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
go run github.com/akavel/rsrc@v0.10.2 -manifest assets/app.manifest -ico assets/blockthespot.ico -arch amd64 -o windows_app_resources_amd64.syso
```
