package downloader

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"testing"
)

func TestVerifyFile(t *testing.T) {
	// Create a temp file
	tmpFile, err := os.CreateTemp("", "testverify")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	content := "hello world"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Calc hashes
	h256 := sha256.New()
	io.WriteString(h256, content)
	sum256 := hex.EncodeToString(h256.Sum(nil))

	hMd5 := md5.New()
	io.WriteString(hMd5, content)
	sumMd5 := hex.EncodeToString(hMd5.Sum(nil))

	tests := []struct {
		name     string
		checksum string
		wantErr  bool
	}{
		{"Empty checksum", "", false},
		{"Valid SHA256 implicit", sum256, false},
		{"Valid SHA256 explicit", "sha256:" + sum256, false},
		{"Valid MD5 explicit", "md5:" + sumMd5, false},
		{"Valid MD5 implicit", sumMd5, false}, // 32 chars
		{"Invalid Checksum", "badchecksum", true},
		{"Mismatch Checksum", "sha256:0000000000000000000000000000000000000000000000000000000000000000", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyFile(tmpFile.Name(), tt.checksum)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyFile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
