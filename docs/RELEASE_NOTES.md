A BlockTheSpot section inside Spotify's own settings.

- **Settings panel.** The installer adds a `BlockTheSpot` section to the top of Spotify's settings: whether the patch is active, which kit and Spotify build it applied to, whether auto-update is locked, switches for hiding ad slots, the Premium upsell and the "download the app" prompts, and a collapsed **A/B and feature flags** list.
- **About shows the patch.** The version line in Spotify's About area gets a "BlockTheSpot is active" line beside it, with the same kit and build detail.
- **How it is added.** One script file plus one tag in `index.html`, inside `Apps/xpui.spa`. Spotify's own bundle is kept untouched next to it, so **Restore original Spotify** (or `install.ps1 -Restore`) puts back the exact original file, and re-running never stacks changes. The rebuilt bundle is opened and checked before it replaces the original: if anything about it fails, Spotify's file is left alone, because a corrupt bundle stops Spotify from starting.
- **Turning it off.** Clear **Add BlockTheSpot panel to Spotify** in the installer, or pass `-NoPanel` to `install.ps1`. Either one also removes a panel a previous run added.
- The flag list reads what the client keeps locally; Spotify decides most flags server-side, so an override may be ignored or reset. The panel says so.

**Downloads:** `BlockTheSpotInstaller.exe`, plus `chrome_elf.dll`, `blockthespot.dll` and `config.ini` for a manual install. Their SHA-256 are listed below.

Open the app normally, without "Run as administrator." The Spotify setup and patch target the current Windows account. The executable is not code-signed; Spotify's downloaded installer is signature-checked before it runs.

Verification: 89 core regression tests on Linux and Windows, including a bundle round-trip that asserts restoring returns the byte-exact original, that a second run does not stack, and that a bundle missing `index.html` is refused rather than rewritten; the same round-trip was run against the PowerShell path. 23 catalog/site checks and the native Windows startup/render test in both themes. The injected panel itself is not exercised on GitHub's runners.
