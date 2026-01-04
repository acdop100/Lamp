package core

import (
	"testing"
)

func TestValidateRegexPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{
			name:    "valid simple pattern",
			pattern: `^test-\d+\.zip$`,
			wantErr: false,
		},
		{
			name:    "valid pattern with groups",
			pattern: `^(.*?)-(v?\d+\.\d+)\.exe$`,
			wantErr: false,
		},
		{
			name:    "nested quantifiers - ReDoS",
			pattern: `(a+)+`,
			wantErr: true,
		},
		{
			name:    "excessively long pattern",
			pattern: string(make([]byte, 600)),
			wantErr: true,
		},
		{
			name:    "invalid regex",
			pattern: `[unclosed`,
			wantErr: true,
		},
		{
			name:    "empty pattern",
			pattern: "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRegexPattern(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRegexPattern() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
		wantErr  bool
	}{
		{
			name:     "valid filename",
			filename: "test-file.zip",
			want:     "test-file.zip",
			wantErr:  false,
		},
		{
			name:     "path traversal attempt",
			filename: "../../../etc/passwd",
			want:     "",
			wantErr:  true,
		},
		{
			name:     "absolute path unix",
			filename: "/etc/passwd",
			want:     "",
			wantErr:  true,
		},
		{
			name:     "absolute path windows",
			filename: "C:\\Windows\\System32\\cmd.exe",
			want:     "",
			wantErr:  true,
		},
		{
			name:     "filename with slashes",
			filename: "some/path/file.txt",
			want:     "some_path_file.txt",
			wantErr:  false,
		},
		{
			name:     "empty filename",
			filename: "",
			want:     "",
			wantErr:  true,
		},
		{
			name:     "dot files",
			filename: "..",
			want:     "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SanitizeFilename(tt.filename)
			if (err != nil) != tt.wantErr {
				t.Errorf("SanitizeFilename() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SanitizeFilename() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSafeCompileRegex(t *testing.T) {
	// Test that safe compilation works for valid patterns
	re, err := SafeCompileRegex(`^\d+$`)
	if err != nil {
		t.Errorf("SafeCompileRegex() failed for valid pattern: %v", err)
	}
	if re == nil {
		t.Error("SafeCompileRegex() returned nil regex for valid pattern")
	}

	// Test that it rejects dangerous patterns
	_, err = SafeCompileRegex(`(a+)+`)
	if err == nil {
		t.Error("SafeCompileRegex() should reject ReDoS pattern")
	}
}

func TestValidateDownloadURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid HTTPS URL",
			url:     "https://example.com/file.zip",
			wantErr: false,
		},
		{
			name:    "HTTP URL should be rejected",
			url:     "http://example.com/file.zip",
			wantErr: true,
		},
		{
			name:    "localhost HTTP allowed",
			url:     "http://localhost:8080/file.zip",
			wantErr: false,
		},
		{
			name:    "127.0.0.1 HTTP allowed",
			url:     "http://127.0.0.1/file.zip",
			wantErr: false,
		},
		{
			name:    "IPv6 localhost HTTP allowed",
			url:     "http://[::1]/file.zip",
			wantErr: false,
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
		},
		{
			name:    "invalid URL",
			url:     "not a url",
			wantErr: true,
		},
		{
			name:    "FTP scheme rejected",
			url:     "ftp://example.com/file.zip",
			wantErr: true,
		},
		{
			name:    "file scheme rejected",
			url:     "file:///etc/passwd",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDownloadURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDownloadURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
