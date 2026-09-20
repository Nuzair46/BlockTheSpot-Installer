# BlockTheSpot Installer

Install [BlockTheSpot](https://github.com/Nuzair46/BlockTheSpot) — the Spotify desktop ad blocker — on Windows, on **any** Spotify version. The right patch kit is picked automatically for your build, and one click puts Spotify back.

[**Download the installer**](https://github.com/RobyRew/BlockTheSpot-Installer/releases/latest/download/BlockTheSpotInstaller.exe) · [Spotify version library](https://robyrew.github.io/BlockTheSpot-Installer/) · [All releases](https://github.com/RobyRew/BlockTheSpot-Installer/releases/latest)

> Two kits ship inside the installer: **legacy** for Spotify 1.2.70–1.2.95 and **current** for 1.2.96+. The installer and the script choose for you from the installed version.

## Install

Pick one of the four ways below. None of them need administrator rights — Spotify installs per Windows account.

### 1 · Installer app (easiest)

1. [Download `BlockTheSpotInstaller.exe`](https://github.com/RobyRew/BlockTheSpot-Installer/releases/latest/download/BlockTheSpotInstaller.exe) and open it normally, **not** as administrator.
2. Leave the defaults and press **Install BlockTheSpot**. To undo, press **Restore original Spotify**.

### 2 · PowerShell (one line)

```powershell
iwr -useb https://robyrew.github.io/BlockTheSpot-Installer/install.ps1 | iex
```

That patches the Spotify you already have. More options:

```powershell
# Save the script, then:
.\install.ps1                       # patch the installed Spotify
.\install.ps1 -Version latest       # install the newest Spotify, then patch
.\install.ps1 -Version 1.2.93.667.g7b5cc0ce   # install a specific build, then patch
.\install.ps1 -Kit legacy           # force the legacy kit
.\install.ps1 -NoPanel              # patch, but no panel in Spotify's settings
.\install.ps1 -Restore              # restore Spotify's original files
```

### 3 · Installer options

| Option | What it does |
|---|---|
| **Spotify version** | Two pins: **1.3.1.234** (current kit, default) and **1.2.93.667** (legacy kit). |
| **All versions** | Lists every build in the [library](https://robyrew.github.io/BlockTheSpot-Installer/); type a full version or an installer link to add one. |
| **Install this Spotify version** | Off keeps your installed Spotify and patches it in place. |
| **Apply BlockTheSpot patch** | Off installs only Spotify, any version, and removes a previous patch. |
| **Replace Microsoft Store edition** | Swaps the Store app for the desktop app (this account only). |
| **Add BlockTheSpot panel to Spotify** | Adds a BlockTheSpot section to Spotify's settings and marks its About version as patched. Clear it (or use `-NoPanel`) to skip and remove it. |

The **Method** chip on each build shows which kit it uses; the kit is always chosen from the version that will run.

### 4 · Manual

1. Close Spotify.
2. Find your Spotify version (Settings → About), then download `chrome_elf.dll`, `blockthespot.dll` and `config.ini`:
   - **Spotify 1.2.96+** → the [latest release](https://github.com/RobyRew/BlockTheSpot-Installer/releases/latest) here.
   - **Spotify 1.2.95 and older** → [upstream's 1.2.93.667 release](https://github.com/Nuzair46/BlockTheSpot/releases/tag/v1.2.93.667-build.8) (the legacy kit, unchanged).
3. Open `%APPDATA%\Spotify`. Rename `chrome_elf.dll` to `chrome_elf_required.dll` (this backs up the original).
4. Copy the three downloaded files into `%APPDATA%\Spotify`, overwriting.
5. Start Spotify.

**Restore manually:** close Spotify, delete `blockthespot.dll`, `config.ini` and `chrome_elf.dll` from `%APPDATA%\Spotify`, rename `chrome_elf_required.dll` back to `chrome_elf.dll`, start Spotify.

## How it works

BlockTheSpot proxies Spotify's `chrome_elf.dll` and reads `config.ini` at startup. Spotify's ad-block hook site changed at 1.2.96, so two kits are bundled: **legacy** is the upstream Nuzair46 build; **current** is that build adapted for 1.2.96+ (`config.ini` rebuilt for the newer `xpui`). `Compatibility.Kits` maps each Spotify version to a kit — the newest kit is always the current one, and every download is checked against Spotify's Authenticode signature before setup runs.

The **panel** is one script plus one tag added inside `Apps/xpui.spa`; Spotify's untouched bundle is kept beside it, so restoring is a copy back and re-running rebuilds from the original instead of stacking. The rebuilt bundle is validated before it replaces the original, and the injected script is wrapped so a selector that stops matching costs a hidden element rather than a working client.

Auto-update is stopped in the client's own request path: Spotify asks about updates at `/desktop-update/` but downloads the package from `upgrade.scdn.co/upgrade/client/`, so the kit blocks the download and leaves the status query alone. The About panel keeps its version and update status, and nothing can land on top of the patch. `%LOCALAPPDATA%\Spotify\Update` is locked as well; restoring, or turning the patch off, releases it.

A kit's `config.ini` only works if its byte signatures match the Spotify build it targets, and a signature that stops matching fails silently. Spotify ships the same web bundle (`Apps/xpui.spa`) on every desktop platform, so any build can be checked from the macOS package without Windows:

```sh
node scripts/check-kit.mjs             # newest build in the catalog
node scripts/check-kit.mjs 1.3.1.234   # a specific build
```

It downloads that build's package, reads the bundle and verifies the kit that version maps to. The catalog refresh runs it whenever a new Spotify build appears, so a kit that needs rebuilding shows up in the run summary. `scripts/verify-config.py` does the check alone against an `xpui.spa` or an extracted directory.

The [version library](https://robyrew.github.io/BlockTheSpot-Installer/) is a searchable catalog of Spotify installers for Windows, macOS and Linux, with SHA-256 hashes a watcher takes from Spotify's own servers. See [docs/SPOTIFY_DOWNLOADS.md](docs/SPOTIFY_DOWNLOADS.md) for how the links and hashes are sourced.

Support the artists you listen to — consider [Spotify Premium](https://www.spotify.com/premium/).

## Build

.NET 10 (`global.json`) and Node 24. The app is a WPF/Fluent front end over a cross-platform core; patch kits are embedded from `src/BlockTheSpot.Core/Patch`.

```sh
dotnet test tests/BlockTheSpot.Tests/BlockTheSpot.Tests.csproj -c Release
node --test scripts/test-site.mjs
npm ci --prefix site && npm run build --prefix site
```

Publish the self-contained EXE on Windows:

```powershell
dotnet publish src/BlockTheSpot.App/BlockTheSpot.App.csproj -c Release -r win-x64 --self-contained true -p:PublishSingleFile=true -p:IncludeNativeLibrariesForSelfExtract=true -p:EnableCompressionInSingleFile=true -o dist
```

Releases are cut from **Actions → Release installer**, which attaches four files: the EXE and the current kit's `chrome_elf.dll`, `blockthespot.dll` and `config.ini`, with their SHA-256 listed in the release notes. `install.ps1` is served from GitHub Pages. The version library and its JSON APIs deploy from GitHub Pages; a scheduled watcher records new Spotify builds. Not affiliated with Spotify.
