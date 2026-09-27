//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

const (
	spotifyVersionsURL = "https://loadspot.pages.dev/versions.json"

	installerLatestReleaseAPI = "https://api.github.com/repos/Nuzair46/BlockTheSpot-Installer/releases/latest"
	installerReleasesURL      = "https://github.com/Nuzair46/BlockTheSpot-Installer/releases/latest"
)

type installOptions struct {
	ResetSettings       bool
	UpdateSpotify       bool
	LaunchSpotifyOnDone bool
	SpotifyVersion      spotifyInstallChoice
}

type operationMode int

const (
	operationInstall operationMode = iota
	operationUninstall
)

type installer struct {
	options     installOptions
	release     *patchRelease
	logf        func(format string, args ...any)
	setProgress func(value int)
	setStatus   func(status string)
}

type installerApp struct {
	mw              *walk.MainWindow
	logoView        *walk.ImageView
	updateInfo      *walk.LinkLabel
	resetCheck      *walk.CheckBox
	release         *patchRelease
	updateCheck     *walk.CheckBox
	versionCombo    *walk.ComboBox
	launchCheck     *walk.CheckBox
	progress        *walk.ProgressBar
	status          *walk.Label
	logView         *walk.TextEdit
	installButton   *walk.PushButton
	uninstallButton *walk.PushButton
	spotifyVersions []spotifyInstallChoice
	versionDetails  *walk.TextLabel
	retryButton     *walk.PushButton
	exitButton      *walk.PushButton
	progressLabel   *walk.Label
	busy            bool
	loadingVersions bool
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			details := fmt.Sprintf("Unhandled panic: %v\r\n\r\n%s", r, string(debug.Stack()))
			reportFatalError(details)
			os.Exit(1)
		}
	}()

	app := &installerApp{}
	if err := app.run(); err != nil {
		reportFatalError(err.Error())
		os.Exit(1)
	}
}

func (a *installerApp) run() error {
	appIcon, _ := loadAppIcon()
	if err := (MainWindow{
		AssignTo: &a.mw,
		Title:    "BlockTheSpot Installer",
		Icon:     appIcon,
		Font:     Font{Family: "Segoe UI", PointSize: 10},
		Size:     Size{Width: 760, Height: 680},
		MinSize:  Size{Width: 700, Height: 640},
		Layout:   VBox{Margins: Margins{Left: 24, Top: 20, Right: 24, Bottom: 20}, Spacing: 16},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 16},
				Children: []Widget{
					ImageView{AssignTo: &a.logoView, MinSize: Size{Width: 48, Height: 48}, MaxSize: Size{Width: 48, Height: 48}, Mode: ImageViewModeZoom},
					Composite{
						Layout: VBox{MarginsZero: true, Spacing: 4},
						Children: []Widget{
							TextLabel{Text: "BlockTheSpot", Font: Font{Family: "Segoe UI", PointSize: 20, Bold: true}},
							TextLabel{Text: "Install, patch or restore Spotify for Windows x64."},
						},
					},
				},
			},
			GroupBox{
				Title:  "Spotify setup",
				Layout: VBox{Margins: Margins{Left: 16, Top: 12, Right: 16, Bottom: 12}, Spacing: 10},
				Children: []Widget{
					Label{Text: "Spotify &version"},
					Composite{
						Layout: HBox{MarginsZero: true, Spacing: 8},
						Children: []Widget{
							ComboBox{AssignTo: &a.versionCombo, Editable: false, StretchFactor: 1,
								Model:                 []string{"Loading Windows x64 versions…"},
								OnCurrentIndexChanged: a.updateVersionDetails},
							PushButton{AssignTo: &a.retryButton, Text: "&Retry", Enabled: false, OnClicked: a.reloadSpotifyVersions},
						},
					},
					TextLabel{AssignTo: &a.versionDetails, Text: "Checking the recommended version…", MinSize: Size{Width: 100}},
					TextLabel{Text: "Settings are kept. A different selected version will reinstall Spotify before patching.", MinSize: Size{Width: 100}},
					CheckBox{AssignTo: &a.updateCheck, Text: "&Update or reinstall Spotify before patching", Checked: false},
					CheckBox{AssignTo: &a.resetCheck, Text: "&Reset BlockTheSpot settings to defaults", Checked: false, Enabled: false},
					CheckBox{AssignTo: &a.launchCheck, Text: "&Launch Spotify and close installer after completion", Checked: true},
				},
			},
			Composite{
				Layout: VBox{MarginsZero: true, Spacing: 8},
				Children: []Widget{
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							Label{AssignTo: &a.status, Text: "Ready to install"},
							HSpacer{},
							Label{AssignTo: &a.progressLabel, Text: "0%"},
						},
					},
					ProgressBar{AssignTo: &a.progress, MinValue: 0, MaxValue: 100},
				},
			},
			Composite{
				Layout:        VBox{MarginsZero: true, Spacing: 8},
				StretchFactor: 1,
				Children: []Widget{
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							Label{Text: "Activity", Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}},
							HSpacer{},
							PushButton{Text: "&Copy log", OnClicked: func() {
								if err := walk.Clipboard().SetText(a.logView.Text()); err != nil {
									walk.MsgBox(a.mw, "Copy log", err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
								}
							}},
						},
					},
					TextEdit{AssignTo: &a.logView, ReadOnly: true, VScroll: true, MinSize: Size{Height: 100},
						Font: Font{Family: "Consolas", PointSize: 9}},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 8},
				Children: []Widget{
					PushButton{AssignTo: &a.uninstallButton, Text: "Uninstall / &Restore", MinSize: Size{Height: 34}, OnClicked: a.startUninstall},
					HSpacer{},
					PushButton{AssignTo: &a.exitButton, Text: "E&xit", MinSize: Size{Width: 80, Height: 34}, OnClicked: func() { a.mw.Close() }},
					PushButton{AssignTo: &a.installButton, Text: "&Install / Patch", MinSize: Size{Width: 144, Height: 34}, OnClicked: a.startInstall},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					LinkLabel{AssignTo: &a.updateInfo, Text: "Checking installer updates…", OnLinkActivated: func(link *walk.LinkLabelLink) { _ = openExternalURL(link.URL()) }},
					HSpacer{},
					LinkLabel{Text: `<a href="https://github.com/Nuzair46/BlockTheSpot">BlockTheSpot</a> · <a href="https://github.com/Nuzair46/BlockTheSpot-Installer">Installer</a> · <a href="https://discord.gg/eYudMwgYtY">Discord</a>`,
						OnLinkActivated: func(link *walk.LinkLabelLink) { _ = openExternalURL(link.URL()) }},
				},
			},
		},
	}).Create(); err != nil {
		return err
	}
	if appIcon != nil {
		_ = a.logoView.SetImage(appIcon)
	}
	a.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		*canceled = a.busy
	})
	go a.checkForInstallerUpdate()
	a.reloadSpotifyVersions()
	a.mw.Run()
	return nil
}

func (a *installerApp) updateVersionDetails() {
	if a.versionDetails == nil {
		return
	}
	choice := a.selectedSpotifyVersion()
	if choice.FullVersion == "" {
		return
	}
	details := []string{"Windows x64"}
	if choice.Date != "" {
		details = append(details, "Released "+choice.Date)
	}
	if choice.Size > 0 {
		details = append(details, fmt.Sprintf("%.1f MiB", bytesToMiB(choice.Size)))
	}
	if choice.Recommended {
		details = append(details, "Recommended for BlockTheSpot")
	}
	_ = a.versionDetails.SetText(strings.Join(details, " · "))
}

func (a *installerApp) reloadSpotifyVersions() {
	if a.busy || a.loadingVersions {
		return
	}
	a.loadingVersions = true
	a.versionDetails.SetText("Checking the recommended version…")
	a.setBusy(false)
	go a.loadSpotifyVersionChoices()
}

func reportFatalError(details string) {
	logPath, err := writeStartupErrorLog(details)
	msg := details
	if err == nil {
		msg += "\r\n\r\nLog file:\r\n" + logPath
	}

	fmt.Fprintln(os.Stderr, details)
	_ = walk.MsgBox(nil, "BlockTheSpot Installer Error", msg, walk.MsgBoxOK|walk.MsgBoxIconError)
}

func writeStartupErrorLog(details string) (string, error) {
	dir := filepath.Join(os.TempDir(), "BlockTheSpotInstaller")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	filename := fmt.Sprintf("startup-error-%s.log", time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, filename)
	content := fmt.Sprintf("[%s] %s\r\n", time.Now().Format(time.RFC3339), details)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (a *installerApp) startInstall() {
	a.startOperation(operationInstall)
}

func (a *installerApp) startUninstall() {
	a.startOperation(operationUninstall)
}

func (a *installerApp) startOperation(mode operationMode) {
	if a.busy || (mode == operationInstall && (a.loadingVersions || a.release == nil)) {
		return
	}
	opts := installOptions{
		ResetSettings:       a.resetCheck.Checked(),
		UpdateSpotify:       a.updateCheck.Checked(),
		LaunchSpotifyOnDone: a.launchCheck.Checked(),
		SpotifyVersion:      a.selectedSpotifyVersion(),
	}

	a.setBusy(true)
	a.setProgressSafe(0)
	a.setStatusSafe("Starting")
	if mode == operationUninstall {
		a.logfSafe("Starting uninstaller.")
	} else {
		a.logfSafe("Starting installer.")
	}

	release := a.release
	go func() {
		ins := installer{
			release:     release,
			options:     opts,
			logf:        a.logfSafe,
			setProgress: a.setProgressSafe,
			setStatus:   a.setStatusSafe,
		}

		var err error
		if mode == operationUninstall {
			err = ins.runUninstall()
		} else {
			err = ins.runInstall()
		}

		a.mw.Synchronize(func() {
			a.setBusy(false)
			if err != nil {
				a.status.SetText("Failed — see the activity log")
				a.logView.AppendText("\r\nError: " + err.Error())
				walk.MsgBox(a.mw, "Installer Error", err.Error(), walk.MsgBoxIconError)
				return
			}

			a.progress.SetValue(100)
			a.progressLabel.SetText("100%")
			a.status.SetText("Completed")
			if opts.LaunchSpotifyOnDone {
				a.mw.Close()
			}
		})
	}()
}

func (a *installerApp) setBusy(busy bool) {
	a.busy = busy
	a.installButton.SetEnabled(!busy && !a.loadingVersions && a.release != nil)
	a.resetCheck.SetEnabled(!busy && !a.loadingVersions && a.release != nil && a.release.Compatibility.Exact)
	a.uninstallButton.SetEnabled(!busy)
	a.updateCheck.SetEnabled(!busy)
	a.versionCombo.SetEnabled(!busy && !a.loadingVersions && len(a.spotifyVersions) > 0)
	a.retryButton.SetEnabled(!busy && !a.loadingVersions && len(a.spotifyVersions) == 0)
	a.launchCheck.SetEnabled(!busy)
	a.exitButton.SetEnabled(!busy)
}

func (a *installerApp) logfSafe(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	a.mw.Synchronize(func() {
		current := a.logView.Text()
		if current == "" {
			a.logView.SetText(line)
		} else {
			a.logView.SetText(current + "\r\n" + line)
		}
		caret := len(a.logView.Text())
		a.logView.SetTextSelection(caret, caret)
	})
}

func (a *installerApp) setProgressSafe(value int) {
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	a.mw.Synchronize(func() {
		a.progress.SetValue(value)
		a.progressLabel.SetText(fmt.Sprintf("%d%%", value))
	})
}

func (a *installerApp) setStatusSafe(status string) {
	a.mw.Synchronize(func() {
		a.status.SetText(status)
	})
}

func (a *installerApp) setUpdateInfo(text string) {
	if a.updateInfo == nil {
		return
	}

	a.mw.Synchronize(func() {
		_ = a.updateInfo.SetText(text)
	})
}

func (a *installerApp) loadSpotifyVersionChoices() {
	release, choices, selectedIndex, err := fetchSpotifyInstallChoices()
	if err != nil {
		a.logfSafe("Warning: failed to load Spotify version list: %v", err)
	}

	a.mw.Synchronize(func() {
		a.loadingVersions = false
		a.release = release
		defer a.setBusy(a.busy)
		if err != nil {
			a.versionDetails.SetText("Unable to load a compatible download. Retry, or patch an already compatible Spotify installation.")
			if release == nil {
				a.versionDetails.SetText("Unable to load the patch release. Use Retry before installing.")
			}
			a.spotifyVersions = nil
			if a.versionCombo != nil {
				_ = a.versionCombo.SetModel([]string{"No compatible download available"})
				_ = a.versionCombo.SetCurrentIndex(0)
				a.versionCombo.SetEnabled(false)
			}
			return
		}

		a.spotifyVersions = choices
		model := make([]string, 0, len(choices))
		for _, choice := range choices {
			model = append(model, choice.Display)
		}

		if a.versionCombo != nil {
			_ = a.versionCombo.SetModel(model)
			if selectedIndex < 0 || selectedIndex >= len(model) {
				selectedIndex = 0
			}
			_ = a.versionCombo.SetCurrentIndex(selectedIndex)
			a.updateVersionDetails()
		}
	})
}

func (a *installerApp) selectedSpotifyVersion() spotifyInstallChoice {
	if a.versionCombo == nil {
		return spotifyInstallChoice{}
	}

	index := a.versionCombo.CurrentIndex()
	if index < 0 || index >= len(a.spotifyVersions) {
		return spotifyInstallChoice{}
	}

	return a.spotifyVersions[index]
}

func (a *installerApp) checkForInstallerUpdate() {
	release, err := fetchLatestInstallerRelease()
	if err != nil {
		a.setUpdateInfo(fmt.Sprintf("Installer version: %s (update check unavailable)", installerVersion))
		return
	}

	cmp, err := compareInstallerVersion(installerVersion, release.TagName)
	if err != nil {
		a.setUpdateInfo(fmt.Sprintf("Installer version: %s (latest: %s)", installerVersion, release.TagName))
		return
	}

	if cmp < 0 {
		a.setUpdateInfo(fmt.Sprintf(`Update available: <a href="%s">%s</a> (current %s)`, release.HTMLURL, release.TagName, installerVersion))
		return
	}

	a.setUpdateInfo(fmt.Sprintf("Installer is up to date (%s)", installerVersion))
}

func (i *installer) runInstall() error {
	spotifyDir := defaultSpotifyDir()
	if spotifyDir == "" {
		return errors.New("unable to determine Spotify directory")
	}
	if i.release == nil {
		return errors.New("load the BlockTheSpot release with Retry first")
	}
	compatibility := i.release.Compatibility
	i.setStatus("Downloading BlockTheSpot")
	i.logf("Using BlockTheSpot release %s for Spotify %s (exact match: %t).", i.release.Tag, compatibility.Version, compatibility.Exact)
	files, err := i.release.downloadFiles(downloadBytes)
	if err != nil {
		return err
	}
	if err := validatePatchDLLs(files); err != nil {
		return err
	}
	if compatibility.Exact {
		settings, message, err := prepareSettings(spotifyDir, files["settings.example.ini"], i.options.ResetSettings)
		if err != nil {
			return err
		}
		files["settings.ini"] = settings
		i.logf("%s", message)
	} else {
		i.logf("This older release does not support settings.ini; any existing settings.ini will be preserved.")
	}
	i.setProgress(15)

	spotifyExe := filepath.Join(spotifyDir, "Spotify.exe")
	selected := i.options.SpotifyVersion
	detected := ""
	if fileExists(spotifyExe) {
		detected, err = getSpotifyVersion(spotifyExe)
		if err != nil {
			i.logf("Unable to read installed Spotify version: %v", err)
		}
		i.logf("Installed Spotify version: %s.", detected)
	}
	needsInstall := i.options.UpdateSpotify || !compatibility.supports(detected) ||
		(selected.BaseVersion != "" && baseSpotifyVersion(detected) != selected.BaseVersion)
	if needsInstall && (selected.URL == "" || !compatibility.supports(selected.BaseVersion)) {
		return fmt.Errorf("Spotify %s is required by this patch release. Retry the version list, or install a compatible version before patching", compatibility.Version)
	}

	i.setStatus("Stopping Spotify")
	i.logf("Stopping Spotify processes.")
	stopSpotifyProcesses()

	i.setStatus("Checking Store edition")
	storeInstalled, err := isSpotifyStoreInstalled()
	if err != nil {
		i.logf("Warning: failed to check Microsoft Store Spotify: %v", err)
	} else if storeInstalled {
		i.logf("Uninstalling Microsoft Store Spotify.")
		if err := uninstallSpotifyStore(); err != nil {
			return fmt.Errorf("failed to uninstall Microsoft Store Spotify: %w", err)
		}
	}
	if needsInstall {
		i.setStatus("Installing Spotify")
		i.setProgress(20)
		if err := withPreservedSettings(spotifyDir, func() error { return i.installSpotify(spotifyExe, selected) }); err != nil {
			return err
		}
	} else {
		i.logf("Spotify update not required.")
		i.setProgress(45)
	}
	// Setup may install something different from the requested catalog entry.
	detected, err = getSpotifyVersion(spotifyExe)
	if err != nil {
		return fmt.Errorf("verify installed Spotify: %w", err)
	}
	if !compatibility.supports(detected) {
		return fmt.Errorf("installed Spotify %s does not match the patch requirement %s; no patch files were installed", detected, compatibility.Version)
	}
	if err := requireWindowsX64(spotifyExe); err != nil {
		return err
	}

	i.setStatus("Applying BlockTheSpot files")
	originalPath, err := matchingOriginalDLL(spotifyDir)
	if err != nil {
		return err
	}
	original, err := os.ReadFile(originalPath)
	if err != nil {
		return err
	}
	files["chrome_elf_required.dll"] = original
	i.logf("Using matching original DLL from %s.", originalPath)
	if err := installPatchFiles(spotifyDir, files); err != nil {
		return err
	}
	i.logf("Installed both DLLs and config.ini from %s; settings are separate from the signature pack.", i.release.Tag)
	i.setProgress(98)
	if i.options.LaunchSpotifyOnDone {
		i.setStatus("Launching Spotify")
		i.logf("Starting Spotify.")
		if err := startSpotify(spotifyExe, spotifyDir); err != nil {
			return fmt.Errorf("failed to launch Spotify: %w", err)
		}
		time.Sleep(2 * time.Second)
		running, err := processRunning("Spotify.exe")
		if err == nil && !running {
			return errors.New("Spotify did not stay running after patch. Check blockthespot.log and blockthespot-status.txt in the Spotify folder")
		}
	}
	i.setStatus("Completed")
	i.logf("Install finished successfully. Runtime results are in blockthespot-status.txt after Spotify starts.")
	i.setProgress(100)
	return nil
}

func (i *installer) runUninstall() error {
	spotifyDir := defaultSpotifyDir()
	if spotifyDir == "" {
		return errors.New("unable to determine Spotify directory")
	}

	spotifyExe := filepath.Join(spotifyDir, "Spotify.exe")
	i.setStatus("Stopping Spotify")
	i.setProgress(5)
	i.logf("Stopping Spotify processes.")
	stopSpotifyProcesses()

	i.setStatus("Restoring Spotify")
	originalPath, err := matchingOriginalDLL(spotifyDir)
	if err != nil {
		return fmt.Errorf("cannot restore Spotify: %w", err)
	}
	if err := restoreSpotifyFiles(spotifyDir, originalPath); err != nil {
		return err
	}
	i.logf("Restored matching original chrome_elf.dll.")
	i.logf("Preserved settings.ini and diagnostic logs for future installs.")
	i.setProgress(80)

	if i.options.LaunchSpotifyOnDone {
		i.setStatus("Launching Spotify")
		i.setProgress(90)
		if fileExists(spotifyExe) {
			i.logf("Starting Spotify.")
			if err := startSpotify(spotifyExe, spotifyDir); err != nil {
				return fmt.Errorf("failed to launch Spotify: %w", err)
			}
		} else {
			i.logf("Spotify.exe not found; skipping launch.")
		}
	}

	i.setStatus("Completed")
	i.setProgress(100)
	i.logf("Uninstall/restore finished successfully.")
	return nil
}

func (i *installer) installSpotify(spotifyExe string, selectedVersion spotifyInstallChoice) error {
	tempDir, err := os.MkdirTemp("", "blockthespot-spotify-setup-")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	setupPath := filepath.Join(tempDir, "SpotifyFullSetupX64.exe")
	downloadURL := selectedVersion.URL
	versionLabel := selectedVersion.FullVersion
	if downloadURL == "" {
		return errors.New("no compatible Spotify download selected")
	}

	i.logf("Downloading Spotify installer for %s.", versionLabel)
	if err := downloadFileWithProgress(downloadURL, setupPath, i.logf); err != nil {
		return fmt.Errorf("failed to download Spotify installer: %w", err)
	}

	i.setProgress(35)
	i.logf("Running Spotify installer.")
	if isRunningAsAdmin() {
		i.logf("Installer is running as administrator, launching setup via a temporary scheduled task.")
		if err := runInstallerViaScheduledTask(setupPath); err != nil {
			i.logf("Warning: scheduled task launch failed: %v", err)
			i.logf("Falling back to direct installer launch.")
			if err := launchDetached(setupPath); err != nil {
				return fmt.Errorf("failed to launch Spotify installer: %w", err)
			}
		}
	} else {
		if err := launchDetached(setupPath); err != nil {
			return fmt.Errorf("failed to launch Spotify installer: %w", err)
		}
	}

	i.logf("Waiting for Spotify install/update to finish.")
	if err := waitForSpotifyInstall(spotifyExe, selectedVersion.BaseVersion, 6*time.Minute); err != nil {
		stopSpotifyProcesses()
		return err
	}

	i.logf("Stopping Spotify after install/update.")
	stopSpotifyProcesses()
	i.setProgress(45)
	return nil
}

func startSpotify(exePath, workingDir string) error {
	cmd := exec.Command(exePath)
	cmd.Dir = workingDir
	return cmd.Start()
}

func loadAppIcon() (*walk.Icon, error) {
	if icon, err := walk.NewIconFromResourceId(1); err == nil {
		return icon, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return walk.NewIconExtractedFromFileWithSize(exePath, 0, 64)
}

func openExternalURL(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func hiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func launchDetached(filePath string) error {
	cmd := exec.Command(filePath)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func fetchLatestInstallerRelease() (githubRelease, error) {
	req, err := http.NewRequest(http.MethodGet, installerLatestReleaseAPI, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "BlockTheSpotInstaller/"+installerVersion)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return githubRelease{}, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return githubRelease{}, err
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return githubRelease{}, errors.New("latest release has no tag")
	}
	if strings.TrimSpace(rel.HTMLURL) == "" {
		rel.HTMLURL = installerReleasesURL
	}
	return rel, nil
}

func compareInstallerVersion(current, latest string) (int, error) {
	cv, err := parseInstallerVersion(current)
	if err != nil {
		return 0, err
	}
	lv, err := parseInstallerVersion(latest)
	if err != nil {
		return 0, err
	}

	maxLen := len(cv)
	if len(lv) > maxLen {
		maxLen = len(lv)
	}
	for len(cv) < maxLen {
		cv = append(cv, 0)
	}
	for len(lv) < maxLen {
		lv = append(lv, 0)
	}

	for idx := 0; idx < maxLen; idx++ {
		if cv[idx] < lv[idx] {
			return -1, nil
		}
		if cv[idx] > lv[idx] {
			return 1, nil
		}
	}
	return 0, nil
}

func parseInstallerVersion(value string) ([]int, error) {
	clean := strings.TrimSpace(value)
	clean = strings.TrimPrefix(clean, "v")
	if clean == "" {
		return nil, errors.New("empty version")
	}
	if strings.EqualFold(clean, "dev") {
		return nil, errors.New("dev build has no comparable release version")
	}

	if dash := strings.Index(clean, "-"); dash >= 0 {
		clean = clean[:dash]
	}

	parts := strings.Split(clean, ".")
	if len(parts) == 0 {
		return nil, errors.New("invalid version")
	}

	parsed := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, errors.New("invalid version component")
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, n)
	}
	return parsed, nil
}

func fetchSpotifyInstallChoices() (*patchRelease, []spotifyInstallChoice, int, error) {
	release, err := fetchPatchRelease(patchLatestReleaseAPI, downloadBytes)
	if err != nil {
		return nil, nil, -1, err
	}
	body, err := downloadBytes(spotifyVersionsURL)
	if err != nil {
		return release, nil, -1, fmt.Errorf("failed to download Spotify versions list: %w", err)
	}
	choices, selected, err := parseSpotifyInstallChoices(release.Compatibility, body)
	return release, choices, selected, err
}

func downloadFileWithProgress(url, targetPath string, logf func(format string, args ...any)) error {
	req, err := newDownloadRequest(url)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}

	tmpPath := targetPath + ".download"
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	buf := make([]byte, 256*1024)
	var written int64
	nextLogAt := int64(5 * 1024 * 1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := file.Write(buf[:n]); writeErr != nil {
				_ = file.Close()
				_ = os.Remove(tmpPath)
				return writeErr
			}
			written += int64(n)
			if logf != nil && written >= nextLogAt {
				if resp.ContentLength > 0 {
					logf("Downloaded Spotify installer: %.1f MB / %.1f MB.", bytesToMiB(written), bytesToMiB(resp.ContentLength))
				} else {
					logf("Downloaded Spotify installer: %.1f MB.", bytesToMiB(written))
				}
				nextLogAt = written + int64(5*1024*1024)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = file.Close()
			_ = os.Remove(tmpPath)
			return readErr
		}
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if logf != nil {
		logf("Spotify installer download complete: %.1f MB.", bytesToMiB(written))
	}

	_ = os.Remove(targetPath)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}

func bytesToMiB(value int64) float64 {
	return float64(value) / 1024 / 1024
}

func stopSpotifyProcesses() {
	processes := []string{"Spotify.exe", "SpotifyWebHelper.exe", "SpotifyFullSetup.exe", "SpotifyFullSetupX64.exe"}
	for _, name := range processes {
		_ = hiddenCommand("taskkill", "/IM", name, "/F").Run()
	}
}

func isSpotifyStoreInstalled() (bool, error) {
	script := "$pkg = Get-AppxPackage -Name SpotifyAB.SpotifyMusic -ErrorAction SilentlyContinue; if ($pkg) { '1' } else { '0' }"
	out, err := hiddenCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("powershell failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

func uninstallSpotifyStore() error {
	script := "$pkg = Get-AppxPackage -Name SpotifyAB.SpotifyMusic -ErrorAction SilentlyContinue; if ($pkg) { $pkg | Remove-AppxPackage -ErrorAction Stop }"
	out, err := hiddenCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("powershell failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func isRunningAsAdmin() bool {
	script := "([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)"
	out, err := hiddenCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(string(out)), "True")
}

func runInstallerViaScheduledTask(setupPath string) error {
	taskName := fmt.Sprintf("Spotify install %d", time.Now().UnixNano())
	escapedTaskName := strings.ReplaceAll(taskName, "'", "''")
	escapedSetupPath := strings.ReplaceAll(setupPath, "'", "''")

	script := fmt.Sprintf("$apppath='powershell.exe'; $taskname='%s'; $action=New-ScheduledTaskAction -Execute $apppath -Argument \"-NoLogo -NoProfile -Command & '%s'\"; $trigger=New-ScheduledTaskTrigger -Once -At (Get-Date); $settings=New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -WakeToRun; Register-ScheduledTask -Action $action -Trigger $trigger -TaskName $taskname -Settings $settings -Force | Out-Null; Start-ScheduledTask -TaskName $taskname; Start-Sleep -Seconds 2; Unregister-ScheduledTask -TaskName $taskname -Confirm:$false", escapedTaskName, escapedSetupPath)

	out, err := hiddenCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("powershell failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func getSpotifyVersion(spotifyExe string) (string, error) {
	escaped := strings.ReplaceAll(spotifyExe, "'", "''")
	script := fmt.Sprintf("$ErrorActionPreference='Stop'; $vi=(Get-Item -LiteralPath '%s').VersionInfo; '{0}.{1}.{2}.{3}' -f $vi.FileMajorPart,$vi.FileMinorPart,$vi.FileBuildPart,$vi.FilePrivatePart", escaped)
	out, err := hiddenCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("powershell failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	v := normalizeVersionString(strings.TrimSpace(string(out)))
	if v == "" {
		return "", errors.New("empty Spotify version")
	}
	return v, nil
}

func waitForSpotifyInstall(path, version string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if running, err := processRunning("Spotify.exe"); err == nil && running {
			installed, err := getSpotifyVersion(path)
			if err == nil && baseSpotifyVersion(installed) == version {
				return nil
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("Spotify setup did not start version %s before the timeout; finish setup and retry", version)
}

func processRunning(name string) (bool, error) {
	out, err := hiddenCommand("tasklist", "/FI", "IMAGENAME eq "+name, "/FO", "CSV", "/NH").CombinedOutput()
	if err != nil {
		return false, err
	}
	line := strings.TrimSpace(string(out))
	if line == "" || strings.Contains(line, "No tasks are running") {
		return false, nil
	}
	return strings.Contains(strings.ToLower(line), strings.ToLower(name)), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func defaultSpotifyDir() string {
	appData := os.Getenv("APPDATA")
	if appData != "" {
		return filepath.Join(appData, "Spotify")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "AppData", "Roaming", "Spotify")
}
