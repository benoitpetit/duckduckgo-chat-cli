package media

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxImageSize = 10 << 20

// ImageAttachment stores a local image for a chat message or session archive.
type ImageAttachment struct {
	Name     string `json:"name"`
	MIMEType string `json:"mime_type"`
	Data     []byte `json:"data,omitempty"`
}

// ReadImage reads a supported image without decoding or changing its bytes.
func ReadImage(path string) (ImageAttachment, error) {
	extension := strings.ToLower(filepath.Ext(path))
	mimeType, ok := supportedImageTypes[extension]
	if !ok {
		return ImageAttachment{}, fmt.Errorf("%s is not a supported image format (PNG, JPEG, or WebP)", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return ImageAttachment{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ImageAttachment{}, err
	}
	if info.IsDir() {
		return ImageAttachment{}, fmt.Errorf("%s is a directory", path)
	}
	if !info.Mode().IsRegular() {
		return ImageAttachment{}, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > maxImageSize {
		return ImageAttachment{}, fmt.Errorf("%s exceeds the 10 MiB image limit", path)
	}

	data, err := io.ReadAll(io.LimitReader(file, maxImageSize+1))
	if err != nil {
		return ImageAttachment{}, err
	}
	if len(data) > maxImageSize {
		return ImageAttachment{}, fmt.Errorf("%s exceeds the 10 MiB image limit", path)
	}
	if !matchesImageSignature(mimeType, data) {
		return ImageAttachment{}, fmt.Errorf("%s file extension does not match its image data", path)
	}

	return ImageAttachment{Name: filepath.Base(path), MIMEType: mimeType, Data: data}, nil
}

var supportedImageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
}

func matchesImageSignature(mimeType string, data []byte) bool {
	switch mimeType {
	case "image/png":
		return bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n"))
	case "image/jpeg":
		return bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff})
	case "image/webp":
		return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	default:
		return false
	}
}
