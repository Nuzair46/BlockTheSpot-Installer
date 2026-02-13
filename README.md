# BlockTheSpot Installer

![BlockTheSpot icon](assets/blockthespot.png)

Windows-only GUI installer for BlockTheSpot.

## Install

1. Download `BlockTheSpotInstaller.exe` from the latest GitHub Release.
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
