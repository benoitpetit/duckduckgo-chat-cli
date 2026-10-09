package media

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
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

func TestReadImageReencodesJPEGOnlyWhenSmallerAndLeavesSourceUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photo.jpg")
	var original bytes.Buffer
	if err := jpeg.Encode(&original, testImage(480, 320), &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	originalBytes := append([]byte(nil), original.Bytes()...)
	if err := os.WriteFile(path, originalBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	attachment, err := ReadImage(path)
	if err != nil {
		t.Fatalf("ReadImage() error = %v", err)
	}
	if len(attachment.Data) >= len(originalBytes) {
		t.Fatalf("JPEG attachment size = %d, want smaller than original %d", len(attachment.Data), len(originalBytes))
	}
	if attachment.MIMEType != "image/jpeg" {
		t.Fatalf("JPEG MIME type = %q, want image/jpeg", attachment.MIMEType)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged, originalBytes) {
		t.Fatal("ReadImage() modified the source file")
	}
}

func TestReadImageResizesOversizedJPEGForDuckAI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large-photo.jpg")
	var original bytes.Buffer
	if err := jpeg.Encode(&original, testImage(1254, 1254), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	originalBytes := append([]byte(nil), original.Bytes()...)
	if err := os.WriteFile(path, originalBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	attachment, err := ReadImage(path)
	if err != nil {
		t.Fatalf("ReadImage() error = %v", err)
	}
	decoded, err := jpeg.DecodeConfig(bytes.NewReader(attachment.Data))
	if err != nil {
		t.Fatalf("compressed JPEG did not decode: %v", err)
	}
	if decoded.Width > maxDuckAIImageDimension || decoded.Height > maxDuckAIImageDimension {
		t.Fatalf("prepared image dimensions = %dx%d, want each side <= %d", decoded.Width, decoded.Height, maxDuckAIImageDimension)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged, originalBytes) {
		t.Fatal("ReadImage() modified the source file")
	}
}

func TestReadImageResizePreservesTranslucentPixelColors(t *testing.T) {
	// A uniform field of fully saturated red at 50% alpha. Resizing must not
	// apply alpha a second time, which would darken the color channels.
	const (
		width  = 1024
		height = 1024
	)
	source := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(source, source.Bounds(), &image.Uniform{color.NRGBA{R: 0xff, A: 0x80}}, image.Point{}, draw.Src)

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "translucent.png")
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	attachment, err := ReadImage(path)
	if err != nil {
		t.Fatalf("ReadImage() error = %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(attachment.Data))
	if err != nil {
		t.Fatalf("prepared PNG did not decode: %v", err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(attachment.Data))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width > maxDuckAIImageDimension || config.Height > maxDuckAIImageDimension {
		t.Fatalf("prepared image dimensions = %dx%d, want each side <= %d", config.Width, config.Height, maxDuckAIImageDimension)
	}

	center := decoded.Bounds().Min
	// Compare non-premultiplied channels: decoded.At().RGBA() would apply
	// alpha again and hide the very darkening this test guards against.
	got, ok := color.NRGBAModel.Convert(decoded.At(center.X, center.Y)).(color.NRGBA)
	if !ok {
		t.Fatalf("decoded pixel is not an NRGBA color")
	}
	if got.A != 0x80 {
		t.Fatalf("resized alpha = %#x, want 0x80", got.A)
	}
	// Tolerate rounding from bilinear resampling and the PNG round trip, but
	// catch double-alpha darkening, which would drop red to roughly half.
	if got.R < 0xf0 {
		t.Fatalf("resized red = %#x, want ~0xff; alpha appears applied twice", got.R)
	}
	if got.G > 0x10 || got.B > 0x10 {
		t.Fatalf("resized green/blue = %#x/%#x, want ~0x00; the opaque red field leaked", got.G, got.B)
	}
}

func TestReadImagePreservesJPEGWithEXIFMetadata(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, testImage(128, 96), &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}

	// JPEG APP1 segment with the EXIF identifier. Re-encoding would discard it.
	original := append([]byte(nil), encoded.Bytes()[:2]...)
	original = append(original, []byte{0xff, 0xe1, 0x00, 0x08, 'E', 'x', 'i', 'f', 0x00, 0x00}...)
	original = append(original, encoded.Bytes()[2:]...)
	path := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	attachment, err := ReadImage(path)
	if err != nil {
		t.Fatalf("ReadImage() error = %v", err)
	}
	if !bytes.Equal(attachment.Data, original) {
		t.Fatal("ReadImage() re-encoded JPEG and dropped EXIF metadata")
	}
}

func TestReadImagePreservesEXIFWhenResizingOversizedJPEG(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, testImage(1254, 900), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), encoded.Bytes()[:2]...)
	original = append(original, []byte{0xff, 0xe1, 0x00, 0x08, 'E', 'x', 'i', 'f', 0x00, 0x00}...)
	original = append(original, encoded.Bytes()[2:]...)
	path := filepath.Join(t.TempDir(), "oriented-photo.jpg")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	attachment, err := ReadImage(path)
	if err != nil {
		t.Fatalf("ReadImage() error = %v", err)
	}
	if !hasJPEGEXIF(attachment.Data) {
		t.Fatal("resized JPEG lost its EXIF metadata")
	}
	decoded, err := jpeg.DecodeConfig(bytes.NewReader(attachment.Data))
	if err != nil {
		t.Fatalf("resized JPEG did not decode: %v", err)
	}
	if decoded.Width > maxDuckAIImageDimension || decoded.Height > maxDuckAIImageDimension {
		t.Fatalf("resized image dimensions = %dx%d, want each side <= %d", decoded.Width, decoded.Height, maxDuckAIImageDimension)
	}
}

func TestReadImageReencodesPNGLosslesslyWhenSmaller(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photo.png")
	source := testPatternImage(512, 384)
	var original bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&original, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	attachment, err := ReadImage(path)
	if err != nil {
		t.Fatalf("ReadImage() error = %v", err)
	}
	if len(attachment.Data) >= original.Len() {
		t.Fatalf("PNG attachment size = %d, want smaller than original %d", len(attachment.Data), original.Len())
	}
	decoded, err := png.Decode(bytes.NewReader(attachment.Data))
	if err != nil {
		t.Fatalf("compressed PNG did not decode: %v", err)
	}
	if !imagesEqual(source, decoded) {
		t.Fatal("PNG re-encoding changed pixel values")
	}
}

func testImage(width, height int) *image.RGBA {
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	state := uint32(1)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			state = state*1664525 + 1013904223
			result.SetRGBA(x, y, color.RGBA{R: byte(state >> 24), G: byte(state >> 16), B: byte(state >> 8), A: 255})
		}
	}
	return result
}

func testPatternImage(width, height int) *image.RGBA {
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			result.SetRGBA(x, y, color.RGBA{R: byte((x/16)%16) * 16, G: byte((y/16)%16) * 16, B: byte(((x+y)/16)%16) * 16, A: 255})
		}
	}
	return result
}

func imagesEqual(left, right image.Image) bool {
	if left.Bounds() != right.Bounds() {
		return false
	}
	for y := left.Bounds().Min.Y; y < left.Bounds().Max.Y; y++ {
		for x := left.Bounds().Min.X; x < left.Bounds().Max.X; x++ {
			lr, lg, lb, la := left.At(x, y).RGBA()
			rr, rg, rb, ra := right.At(x, y).RGBA()
			if lr != rr || lg != rg || lb != rb || la != ra {
				return false
			}
		}
	}
	return true
}
