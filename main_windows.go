//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

const (
	spotifySetupURL  = "https://download.scdn.co/SpotifyFullSetupX64.exe"
	releaseChromeURL = "https://github.com/mrpond/BlockTheSpot/releases/latest/download/chrome_elf.dll"
	releaseBlockURL  = "https://github.com/mrpond/BlockTheSpot/releases/latest/download/blockthespot.dll"
	configURL        = "https://raw.githubusercontent.com/mrpond/BlockTheSpot/master/config.ini"
)

type installOptions struct {
	SpotifyDir          string
	UpdateSpotify       bool
	UninstallStore      bool
	LaunchSpotifyOnDone bool
}

type installer struct {
	options          installOptions
	minimumVersion   string
	downloadedConfig []byte
	logf             func(format string, args ...any)
	setProgress      func(value int)
	setStatus        func(status string)
}

type installerApp struct {
	mw             *walk.MainWindow
	spotifyPath    *walk.LineEdit
	updateCheck    *walk.CheckBox
	uninstallCheck *walk.CheckBox
	launchCheck    *walk.CheckBox
	progress       *walk.ProgressBar
	status         *walk.Label
	logView        *walk.TextEdit
	runButton      *walk.PushButton
}

func main() {
	app := &installerApp{}
	if err := app.run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (a *installerApp) run() error {
	spotifyDir := defaultSpotifyDir()

	if err := (MainWindow{
		AssignTo: &a.mw,
		Title:    "BlockTheSpot Installer (Windows x64)",
		MinSize:  Size{Width: 760, Height: 560},
		Layout:   VBox{},
		Children: []Widget{
			Composite{
				Layout: Grid{Columns: 2},
				Children: []Widget{
					Label{Text: "Spotify directory:"},
					LineEdit{AssignTo: &a.spotifyPath, Text: spotifyDir, ReadOnly: true},
				},
			},
			CheckBox{AssignTo: &a.updateCheck, Text: "Update or reinstall Spotify before patching", Checked: false},
			CheckBox{AssignTo: &a.uninstallCheck, Text: "Uninstall Microsoft Store Spotify if detected", Checked: true},
			CheckBox{AssignTo: &a.launchCheck, Text: "Launch Spotify after install", Checked: true},
			ProgressBar{AssignTo: &a.progress, MinValue: 0, MaxValue: 100},
			Label{AssignTo: &a.status, Text: "Idle"},
			TextEdit{AssignTo: &a.logView, ReadOnly: true, VScroll: true},
			Composite{
				Layout: HBox{Alignment: AlignHFarVCenter},
				Children: []Widget{
					PushButton{
						AssignTo: &a.runButton,
						Text:     "Install / Patch",
						OnClicked: func() {
							a.startInstall()
						},
					},
					PushButton{Text: "Exit", OnClicked: func() { a.mw.Close() }},
				},
			},
		},
	}).Create(); err != nil {
		return err
	}

	a.mw.Run()
	return nil
}

func (a *installerApp) startInstall() {
	opts := installOptions{
		SpotifyDir:          strings.TrimSpace(a.spotifyPath.Text()),
		UpdateSpotify:       a.updateCheck.Checked(),
		UninstallStore:      a.uninstallCheck.Checked(),
		LaunchSpotifyOnDone: a.launchCheck.Checked(),
	}

	a.setBusy(true)
	a.setProgressSafe(0)
	a.setStatusSafe("Starting")
	a.logfSafe("Starting installer.")

	go func() {
		ins := installer{
			options:     opts,
			logf:        a.logfSafe,
			setProgress: a.setProgressSafe,
			setStatus:   a.setStatusSafe,
		}

		err := ins.run()

		a.mw.Synchronize(func() {
			a.setBusy(false)
			if err != nil {
				a.setStatusSafe("Failed")
				walk.MsgBox(a.mw, "Installer Error", err.Error(), walk.MsgBoxIconError)
				return
			}

			a.progress.SetValue(100)
			a.status.SetText("Completed")
			walk.MsgBox(a.mw, "Completed", "Spotify patching completed.", walk.MsgBoxIconInformation)
		})
	}()
}

func (a *installerApp) setBusy(busy bool) {
	a.runButton.SetEnabled(!busy)
	a.updateCheck.SetEnabled(!busy)
	a.uninstallCheck.SetEnabled(!busy)
	a.launchCheck.SetEnabled(!busy)
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
	})
}

func (a *installerApp) setStatusSafe(status string) {
	a.mw.Synchronize(func() {
		a.status.SetText(status)
	})
}

func (i *installer) run() error {
	i.setStatus("Stopping Spotify")
	i.setProgress(5)
	i.logf("Stopping Spotify processes.")
	stopSpotifyProcesses()

	i.setStatus("Checking Store edition")
	i.setProgress(12)
	storeInstalled, err := isSpotifyStoreInstalled()
	if err != nil {
		i.logf("Warning: failed to check Microsoft Store Spotify: %v", err)
	} else if storeInstalled {
		i.logf("Microsoft Store Spotify detected.")
		if !i.options.UninstallStore {
			return errors.New("Microsoft Store Spotify is installed. Enable uninstall option and run again")
		}

		i.logf("Uninstalling Microsoft Store Spotify.")
		if err := uninstallSpotifyStore(); err != nil {
			return fmt.Errorf("failed to uninstall Microsoft Store Spotify: %w", err)
		}
		i.logf("Microsoft Store Spotify removed.")
	} else {
		i.logf("Microsoft Store Spotify not detected.")
	}

	i.setStatus("Loading config")
	i.setProgress(15)
	if err := i.loadConfig(); err != nil {
		return err
	}
	i.logf("Minimum supported Spotify version from config.ini: %s", i.minimumVersion)

	spotifyExe := filepath.Join(i.options.SpotifyDir, "Spotify.exe")
	spotifyInstalled := fileExists(spotifyExe)
	unsupportedVersion := true
	if spotifyInstalled {
		v, err := getSpotifyVersion(spotifyExe)
		if err != nil {
			i.logf("Warning: unable to read Spotify version: %v", err)
		} else {
			i.logf("Detected Spotify version: %s", v)
			unsupportedVersion = compareVersion(v, i.minimumVersion) < 0
		}
	}

	needsInstall := !spotifyInstalled || i.options.UpdateSpotify || unsupportedVersion
	if needsInstall {
		i.setStatus("Installing Spotify")
		i.setProgress(20)
		if err := os.MkdirAll(i.options.SpotifyDir, 0o755); err != nil {
			return fmt.Errorf("failed to prepare Spotify directory: %w", err)
		}

		if err := i.installSpotify(spotifyExe); err != nil {
			return err
		}
	} else {
		i.logf("Spotify update not required.")
		i.setProgress(45)
	}

	i.setStatus("Applying BlockTheSpot files")
	if err := i.patchSpotify(); err != nil {
		return err
	}

	if i.options.LaunchSpotifyOnDone {
		i.setStatus("Launching Spotify")
		i.logf("Starting Spotify.")
		if err := startSpotify(spotifyExe, i.options.SpotifyDir); err != nil {
			return fmt.Errorf("failed to launch Spotify: %w", err)
		}
	}

	i.setStatus("Completed")
	i.logf("Install finished successfully.")
	i.setProgress(100)
	return nil
}

func (i *installer) installSpotify(spotifyExe string) error {
	tempDir, err := os.MkdirTemp("", "blockthespot-spotify-setup-")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	setupPath := filepath.Join(tempDir, "SpotifyFullSetupX64.exe")
	i.logf("Downloading Spotify installer: %s", spotifySetupURL)
	if err := downloadFile(spotifySetupURL, setupPath); err != nil {
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
	if err := waitForFile(spotifyExe, 6*time.Minute); err != nil {
		return err
	}
	_ = waitForProcess("Spotify.exe", 90*time.Second)

	i.logf("Stopping Spotify after install/update.")
	stopSpotifyProcesses()
	i.setProgress(45)
	return nil
}

func (i *installer) patchSpotify() error {
	spotifyDir := i.options.SpotifyDir
	requiredPath := filepath.Join(spotifyDir, "chrome_elf_required.dll")
	chromePath := filepath.Join(spotifyDir, "chrome_elf.dll")
	blockPath := filepath.Join(spotifyDir, "blockthespot.dll")
	configPath := filepath.Join(spotifyDir, "config.ini")

	if err := removeIfExists(requiredPath); err != nil {
		return fmt.Errorf("failed to delete chrome_elf_required.dll: %w", err)
	}
	if err := removeIfExists(blockPath); err != nil {
		return fmt.Errorf("failed to delete blockthespot.dll: %w", err)
	}

	if fileExists(chromePath) {
		if err := os.Rename(chromePath, requiredPath); err != nil {
			return fmt.Errorf("failed to rename chrome_elf.dll to chrome_elf_required.dll: %w", err)
		}
		i.logf("Renamed chrome_elf.dll to chrome_elf_required.dll.")
	} else {
		i.logf("Warning: chrome_elf.dll was not found before patching.")
	}

	i.setProgress(60)
	i.logf("Downloading latest chrome_elf.dll.")
	if err := downloadFile(releaseChromeURL, chromePath); err != nil {
		return fmt.Errorf("failed to download chrome_elf.dll: %w", err)
	}

	i.setProgress(75)
	i.logf("Downloading latest blockthespot.dll.")
	if err := downloadFile(releaseBlockURL, blockPath); err != nil {
		return fmt.Errorf("failed to download blockthespot.dll: %w", err)
	}

	i.setProgress(90)
	i.logf("Writing latest config.ini.")
	if err := writeFileAtomically(configPath, i.downloadedConfig); err != nil {
		return fmt.Errorf("failed to write config.ini: %w", err)
	}

	i.setProgress(98)
	return nil
}

func startSpotify(exePath, workingDir string) error {
	cmd := exec.Command(exePath)
	cmd.Dir = workingDir
	return cmd.Start()
}

func launchDetached(filePath string) error {
	cmd := exec.Command(filePath)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func (i *installer) loadConfig() error {
	body, err := downloadBytes(configURL)
	if err != nil {
		return fmt.Errorf("failed to download config.ini: %w", err)
	}

	version, err := extractMinimumVersionFromConfig(body)
	if err != nil {
		return fmt.Errorf("failed to parse minimum Spotify version from config.ini: %w", err)
	}

	i.minimumVersion = version
	i.downloadedConfig = body
	return nil
}

func downloadFile(url, targetPath string) error {
	body, err := downloadBytes(url)
	if err != nil {
		return err
	}
	return writeFileAtomically(targetPath, body)
}

func downloadBytes(url string) ([]byte, error) {
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func writeFileAtomically(targetPath string, body []byte) error {
	tmpPath := targetPath + ".download"
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	_, copyErr := file.Write(body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}

	_ = os.Remove(targetPath)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}

func extractMinimumVersionFromConfig(body []byte) (string, error) {
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, ";") {
			continue
		}

		candidate := strings.TrimSpace(strings.TrimPrefix(line, ";"))
		if candidate == "" || !looksLikeSpotifyVersion(candidate) {
			continue
		}
		return candidate, nil
	}

	return "", errors.New("no Spotify version marker found")
}

func looksLikeSpotifyVersion(value string) bool {
	if !strings.HasPrefix(value, "1.") {
		return false
	}

	parts := strings.Split(value, ".")
	if len(parts) < 3 {
		return false
	}

	numericParts := 0
	for _, part := range parts {
		if leadingDigits(part) == "" {
			break
		}
		numericParts++
		if numericParts == 4 {
			break
		}
	}

	return numericParts >= 3
}

func stopSpotifyProcesses() {
	processes := []string{"Spotify.exe", "SpotifyWebHelper.exe", "SpotifyFullSetup.exe", "SpotifyFullSetupX64.exe"}
	for _, name := range processes {
		_ = exec.Command("taskkill", "/IM", name, "/F").Run()
	}
}

func isSpotifyStoreInstalled() (bool, error) {
	script := "$pkg = Get-AppxPackage -Name SpotifyAB.SpotifyMusic -ErrorAction SilentlyContinue; if ($pkg) { '1' } else { '0' }"
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("powershell failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

func uninstallSpotifyStore() error {
	script := "$pkg = Get-AppxPackage -Name SpotifyAB.SpotifyMusic -ErrorAction SilentlyContinue; if ($pkg) { $pkg | Remove-AppxPackage -ErrorAction Stop }"
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("powershell failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func isRunningAsAdmin() bool {
	script := "([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)"
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
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

	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("powershell failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func getSpotifyVersion(spotifyExe string) (string, error) {
	escaped := strings.ReplaceAll(spotifyExe, "'", "''")
	script := fmt.Sprintf("(Get-Item '%s').VersionInfo.ProductVersionRaw", escaped)
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("powershell failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", errors.New("empty Spotify version")
	}
	return v, nil
}

func compareVersion(a, b string) int {
	av := normalizeVersion(a)
	bv := normalizeVersion(b)
	for idx := 0; idx < len(av) && idx < len(bv); idx++ {
		if av[idx] < bv[idx] {
			return -1
		}
		if av[idx] > bv[idx] {
			return 1
		}
	}
	return 0
}

func normalizeVersion(value string) []int {
	parts := strings.Split(value, ".")
	parsed := make([]int, 0, 4)
	for _, part := range parts {
		digits := leadingDigits(part)
		if digits == "" {
			break
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			break
		}
		parsed = append(parsed, n)
		if len(parsed) == 4 {
			break
		}
	}
	for len(parsed) < 4 {
		parsed = append(parsed, 0)
	}
	return parsed
}

func leadingDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fileExists(path) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", path)
}

func waitForProcess(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := processRunning(name)
		if err == nil && running {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for process %s", name)
}

func processRunning(name string) (bool, error) {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+name, "/FO", "CSV", "/NH").CombinedOutput()
	if err != nil {
		return false, err
	}
	line := strings.TrimSpace(string(out))
	if line == "" || strings.Contains(line, "No tasks are running") {
		return false, nil
	}
	return strings.Contains(strings.ToLower(line), strings.ToLower(name)), nil
}

func removeIfExists(path string) error {
	if !fileExists(path) {
		return nil
	}
	return os.Remove(path)
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
