package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/gen2brain/webp"

	_ "golang.org/x/image/webp"
)

const (
	maxImageSize            = 10 << 20
	maxDecodedImagePixels   = 40_000_000
	maxDuckAIImageDimension = 512
	compressedWebPQuality   = 82
	webPEncodingMethod      = 4
)

// ImageAttachment stores a local image for a chat message or session archive.
type ImageAttachment struct {
	Name            string `json:"name"`
	MIMEType        string `json:"mime_type"`
	Data            []byte `json:"data,omitempty"`
	ModerationToken string `json:"-"`
}

// ReadImage reads a supported image, normalizes it to the WebP format used by
// Duck.ai's browser client, and scales it to a 512-pixel maximum dimension.
// The source file is unchanged.
func ReadImage(path string) (ImageAttachment, error) {
	extension := strings.ToLower(filepath.Ext(path))
	mimeType, ok := supportedImageTypes[extension]
	if !ok {
		return ImageAttachment{}, fmt.Errorf("%s is not a supported image format (PNG, JPEG, or WebP)", path)
	}

	// #nosec G304 -- the CLI explicitly selects the image path; type, size, and signature are checked before use.
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
	data, mimeType, err = compressImageData(mimeType, data)
	if err != nil {
		return ImageAttachment{}, fmt.Errorf("could not prepare %s for Duck.ai: %w", path, err)
	}

	return ImageAttachment{Name: filepath.Base(path), MIMEType: mimeType, Data: data}, nil
}

func compressImageData(mimeType string, data []byte) ([]byte, string, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("decode image header: %w", err)
	}
	if config.Width < 1 || config.Height < 1 {
		return nil, "", fmt.Errorf("image has invalid dimensions %dx%d", config.Width, config.Height)
	}
	if int64(config.Width) > maxDecodedImagePixels/int64(config.Height) {
		return nil, "", fmt.Errorf("image exceeds the %d megapixel processing limit", maxDecodedImagePixels/1_000_000)
	}
	needsResize := config.Width > maxDuckAIImageDimension || config.Height > maxDuckAIImageDimension
	if !needsResize && mimeType == "image/webp" {
		return data, mimeType, nil
	}
	var decoded image.Image
	if mimeType == "image/webp" {
		decoded, err = webp.Decode(bytes.NewReader(data), webp.Options{AutoRotate: true})
	} else {
		decoded, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, "", fmt.Errorf("decode image: %w", err)
	}
	if mimeType == "image/jpeg" {
		decoded = orientJPEGImage(decoded, jpegOrientation(data))
	}
	if needsResize {
		decoded = resizeToMaxDimension(decoded, maxDuckAIImageDimension)
	}

	var encoded bytes.Buffer
	if err := webp.Encode(&encoded, decoded, webp.Options{
		Quality: compressedWebPQuality,
		Method:  webPEncodingMethod,
		Exact:   true,
	}); err != nil {
		return nil, "", fmt.Errorf("encode WebP image: %w", err)
	}
	if encoded.Len() == 0 || encoded.Len() > maxImageSize {
		return nil, "", fmt.Errorf("prepared WebP image exceeds the 10 MiB limit")
	}
	return encoded.Bytes(), "image/webp", nil
}

func resizeToMaxDimension(src image.Image, maxDimension int) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= maxDimension && height <= maxDimension {
		return src
	}
	scale := math.Min(float64(maxDimension)/float64(width), float64(maxDimension)/float64(height))
	dstWidth := max(1, int(math.Round(float64(width)*scale)))
	dstHeight := max(1, int(math.Round(float64(height)*scale)))
	dst := image.NewNRGBA64(image.Rect(0, 0, dstWidth, dstHeight))

	for y := 0; y < dstHeight; y++ {
		sourceY := (float64(y)+0.5)/scale - 0.5
		y0 := int(math.Floor(sourceY))
		fy := sourceY - float64(y0)
		if y0 < 0 {
			y0, fy = 0, 0
		}
		y1 := min(y0+1, height-1)
		for x := 0; x < dstWidth; x++ {
			sourceX := (float64(x)+0.5)/scale - 0.5
			x0 := int(math.Floor(sourceX))
			fx := sourceX - float64(x0)
			if x0 < 0 {
				x0, fx = 0, 0
			}
			x1 := min(x0+1, width-1)
			c00 := nrgba64At(src, bounds.Min.X+x0, bounds.Min.Y+y0)
			c10 := nrgba64At(src, bounds.Min.X+x1, bounds.Min.Y+y0)
			c01 := nrgba64At(src, bounds.Min.X+x0, bounds.Min.Y+y1)
			c11 := nrgba64At(src, bounds.Min.X+x1, bounds.Min.Y+y1)
			dst.SetNRGBA64(x, y, color.NRGBA64{
				R: bilinearChannel(c00.R, c10.R, c01.R, c11.R, fx, fy),
				G: bilinearChannel(c00.G, c10.G, c01.G, c11.G, fx, fy),
				B: bilinearChannel(c00.B, c10.B, c01.B, c11.B, fx, fy),
				A: bilinearChannel(c00.A, c10.A, c01.A, c11.A, fx, fy),
			})
		}
	}
	return dst
}

// nrgba64At reads a pixel as non-premultiplied channels. color.Color.RGBA
// returns alpha-premultiplied values, so storing those directly in an NRGBA
// destination would apply alpha twice and darken translucent pixels.
func nrgba64At(img image.Image, x, y int) color.NRGBA64 {
	r, g, b, a := img.At(x, y).RGBA()
	if a == 0 {
		return color.NRGBA64{}
	}
	if a == 0xffff {
		return color.NRGBA64{R: uint16(r), G: uint16(g), B: uint16(b), A: 0xffff}
	}
	return color.NRGBA64{
		R: unpremultiplyChannel(uint16(r), uint16(a)),
		G: unpremultiplyChannel(uint16(g), uint16(a)),
		B: unpremultiplyChannel(uint16(b), uint16(a)),
		A: uint16(a),
	}
}

func unpremultiplyChannel(value, alpha uint16) uint16 {
	if alpha == 0 {
		return 0
	}
	scaled := (uint32(value)*0xffff + uint32(alpha)/2) / uint32(alpha)
	return uint16(min(scaled, 0xffff))
}

func bilinearChannel(c00, c10, c01, c11 uint16, fx, fy float64) uint16 {
	top := float64(c00)*(1-fx) + float64(c10)*fx
	bottom := float64(c01)*(1-fx) + float64(c11)*fx
	return uint16(math.Round(top*(1-fy) + bottom*fy))
}

func jpegEXIFSegment(data []byte) []byte {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return nil
	}
	for offset := 2; offset+1 < len(data); {
		segmentStart := offset
		if data[offset] != 0xff {
			return nil
		}
		for offset < len(data) && data[offset] == 0xff {
			offset++
		}
		if offset >= len(data) {
			return nil
		}
		marker := data[offset]
		offset++
		if marker == 0xda || marker == 0xd9 {
			return nil
		}
		if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		if offset+2 > len(data) {
			return nil
		}
		segmentLength := int(data[offset])<<8 | int(data[offset+1])
		if segmentLength < 2 || offset+segmentLength > len(data) {
			return nil
		}
		payload := data[offset+2 : offset+segmentLength]
		if marker == 0xe1 && bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
			return data[segmentStart : offset+segmentLength]
		}
		offset += segmentLength
	}
	return nil
}

func jpegOrientation(data []byte) int {
	segment := jpegEXIFSegment(data)
	// APP1 marker, segment length, then the six-byte EXIF identifier.
	if len(segment) < 18 {
		return 1
	}
	tiff := segment[10:]
	var readUint16 func([]byte) uint16
	var readUint32 func([]byte) uint32
	switch string(tiff[:2]) {
	case "II":
		readUint16 = binary.LittleEndian.Uint16
		readUint32 = binary.LittleEndian.Uint32
	case "MM":
		readUint16 = binary.BigEndian.Uint16
		readUint32 = binary.BigEndian.Uint32
	default:
		return 1
	}
	if len(tiff) < 8 || readUint16(tiff[2:4]) != 42 {
		return 1
	}
	ifdOffset := uint64(readUint32(tiff[4:8]))
	if ifdOffset+2 > uint64(len(tiff)) {
		return 1
	}
	ifd := tiff[ifdOffset:]
	entries := int(readUint16(ifd[:2]))
	for index := 0; index < entries; index++ {
		entryOffset := 2 + index*12
		if entryOffset+12 > len(ifd) {
			return 1
		}
		entry := ifd[entryOffset : entryOffset+12]
		if readUint16(entry[:2]) != 0x0112 || readUint16(entry[2:4]) != 3 || readUint32(entry[4:8]) != 1 {
			continue
		}
		orientation := int(readUint16(entry[8:10]))
		if orientation >= 1 && orientation <= 8 {
			return orientation
		}
		return 1
	}
	return 1
}

func orientJPEGImage(src image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return src
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	dstWidth, dstHeight := width, height
	if orientation >= 5 {
		dstWidth, dstHeight = height, width
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dx, dy := x, y
			switch orientation {
			case 2:
				dx = width - 1 - x
			case 3:
				dx, dy = width-1-x, height-1-y
			case 4:
				dy = height - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = height-1-y, x
			case 7:
				dx, dy = height-1-y, width-1-x
			case 8:
				dx, dy = y, width-1-x
			}
			dst.Set(dx, dy, src.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return dst
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
