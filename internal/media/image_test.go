package media

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReadImageAcceptsPNGJPEGAndWebP(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		mime string
	}{
		{name: "duck.png", data: []byte("\x89PNG\r\n\x1a\nimage"), mime: "image/png"},
		{name: "duck.jpg", data: []byte("\xff\xd8\xff\xe0image"), mime: "image/jpeg"},
		{name: "duck.jpeg", data: []byte("\xff\xd8\xff\xe0image"), mime: "image/jpeg"},
		{name: "duck.webp", data: []byte("RIFF\x04\x00\x00\x00WEBPimage"), mime: "image/webp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tt.name)
			if err := os.WriteFile(path, tt.data, 0600); err != nil {
				t.Fatal(err)
			}

			got, err := ReadImage(path)
			if err != nil {
				t.Fatalf("ReadImage() error = %v", err)
			}
			if got.Name != tt.name || got.MIMEType != tt.mime || !bytes.Equal(got.Data, tt.data) {
				t.Fatalf("ReadImage() = {Name:%q MIMEType:%q Data:%v}, want name %q, MIME %q, unchanged bytes", got.Name, got.MIMEType, got.Data, tt.name, tt.mime)
			}
		})
	}
}

func TestReadImageRejectsExtensionSignatureMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duck.jpg")
	if err := os.WriteFile(path, []byte("\x89PNG\r\n\x1a\nimage"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadImage(path); err == nil {
		t.Fatal("ReadImage() accepted PNG bytes with a JPEG extension")
	}
}

func TestReadImageRejectsUnsupportedAndOversizeFiles(t *testing.T) {
	dir := t.TempDir()
	unsupported := filepath.Join(dir, "duck.gif")
	if err := os.WriteFile(unsupported, []byte("GIF89a"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadImage(unsupported); err == nil {
		t.Fatal("ReadImage() accepted GIF")
	}

	oversize := filepath.Join(dir, "large.png")
	if err := os.WriteFile(oversize, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(oversize, (10<<20)+1); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadImage(oversize); err == nil {
		t.Fatal("ReadImage() accepted a file larger than 10 MiB")
	}
}

func TestReadImageRejectsDirectoryAndUnreadablePath(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{dir, filepath.Join(dir, "missing.png")} {
		if _, err := ReadImage(path); err == nil {
			t.Errorf("ReadImage(%q) unexpectedly succeeded", path)
		}
	}
}
