Ads blocked again on 1.2.96+, and the About panel keeps its information.

- **Home and player ads.** In Spotify 1.3.x the ad state moved out of the per-view chunks the old config patched (`1602.js`, `home-hpto.js`, `dwp-top-bar.js`) and into one Redux reducer in `xpui.js`. The current kit never touched it, so `adsEnabled` stayed on and ads kept rendering. The kit now patches that reducer directly: `ADS_ENABLED` can no longer set `adsEnabled`, `ADS_HPTO_HIDDEN` is forced hidden, and `ADS_PREMIUM` is forced on. Those three gates feed the home page, the player, the leaderboard and the upsell surfaces.
- **About panel.** `config.ini` no longer blocks `/desktop-update/`; that endpoint is what the About panel reads for its version and update status, and blocking it left the panel empty.
- **Auto-update is stopped a different way.** Because that URL is now reachable, the installer and `install.ps1` lock Spotify's updater instead, by putting a read-only file where `%LOCALAPPDATA%\Spotify\Update` would go. Restoring Spotify, or turning the patch off, releases it.
- **Signatures are checked against the real build.** Every `Buffer_modify` signature in the current kit was matched byte for byte against the `xpui.spa` of Spotify 1.3.1.234 (9/9). `scripts/verify-config.py` does this check and is the way to validate a kit before pinning a new Spotify build.

**Downloads:** `BlockTheSpotInstaller.exe`, plus `chrome_elf.dll`, `blockthespot.dll` and `config.ini` for a manual install. Their SHA-256 are listed below.

Open the app normally, without "Run as administrator." The Spotify setup and patch target the current Windows account. The executable is not code-signed; Spotify's downloaded installer is signature-checked before it runs.

Verification: 84 core regression tests on Linux and Windows, 23 catalog/site checks, the native Windows startup/render test in both themes, and the 9 kit signatures matched against Spotify 1.3.1.234. The patched client itself is not exercised on GitHub's runners.
