<#
.SYNOPSIS
    Install BlockTheSpot into the Spotify desktop app, or restore the original files.

.DESCRIPTION
    Patches the Spotify desktop client for the current Windows account. The right BlockTheSpot kit
    is chosen from the Spotify version: the legacy kit for Spotify 1.2.70-1.2.95, the current kit for
    1.2.96 and newer. Kit files are downloaded from this repository's release. Nothing here needs
    administrator rights, and -Restore puts Spotify back exactly as it was.

.PARAMETER Version
    Install this Spotify version first, then patch it. A full build such as 1.3.1.234.g59d6bf59, or
    'latest' for Spotify's current release. Omit to patch the Spotify already installed.

.PARAMETER Kit
    Which bundled kit to apply: 'auto' (default, chosen from the version), 'legacy' or 'current'.

.PARAMETER Restore
    Remove BlockTheSpot and restore Spotify's original files.

.PARAMETER Launch
    Start Spotify when finished.

.PARAMETER NoPanel
    Skip the BlockTheSpot section in Spotify's settings (and remove it if a previous run added it).

.PARAMETER Tag
    Release tag to pull the kit from. Default 'latest'.

.EXAMPLE
    iwr -useb https://raw.githubusercontent.com/RobyRew/BlockTheSpot-Installer/main/scripts/install.ps1 | iex
    Patch the installed Spotify with the matching kit.

.EXAMPLE
    .\install.ps1 -Version latest -Launch
    Install the current Spotify, patch it, and open it.

.EXAMPLE
    .\install.ps1 -Restore
    Restore Spotify's original files.
#>
#Requires -Version 5.1
[CmdletBinding(DefaultParameterSetName = 'Patch')]
param(
    [Parameter(ParameterSetName = 'Patch')][string]$Version,
    [Parameter(ParameterSetName = 'Patch')][ValidateSet('auto', 'legacy', 'current')][string]$Kit = 'auto',
    [Parameter(ParameterSetName = 'Patch')][switch]$Launch,
    [Parameter(ParameterSetName = 'Patch')][switch]$NoPanel,
    [Parameter(ParameterSetName = 'Restore')][switch]$Restore,
    [string]$Tag = 'latest'
)

$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$Repository   = 'RobyRew/BlockTheSpot-Installer'
$SpotifyDir   = Join-Path $env:APPDATA 'Spotify'
$SpotifyExe   = Join-Path $SpotifyDir 'Spotify.exe'
$Cut          = [version]'1.2.96.0'
$LegacyFloor  = [version]'1.2.70.0'
# The Spotify version catalog lives in its own repository.
$Feed         = 'https://robyrew.github.io/spotify-versions-history/api/v1/windows-x64.json'
$RawBase      = "https://raw.githubusercontent.com/$Repository/main"
$PatchNames   = @('chrome_elf.dll', 'blockthespot.dll', 'config.ini')
# This release carries the current kit only. The legacy kit is upstream's 1.2.93.667 release,
# which is the same chrome_elf.dll, blockthespot.dll and config.ini, byte for byte.
$LegacyKit    = 'https://github.com/Nuzair46/BlockTheSpot/releases/download/v1.2.93.667-build.8'
$BackupName   = 'chrome_elf_required.dll'
# Spotify stages updates in %LOCALAPPDATA%\Spotify\Update. A read-only file with that name leaves
# the updater unable to create the folder, so an update cannot land on top of the patch. config.ini
# deliberately does not block /desktop-update/, so the About panel keeps its version and status.
$UpdatePath   = Join-Path $env:LOCALAPPDATA 'Spotify\Update'
# The settings panel is one extra script inside Apps/xpui.spa plus a tag in its index.html.
$SpaPath      = Join-Path $SpotifyDir 'Apps\xpui.spa'
$SpaBackup    = "$SpaPath.bts-backup"
$PanelName    = 'blockthespot-ui.js'
$PanelSource  = "$RawBase/src/BlockTheSpot.Core/Patch/$PanelName"

function Write-Banner {
    Write-Host ''
    Write-Host '  ██  BlockTheSpot  ' -ForegroundColor Black -BackgroundColor Green -NoNewline
    Write-Host '  Spotify desktop patch' -ForegroundColor Green
    Write-Host ''
}
function Write-Step($text)  { Write-Host '  ▸ ' -ForegroundColor Green -NoNewline; Write-Host $text }
function Write-Info($text)  { Write-Host '    ' -NoNewline; Write-Host $text -ForegroundColor DarkGray }
function Write-Done($text)  { Write-Host '  ✓ ' -ForegroundColor Green -NoNewline; Write-Host $text }
function Fail($text)        { Write-Host '  ✗ ' -ForegroundColor Red -NoNewline; Write-Host $text -ForegroundColor Red; exit 1 }

# Four dotted numbers, ignoring the .g<hash> suffix, as a comparable [version].
function ConvertTo-Version($text) {
    if (-not $text) { return $null }
    $parts = ($text -split '\.')[0..3]
    if ($parts.Count -lt 4) { return $null }
    try { return [version]($parts -join '.') } catch { return $null }
}

function Get-InstalledVersion {
    if (-not (Test-Path $SpotifyExe)) { return $null }
    return (Get-Item $SpotifyExe).VersionInfo.ProductVersion
}

function Resolve-Kit($version) {
    if ($Kit -ne 'auto') { return $Kit }
    $parsed = ConvertTo-Version $version
    if (-not $parsed) { return 'current' }
    if ($parsed -lt $LegacyFloor) { Fail "Spotify $version is older than the earliest supported build ($LegacyFloor)." }
    if ($parsed -lt $Cut) { return 'legacy' } else { return 'current' }
}

function Get-KitBase($kit) {
    if ($kit -eq 'legacy') { return $LegacyKit }
    if ($Tag -eq 'latest') { return "https://github.com/$Repository/releases/latest/download" }
    return "https://github.com/$Repository/releases/download/$Tag"
}

function Invoke-Download($uri, $destination) {
    for ($attempt = 1; ; $attempt++) {
        try { Invoke-WebRequest -Uri $uri -OutFile $destination -UseBasicParsing; return }
        catch {
            if ($attempt -ge 4) { throw }
            Write-Info "retry $attempt after: $($_.Exception.Message)"
            Start-Sleep -Seconds ($attempt * 2)
        }
    }
}

function Set-UpdatesBlocked([bool]$blocked) {
    try {
        if ($blocked) {
            if (Test-Path $UpdatePath -PathType Container) { Remove-Item $UpdatePath -Recurse -Force }
            if (-not (Test-Path $UpdatePath)) {
                New-Item -ItemType Directory -Path (Split-Path $UpdatePath) -Force | Out-Null
                New-Item -ItemType File -Path $UpdatePath -Force | Out-Null
            }
            Set-ItemProperty -Path $UpdatePath -Name IsReadOnly -Value $true
            Write-Info 'Spotify auto-update blocked'
        }
        elseif (Test-Path $UpdatePath -PathType Leaf) {
            Set-ItemProperty -Path $UpdatePath -Name IsReadOnly -Value $false
            Remove-Item $UpdatePath -Force
            Write-Info 'Spotify auto-update re-enabled'
        }
    }
    catch { Write-Info "could not change the updater lock: $($_.Exception.Message)" }
}

function Restore-Panel {
    if (Test-Path $SpaBackup) {
        Copy-Item $SpaBackup $SpaPath -Force
        Remove-Item $SpaBackup -Force
        Write-Info 'Spotify settings panel removed'
    }
}

# Rebuilds xpui.spa from the untouched backup with the panel added, and only swaps it in once the
# rebuilt bundle opens cleanly and carries both the script and the tag. A corrupt bundle would stop
# Spotify from starting, so a failure here leaves Spotify's own file in place.
function Add-Panel($kit, $spotifyVersion) {
    if (-not (Test-Path $SpaPath)) { Write-Info 'no xpui.spa in this installation; panel skipped'; return }
    try { Add-Type -AssemblyName System.IO.Compression.FileSystem -ErrorAction SilentlyContinue } catch { }
    $temp = "$SpaPath.bts-new"
    try {
        $panel = (Invoke-WebRequest -Uri $PanelSource -UseBasicParsing).Content
        if ($panel -isnot [string]) { $panel = [Text.Encoding]::UTF8.GetString($panel) }
        $info = @{ appVersion = 'install.ps1'; kit = $kit; spotifyVersion = $spotifyVersion; updatesBlocked = $true } | ConvertTo-Json -Compress
        $script = "window.__BTS_INFO__ = $info;`n$panel"
        $tag = '<script defer="defer" src="/' + $PanelName + '"></script>'

        if (-not (Test-Path $SpaBackup)) { Copy-Item $SpaPath $SpaBackup -Force }
        if (Test-Path $temp) { Remove-Item $temp -Force }

        $source = [IO.Compression.ZipFile]::OpenRead($SpaBackup)
        $output = [IO.Compression.ZipFile]::Open($temp, [IO.Compression.ZipArchiveMode]::Create)
        try {
            $expected = 0
            foreach ($entry in $source.Entries) {
                if ($entry.FullName -eq $PanelName) { continue }
                $expected++
                $copy = $output.CreateEntry($entry.FullName)
                $reader = $entry.Open(); $writer = $copy.Open()
                try {
                    if ($entry.FullName -eq 'index.html') {
                        $text = (New-Object IO.StreamReader($reader)).ReadToEnd()
                        if ($text -notlike "*$PanelName*") {
                            $at = $text.LastIndexOf('</body>')
                            $text = if ($at -lt 0) { $text + $tag } else { $text.Insert($at, $tag) }
                        }
                        $bytes = [Text.Encoding]::UTF8.GetBytes($text)
                        $writer.Write($bytes, 0, $bytes.Length)
                    }
                    else { $reader.CopyTo($writer) }
                }
                finally { $writer.Dispose(); $reader.Dispose() }
            }
            $added = $output.CreateEntry($PanelName).Open()
            try { $bytes = [Text.Encoding]::UTF8.GetBytes($script); $added.Write($bytes, 0, $bytes.Length) }
            finally { $added.Dispose() }
            $expected++
        }
        finally { $output.Dispose(); $source.Dispose() }

        $check = [IO.Compression.ZipFile]::OpenRead($temp)
        try {
            $ok = ($check.Entries.Count -eq $expected) -and ($check.GetEntry($PanelName)) -and ($check.GetEntry('index.html'))
        }
        finally { $check.Dispose() }
        if (-not $ok) { throw 'the rebuilt bundle did not validate' }

        Move-Item $temp $SpaPath -Force
        Write-Info 'Spotify settings panel added'
    }
    catch {
        if (Test-Path $temp) { Remove-Item $temp -Force -ErrorAction SilentlyContinue }
        Write-Info "settings panel not added: $($_.Exception.Message)"
    }
}

function Stop-Spotify {
    Get-Process -Name Spotify -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 400
}

function Install-Spotify($requested) {
    $setup = Join-Path ([IO.Path]::GetTempPath()) ("SpotifySetup-" + [guid]::NewGuid().ToString('N') + '.exe')
    $url = 'https://download.scdn.co/SpotifyFullSetupX64.exe'
    if ($requested -and $requested -ne 'latest') {
        Write-Info 'looking up the version in the catalog'
        $feed = Invoke-RestMethod -Uri $Feed -UseBasicParsing
        $short = (($requested -split '\.')[0..3]) -join '.'
        $entry = $feed.$short
        if (-not $entry) { Fail "Spotify $requested is not in the catalog. Browse https://robyrew.github.io/spotify-versions-history/ for available builds." }
        $url = $entry.win.x64.url
        Write-Info "source: $url"
    }
    Write-Step "Downloading Spotify$(if ($requested) { " $requested" })"
    Invoke-Download $url $setup
    Write-Step 'Running Spotify setup'
    Stop-Spotify
    Start-Process -FilePath $setup -Wait
    Remove-Item $setup -ErrorAction SilentlyContinue
    # Setup relaunches Spotify; close it before touching its files.
    Stop-Spotify
}

function Invoke-Restore {
    Write-Banner
    if (-not (Test-Path $SpotifyDir)) { Fail "Spotify is not installed for this account ($SpotifyDir)." }
    $backup = Join-Path $SpotifyDir $BackupName
    if (-not (Test-Path $backup)) {
        if (Test-Path (Join-Path $SpotifyDir 'blockthespot.dll')) {
            Fail 'The original chrome_elf.dll backup is missing. Reinstall Spotify to restore its files.'
        }
        Write-Done 'Spotify is already unpatched.'
        return
    }
    Write-Step 'Restoring Spotify''s original files'
    Stop-Spotify
    Copy-Item $backup (Join-Path $SpotifyDir 'chrome_elf.dll') -Force
    Remove-Item (Join-Path $SpotifyDir 'blockthespot.dll') -ErrorAction SilentlyContinue
    Remove-Item (Join-Path $SpotifyDir 'config.ini') -ErrorAction SilentlyContinue
    Remove-Item $backup -ErrorAction SilentlyContinue
    Restore-Panel
    Set-UpdatesBlocked $false
    Write-Done 'Original Spotify files restored.'
}

function Invoke-Patch {
    Write-Banner

    $reinstall = [bool]$Version
    if ($reinstall) { Install-Spotify $Version }

    $installed = Get-InstalledVersion
    if (-not $installed) { Fail "Spotify is not installed. Run with -Version latest to install it first." }

    $kit = Resolve-Kit $installed
    Write-Step "Spotify $installed detected"
    Write-Info "kit: $kit  (legacy = kernel32 IAT ≤ 1.2.95, current = api-ms IAT ≥ 1.2.96)"

    # Fetch the kit into a temp folder: chrome_elf.dll (shared) + this kit's blockthespot.dll and config.ini.
    $staging = Join-Path ([IO.Path]::GetTempPath()) ("BlockTheSpot-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $staging -Force | Out-Null
    try {
        $base = Get-KitBase $kit
        Write-Step 'Fetching the BlockTheSpot kit'
        Write-Info "source: $base"
        foreach ($name in $PatchNames) {
            Write-Info "downloading $name"
            Invoke-Download "$base/$name" (Join-Path $staging $name)
        }

        $chrome = Join-Path $SpotifyDir 'chrome_elf.dll'
        $backup = Join-Path $SpotifyDir $BackupName
        if (-not (Test-Path $chrome) -and -not (Test-Path $backup)) {
            Fail 'Spotify''s original chrome_elf.dll is missing. Reinstall Spotify before patching.'
        }

        Write-Step "Applying BlockTheSpot ($kit kit)"
        Stop-Spotify
        # Back up the original proxy once; a reinstall refreshes it, an existing patch keeps its backup.
        if ((Test-Path $chrome) -and ($reinstall -or -not (Test-Path $backup))) {
            Copy-Item $chrome $backup -Force
        }
        foreach ($name in $PatchNames) {
            Copy-Item (Join-Path $staging $name) (Join-Path $SpotifyDir $name) -Force
        }
        Set-UpdatesBlocked $true
        if ($NoPanel) { Restore-Panel } else { Add-Panel $kit $installed }
        Write-Done "Spotify $installed is patched with the $kit kit."
    }
    finally {
        Remove-Item $staging -Recurse -Force -ErrorAction SilentlyContinue
    }

    if ($Launch) { Write-Step 'Opening Spotify'; Start-Process $SpotifyExe }
    Write-Host ''
    Write-Info 'Undo any time with:  .\install.ps1 -Restore'
    Write-Host ''
}

if ($Restore) { Invoke-Restore } else { Invoke-Patch }
