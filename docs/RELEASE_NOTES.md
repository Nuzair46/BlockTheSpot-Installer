Updates are stopped in the client's own request path, and more ad endpoints are blocked.

- **Auto-update blocked in logic.** Spotify asks about updates at `/desktop-update/` and downloads the package from `upgrade.scdn.co/upgrade/client/`. Those are different requests, so the kit now blocks the download path and leaves the status query alone: the About panel keeps showing its version and update status, and no update can be fetched over the patch. Locking `%LOCALAPPDATA%\Spotify\Update` stays as a second line of defence.
- **More ad and tracking endpoints blocked**, all read out of the shipped bundle rather than guessed: podcast leave-behind ads (`/leavebehinds/ads`), the sponsored-recommendations frame, the ad-transparency metadata call (`/dsa-metadata`), Spotify's tracking pixels (`pixel.spotify.com`, `pixel-static.spotify.com`) and the retargeting pixel loader.
- **"Update available" badge** can be hidden from the BlockTheSpot panel; it is on by default, since the update it advertises is blocked anyway.

The `/desktop-update/` status query stays reachable on purpose. Blocking it is what emptied the About panel before, and blocking the download is what actually stops an update.

**Downloads:** `BlockTheSpotInstaller.exe`, plus `chrome_elf.dll`, `blockthespot.dll` and `config.ini` for a manual install. Their SHA-256 are listed below.

Open the app normally, without "Run as administrator." The Spotify setup and patch target the current Windows account. The executable is not code-signed; Spotify's downloaded installer is signature-checked before it runs.

Verification: 89 core regression tests on Linux and Windows, 23 catalog/site checks, the native Windows startup/render test in both themes, and the 9 kit signatures matched against Spotify 1.3.1.234 with `scripts/verify-config.py`. The blocked endpoints are taken from that bundle; the running client is not exercised on GitHub's runners.
