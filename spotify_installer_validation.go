package main

import (
	"bytes"
	"crypto/x509/pkix"
	"debug/pe"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const spotifyInstallerMaxBytes int64 = 1 << 30

// validateSpotifyInstallerFile checks the download without loading or executing it.
// Authenticode and publisher/version verification must also succeed on Windows.
func validateSpotifyInstallerFile(path string, choice spotifyInstallChoice) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open Spotify installer for validation: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("read Spotify installer size: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("Spotify installer is not a regular file")
	}
	size := info.Size()
	if err := validateSpotifyInstallerSize(size, choice.Size); err != nil {
		return err
	}

	// Read fixed-size headers only. Do not use the general-purpose PE parser:
	// its symbol/string-table allocations are unsafe for an untrusted download.
	var dos [64]byte
	if _, err := file.ReadAt(dos[:], 0); err != nil || string(dos[:2]) != "MZ" {
		return errors.New("Spotify installer is not a Windows executable (missing MZ header)")
	}
	peOffset := int64(binary.LittleEndian.Uint32(dos[60:64]))
	var header [24]byte
	if peOffset < int64(len(dos)) || peOffset > size-int64(len(header)) {
		return errors.New("Spotify installer has an invalid PE header offset")
	}
	if _, err := file.ReadAt(header[:], peOffset); err != nil || string(header[:4]) != "PE\x00\x00" {
		return errors.New("Spotify installer has an invalid PE signature")
	}
	sections := int64(binary.LittleEndian.Uint16(header[6:8]))
	optionalBytes := int64(binary.LittleEndian.Uint16(header[20:22]))
	if sections == 0 || sections > 96 || optionalBytes != 240 {
		return errors.New("Spotify installer has invalid x64 PE headers")
	}
	headersEnd := peOffset + int64(len(header)) + optionalBytes + sections*40
	if headersEnd > size {
		return errors.New("Spotify installer has truncated PE headers")
	}
	symbolOffset := int64(binary.LittleEndian.Uint32(header[12:16]))
	symbols := int64(binary.LittleEndian.Uint32(header[16:20]))
	if (symbols > 0 && symbolOffset < headersEnd) || (symbolOffset != 0 && symbolOffset+symbols*18+4 > size) {
		return errors.New("Spotify installer has a truncated PE symbol table")
	}

	characteristics := binary.LittleEndian.Uint16(header[22:24])
	if characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 || characteristics&pe.IMAGE_FILE_DLL != 0 {
		return errors.New("Spotify installer is not an executable image (DLLs are not allowed)")
	}
	// Catalog x64 installers, including the earliest x64 release (1.2.13.656),
	// use AMD64 bootstraps. Do not infer this from the host process architecture.
	var optional pe.OptionalHeader64
	var optionalData [240]byte
	if _, err := file.ReadAt(optionalData[:], peOffset+int64(len(header))); err != nil {
		return fmt.Errorf("read Spotify installer PE optional header: %w", err)
	}
	if err := binary.Read(bytes.NewReader(optionalData[:]), binary.LittleEndian, &optional); err != nil {
		return fmt.Errorf("read Spotify installer PE optional header: %w", err)
	}
	if binary.LittleEndian.Uint16(header[4:6]) != pe.IMAGE_FILE_MACHINE_AMD64 || optional.Magic != 0x20b {
		return errors.New("Spotify installer is not a Windows x64 executable")
	}
	if optional.NumberOfRvaAndSizes > uint32(len(optional.DataDirectory)) {
		return errors.New("Spotify installer has invalid PE data directories")
	}
	if int64(optional.SizeOfHeaders) < headersEnd || int64(optional.SizeOfHeaders) > size {
		return errors.New("Spotify installer has an invalid PE header size")
	}
	if optional.SizeOfImage < optional.SizeOfHeaders || optional.AddressOfEntryPoint == 0 || optional.AddressOfEntryPoint >= optional.SizeOfImage {
		return errors.New("Spotify installer has an invalid PE executable entry point")
	}
	for index := int64(0); index < sections; index++ {
		var sectionData [40]byte
		sectionOffset := peOffset + int64(len(header)) + optionalBytes + index*int64(len(sectionData))
		if _, err := file.ReadAt(sectionData[:], sectionOffset); err != nil {
			return fmt.Errorf("read Spotify installer PE section: %w", err)
		}
		var section pe.SectionHeader32
		if err := binary.Read(bytes.NewReader(sectionData[:]), binary.LittleEndian, &section); err != nil {
			return fmt.Errorf("read Spotify installer PE section: %w", err)
		}
		if section.SizeOfRawData == 0 {
			continue // Uninitialized data has no bytes on disk.
		}
		start := int64(section.PointerToRawData)
		end := start + int64(section.SizeOfRawData)
		if start < int64(optional.SizeOfHeaders) || end > size {
			name := strings.TrimRight(string(section.Name[:]), "\x00")
			return fmt.Errorf("Spotify installer has a truncated or invalid PE section %q", name)
		}
	}
	// Unlike other data directories, the certificate table uses a file offset.
	if optional.NumberOfRvaAndSizes > pe.IMAGE_DIRECTORY_ENTRY_SECURITY {
		certificate := optional.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_SECURITY]
		start, length := int64(certificate.VirtualAddress), int64(certificate.Size)
		if (start == 0) != (length == 0) || (length != 0 && (start < int64(optional.SizeOfHeaders) || start+length > size)) {
			return errors.New("Spotify installer has a truncated or invalid PE certificate table")
		}
	}
	return nil
}

func validateSpotifyInstallerSize(size, expected int64) error {
	if size < 512 || size > spotifyInstallerMaxBytes {
		return fmt.Errorf("Spotify installer has an unsafe size: %d bytes", size)
	}
	if expected > 0 && size != expected {
		return fmt.Errorf("Spotify installer size mismatch: got %d bytes, expected %d", size, expected)
	}
	return nil
}

// These fields come only from the fixed Windows verification script. A successful
// parse is not sufficient: missing or unexpected trust metadata fails closed.
type spotifyInstallerMetadata struct {
	Status        string `json:"status"`
	SignatureType string `json:"signatureType"`
	SignerSubject []byte `json:"signerSubject"`
	FileVersion   string `json:"fileVersion"`
}

func validateSpotifyInstallerMetadata(metadata spotifyInstallerMetadata, choice spotifyInstallChoice) error {
	if metadata.Status != "Valid" {
		return fmt.Errorf("Spotify installer Authenticode signature is not trusted (status %q)", metadata.Status)
	}
	if metadata.SignatureType != "Authenticode" {
		return errors.New("Spotify installer does not have a trusted embedded Authenticode signature")
	}
	// Parse the certificate's DER-encoded subject, never search its display text.
	// Exactly one common name is required so duplicate or lookalike CNs fail closed.
	var subject pkix.RDNSequence
	rest, err := asn1.Unmarshal(metadata.SignerSubject, &subject)
	if err != nil || len(rest) != 0 {
		return errors.New("Spotify installer signer certificate subject is unreadable")
	}
	commonNames := 0
	for _, rdn := range subject {
		for _, attribute := range rdn {
			if attribute.Type.Equal(asn1.ObjectIdentifier{2, 5, 4, 3}) {
				commonNames++
				if name, ok := attribute.Value.(string); !ok || name != "Spotify AB" {
					return errors.New("Spotify installer is not signed by Spotify AB")
				}
			}
		}
	}
	if commonNames != 1 {
		return errors.New("Spotify installer signer must have exactly one Spotify AB common name")
	}
	expected, err := spotifyInstallerVersionParts(choice.BaseVersion)
	if err != nil {
		return errors.New("selected Spotify installer version is invalid")
	}
	actual, err := spotifyInstallerVersionParts(metadata.FileVersion)
	if err != nil {
		return errors.New("Spotify installer signed file version is missing or unreadable")
	}
	if actual != expected {
		return fmt.Errorf("Spotify installer version mismatch: got %s, expected %s", metadata.FileVersion, choice.BaseVersion)
	}
	return nil
}

func spotifyInstallerVersionParts(version string) ([4]uint16, error) {
	var parsed [4]uint16
	parts := strings.Split(version, ".")
	if len(parts) != len(parsed) {
		return parsed, errors.New("expected four numeric version components")
	}
	for i, part := range parts {
		if !isAllDigits(part) {
			return parsed, errors.New("invalid version component")
		}
		number, err := strconv.ParseUint(part, 10, 16)
		if err != nil {
			return parsed, err
		}
		parsed[i] = uint16(number)
	}
	if parsed[0] != 1 {
		return parsed, errors.New("invalid Spotify version")
	}
	return parsed, nil
}
