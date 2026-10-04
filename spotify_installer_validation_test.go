package main

import (
	"bytes"
	"crypto/x509/pkix"
	"debug/pe"
	"encoding/asn1"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func spotifyInstallerTestPE(t *testing.T) []byte {
	t.Helper()
	data := make([]byte, 1024)
	copy(data, "MZ")
	binary.LittleEndian.PutUint32(data[60:64], 128)
	copy(data[128:], "PE\x00\x00")
	write := func(offset int, value any) {
		t.Helper()
		var b bytes.Buffer
		if err := binary.Write(&b, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
		copy(data[offset:], b.Bytes())
	}
	write(132, pe.FileHeader{
		Machine: pe.IMAGE_FILE_MACHINE_AMD64, NumberOfSections: 1,
		SizeOfOptionalHeader: 240, Characteristics: pe.IMAGE_FILE_EXECUTABLE_IMAGE,
	})
	write(152, pe.OptionalHeader64{
		Magic: 0x20b, AddressOfEntryPoint: 4096, ImageBase: 0x140000000,
		SectionAlignment: 4096, FileAlignment: 512, SizeOfImage: 8192,
		SizeOfHeaders: 512, Subsystem: 2, NumberOfRvaAndSizes: 16,
	})
	write(392, pe.SectionHeader32{
		Name: [8]byte{'.', 't', 'e', 'x', 't'}, VirtualSize: 512, VirtualAddress: 4096,
		SizeOfRawData: 512, PointerToRawData: 512, Characteristics: 0x60000020,
	})
	return data
}

func writeSpotifyInstallerTestFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Spotify 'quoted' [installer].exe")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateSpotifyInstallerSize(t *testing.T) {
	tests := []struct {
		size, expected int64
		valid          bool
	}{
		{0, 0, false}, {-1, 0, false}, {511, 0, false},
		{512, 0, true}, {512, 512, true}, {512, 513, false},
		{1024, -1, true}, {spotifyInstallerMaxBytes, 0, true},
		{spotifyInstallerMaxBytes + 1, 0, false},
		{spotifyInstallerMaxBytes + 1, spotifyInstallerMaxBytes + 1, false},
	}
	for _, tc := range tests {
		err := validateSpotifyInstallerSize(tc.size, tc.expected)
		if (err == nil) != tc.valid {
			t.Errorf("size %d expected %d: error = %v, want valid=%v", tc.size, tc.expected, err, tc.valid)
		}
	}
}

func TestValidateSpotifyInstallerFile(t *testing.T) {
	tests := []struct {
		name string
		edit func([]byte) []byte
		size int64
		want string
	}{
		{name: "valid exact size", size: 1024},
		{name: "valid unknown size"},
		{name: "unknown negative size", size: -1},
		{name: "catalog size mismatch", size: 1025, want: "size mismatch"},
		{name: "empty", edit: func(b []byte) []byte { return nil }, want: "unsafe size"},
		{name: "too small", edit: func(b []byte) []byte { return b[:511] }, want: "unsafe size"},
		{name: "HTML error", edit: func(b []byte) []byte { copy(b, "<html>Error"); return b }, want: "MZ"},
		{name: "ZIP archive", edit: func(b []byte) []byte { copy(b, "PK\x03\x04"); return b }, want: "MZ"},
		{name: "PE offset in DOS", edit: func(b []byte) []byte { binary.LittleEndian.PutUint32(b[60:], 1); return b }, want: "header offset"},
		{name: "PE offset past EOF", edit: func(b []byte) []byte { binary.LittleEndian.PutUint32(b[60:], ^uint32(0)); return b }, want: "header offset"},
		{name: "bad PE signature", edit: func(b []byte) []byte { b[128] = 'X'; return b }, want: "PE signature"},
		{name: "no sections", edit: func(b []byte) []byte { binary.LittleEndian.PutUint16(b[134:], 0); return b }, want: "PE headers"},
		{name: "too many sections", edit: func(b []byte) []byte { binary.LittleEndian.PutUint16(b[134:], 97); return b }, want: "PE headers"},
		{name: "truncated section headers", edit: func(b []byte) []byte { binary.LittleEndian.PutUint16(b[134:], 20); return b }, want: "truncated PE headers"},
		{name: "not x64 optional header", edit: func(b []byte) []byte { binary.LittleEndian.PutUint16(b[148:], 224); return b }, want: "x64 PE headers"},
		{name: "no executable flag", edit: func(b []byte) []byte { binary.LittleEndian.PutUint16(b[150:], 0); return b }, want: "not an executable"},
		{name: "DLL", edit: func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[150:], pe.IMAGE_FILE_EXECUTABLE_IMAGE|pe.IMAGE_FILE_DLL)
			return b
		}, want: "DLL"},
		{name: "x86 machine", edit: func(b []byte) []byte { binary.LittleEndian.PutUint16(b[132:], pe.IMAGE_FILE_MACHINE_I386); return b }, want: "not a Windows x64"},
		{name: "ARM64 machine", edit: func(b []byte) []byte { binary.LittleEndian.PutUint16(b[132:], pe.IMAGE_FILE_MACHINE_ARM64); return b }, want: "not a Windows x64"},
		{name: "PE32 optional magic", edit: func(b []byte) []byte { binary.LittleEndian.PutUint16(b[152:], 0x10b); return b }, want: "not a Windows x64"},
		{name: "excess data directories", edit: func(b []byte) []byte { binary.LittleEndian.PutUint32(b[152+108:], 17); return b }, want: "data directories"},
		{name: "headers smaller than section table", edit: func(b []byte) []byte { binary.LittleEndian.PutUint32(b[152+60:], 128); return b }, want: "header size"},
		{name: "headers past EOF", edit: func(b []byte) []byte { binary.LittleEndian.PutUint32(b[152+60:], 2048); return b }, want: "header size"},
		{name: "zero entry point", edit: func(b []byte) []byte { binary.LittleEndian.PutUint32(b[152+16:], 0); return b }, want: "entry point"},
		{name: "entry point past image", edit: func(b []byte) []byte { binary.LittleEndian.PutUint32(b[152+16:], 8192); return b }, want: "entry point"},
		{name: "section inside headers", edit: func(b []byte) []byte { binary.LittleEndian.PutUint32(b[392+20:], 256); return b }, want: "PE section"},
		{name: "truncated section", edit: func(b []byte) []byte { return b[:1023] }, want: "PE section"},
		{name: "section offset overflow", edit: func(b []byte) []byte { binary.LittleEndian.PutUint32(b[392+20:], ^uint32(0)); return b }, want: "PE section"},
		{name: "uninitialized section", edit: func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[392+16:], 0)
			binary.LittleEndian.PutUint32(b[392+20:], 0)
			return b
		}},
		{name: "truncated symbol table", edit: func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[140:], 1024)
			binary.LittleEndian.PutUint32(b[144:], 1)
			return b
		}, want: "symbol table"},
		{name: "symbol count without table", edit: func(b []byte) []byte { binary.LittleEndian.PutUint32(b[144:], 1); return b }, want: "symbol table"},
		{name: "symbol count overflow", edit: func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[140:], 512)
			binary.LittleEndian.PutUint32(b[144:], ^uint32(0))
			return b
		}, want: "symbol table"},
		{name: "truncated certificate table", edit: func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[152+112+32:], 1024)
			binary.LittleEndian.PutUint32(b[152+112+36:], 8)
			return b
		}, want: "certificate table"},
		{name: "certificate size without offset", edit: func(b []byte) []byte { binary.LittleEndian.PutUint32(b[152+112+36:], 8); return b }, want: "certificate table"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := spotifyInstallerTestPE(t)
			if tc.edit != nil {
				data = tc.edit(data)
			}
			path := writeSpotifyInstallerTestFile(t, data)
			err := validateSpotifyInstallerFile(path, spotifyInstallChoice{Size: tc.size})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidateSpotifyInstallerFileMissingAndDirectory(t *testing.T) {
	for _, path := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing.exe")} {
		if err := validateSpotifyInstallerFile(path, spotifyInstallChoice{}); err == nil {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
}

func spotifyInstallerTestSubject(t *testing.T, names ...string) []byte {
	t.Helper()
	subject := pkix.RDNSequence{}
	for _, name := range names {
		subject = append(subject, []pkix.AttributeTypeAndValue{{Type: asn1.ObjectIdentifier{2, 5, 4, 3}, Value: name}})
	}
	data, err := asn1.Marshal(subject)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestValidateSpotifyInstallerMetadata(t *testing.T) {
	good := spotifyInstallerMetadata{Status: "Valid", SignatureType: "Authenticode", SignerSubject: spotifyInstallerTestSubject(t, "Spotify AB"), FileVersion: "1.3.3.264"}
	tests := []struct {
		name    string
		edit    func(*spotifyInstallerMetadata)
		version string
		want    string
	}{
		{name: "trusted exact match"},
		{name: "numeric match", version: "1.03.3.0264"},
		{name: "unsigned", edit: func(m *spotifyInstallerMetadata) { m.Status = "NotSigned" }, want: "not trusted"},
		{name: "invalid signature", edit: func(m *spotifyInstallerMetadata) { m.Status = "HashMismatch" }, want: "not trusted"},
		{name: "untrusted", edit: func(m *spotifyInstallerMetadata) { m.Status = "NotTrusted" }, want: "not trusted"},
		{name: "missing status", edit: func(m *spotifyInstallerMetadata) { m.Status = "" }, want: "not trusted"},
		{name: "catalog signature", edit: func(m *spotifyInstallerMetadata) { m.SignatureType = "Catalog" }, want: "embedded Authenticode"},
		{name: "missing signature type", edit: func(m *spotifyInstallerMetadata) { m.SignatureType = "" }, want: "embedded Authenticode"},
		{name: "missing certificate", edit: func(m *spotifyInstallerMetadata) { m.SignerSubject = nil }, want: "subject is unreadable"},
		{name: "invalid DER", edit: func(m *spotifyInstallerMetadata) { m.SignerSubject = []byte("CN=Spotify AB") }, want: "subject is unreadable"},
		{name: "DER trailing data", edit: func(m *spotifyInstallerMetadata) { m.SignerSubject = append(append([]byte{}, m.SignerSubject...), 0) }, want: "subject is unreadable"},
		{name: "wrong signer", edit: func(m *spotifyInstallerMetadata) { m.SignerSubject = spotifyInstallerTestSubject(t, "Other Publisher") }, want: "not signed by Spotify AB"},
		{name: "signer substring", edit: func(m *spotifyInstallerMetadata) { m.SignerSubject = spotifyInstallerTestSubject(t, "Not Spotify AB") }, want: "not signed by Spotify AB"},
		{name: "signer suffix", edit: func(m *spotifyInstallerMetadata) { m.SignerSubject = spotifyInstallerTestSubject(t, "Spotify AB Fake") }, want: "not signed by Spotify AB"},
		{name: "signer wrong case", edit: func(m *spotifyInstallerMetadata) { m.SignerSubject = spotifyInstallerTestSubject(t, "spotify ab") }, want: "not signed by Spotify AB"},
		{name: "signer escaped display injection", edit: func(m *spotifyInstallerMetadata) {
			m.SignerSubject = spotifyInstallerTestSubject(t, "Other, CN=Spotify AB")
		}, want: "not signed by Spotify AB"},
		{name: "duplicate common name", edit: func(m *spotifyInstallerMetadata) {
			m.SignerSubject = spotifyInstallerTestSubject(t, "Spotify AB", "Spotify AB")
		}, want: "exactly one"},
		{name: "missing common name", edit: func(m *spotifyInstallerMetadata) { m.SignerSubject = spotifyInstallerTestSubject(t) }, want: "exactly one"},
		{name: "version mismatch", edit: func(m *spotifyInstallerMetadata) { m.FileVersion = "1.3.3.265" }, want: "version mismatch"},
		{name: "missing version", edit: func(m *spotifyInstallerMetadata) { m.FileVersion = "" }, want: "missing or unreadable"},
		{name: "version prefix only", edit: func(m *spotifyInstallerMetadata) { m.FileVersion = "1.3.3.264.evil" }, want: "missing or unreadable"},
		{name: "version nonnumeric", edit: func(m *spotifyInstallerMetadata) { m.FileVersion = "1.3.3.264x" }, want: "missing or unreadable"},
		{name: "version overflow", edit: func(m *spotifyInstallerMetadata) { m.FileVersion = "1.3.3.65536" }, want: "missing or unreadable"},
		{name: "invalid selected version", version: "1.3.3", want: "selected Spotify installer version is invalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			metadata := good
			if tc.edit != nil {
				tc.edit(&metadata)
			}
			version := tc.version
			if version == "" {
				version = "1.3.3.264"
			}
			err := validateSpotifyInstallerMetadata(metadata, spotifyInstallChoice{BaseVersion: version})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSpotifyInstallerVersionParts(t *testing.T) {
	for _, version := range []string{"", "1.2.3", "1.2.3.4.5", "1.2.3.-4", "1.2.3.+4", "1.2.3. 4", "1.2.3.4 ", "1.2.3.65536", "2.2.3.4", "1.2.3.٤"} {
		if _, err := spotifyInstallerVersionParts(version); err == nil {
			t.Errorf("accepted version %q", version)
		}
	}
}

// Opt-in, read-only validation of a predownloaded public installer. This never
// executes the file and normal offline test runs do not fetch any network data.
func TestSpotifyInstallerDownloadedFile(t *testing.T) {
	path := os.Getenv("BTS_SPOTIFY_VALIDATION_FILE")
	if path == "" {
		t.Skip("set BTS_SPOTIFY_VALIDATION_FILE to a downloaded installer")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSpotifyInstallerFile(path, spotifyInstallChoice{Size: info.Size()}); err != nil {
		t.Fatal(err)
	}
	t.Logf("validated %d-byte x64 PE installer without executing it", info.Size())
}
