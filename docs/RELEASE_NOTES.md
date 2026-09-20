The settings panel now appears, and promo popups are closed.

- **Why the panel was missing.** It was inserted into a guessed `main` container. The bundle actually renders settings into `data-testid="settings-page"` on the `/preferences` route, so the panel is now placed there — an anchor read out of Spotify's own code rather than guessed. It also logs one line to DevTools on load, so it is obvious whether the script is running at all.
- **Promo popups.** The "3 months of Premium" style dialogs are appended by the client into `inAppMessageContainer`. That container is now emptied as soon as it is filled, which is the same cleanup the client performs itself, and hidden by CSS. Hiding alone can leave the dialog holding focus, so it is emptied as well.
- **Options apply immediately.** Every switch rewrites one `<style>` element and takes effect on the spot; nothing needs a restart. Loading the panel itself still needs Spotify started once after installing.
- **Native theming.** The panel uses Spotify's own custom properties, so it follows the client's light and dark themes.

After installing, start Spotify once, then open **Settings** — the BlockTheSpot section is at the top, and the About version line carries the active status.

**Downloads:** `BlockTheSpotInstaller.exe`, plus `chrome_elf.dll`, `blockthespot.dll` and `config.ini` for a manual install. Their SHA-256 are listed below.

Open the app normally, without "Run as administrator." The Spotify setup and patch target the current Windows account. The executable is not code-signed; Spotify's downloaded installer is signature-checked before it runs.

Verification: 89 core regression tests on Linux and Windows, including the bundle round-trip that asserts restoring returns the byte-exact original; 23 catalog/site checks; the native Windows startup/render test in both themes; and the 9 kit signatures matched against Spotify 1.3.1.234. The panel's own rendering is not exercised on GitHub's runners.
