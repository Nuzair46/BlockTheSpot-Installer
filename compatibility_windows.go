//go:build windows

package main

import (
	"bytes"
	"debug/pe"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

func validatePatchDLLs(files map[string][]byte) error {
	for _, name := range []string{"chrome_elf.dll", "blockthespot.dll"} {
		file, err := pe.NewFile(bytes.NewReader(files[name]))
		if err != nil {
			return fmt.Errorf("invalid %s: %w", name, err)
		}
		err = checkWindowsX64(file, name)
		file.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func requireWindowsX64(path string) error {
	file, err := pe.Open(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()
	return checkWindowsX64(file, path)
}

func checkWindowsX64(file *pe.File, name string) error {
	_, is64 := file.OptionalHeader.(*pe.OptionalHeader64)
	if !is64 || file.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return fmt.Errorf("%s must be a Windows x64 file", name)
	}
	return nil
}

func matchingOriginalDLL(dir string) (string, error) {
	cef := filepath.Join(dir, "libcef.dll")
	if err := requireWindowsX64(cef); err != nil {
		return "", err
	}
	escaped := strings.ReplaceAll(dir, "'", "''")
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'; $dir='%s'; $result=@{};
foreach ($name in @('libcef.dll','chrome_elf.dll','chrome_elf_required.dll')) {
  $path=Join-Path $dir $name
  if (Test-Path -LiteralPath $path -PathType Leaf) {
    $vi=(Get-Item -LiteralPath $path).VersionInfo
    if ($name -eq 'libcef.dll') { $result[$name]=$vi.FileVersion }
    else { $result[$name]='{0}.{1}.{2}.{3}' -f $vi.FileMajorPart,$vi.FileMinorPart,$vi.FileBuildPart,$vi.FilePrivatePart }
  }
}; ConvertTo-Json -Compress -InputObject $result`, escaped)
	out, err := hiddenCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("read original DLL versions: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	var versions map[string]string
	if err := json.Unmarshal(bytes.TrimSpace(out), &versions); err != nil {
		return "", err
	}
	name, err := originalDLLName(versions)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := requireWindowsX64(path); err != nil {
		return "", err
	}
	return path, nil
}
