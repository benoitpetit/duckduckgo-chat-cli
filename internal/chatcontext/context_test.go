package chatcontext

import (
	"testing"

	"duckduckgo-chat-cli/internal/media"
)

func TestChainContextStoresAndReturnsImages(t *testing.T) {
	ctx := New()
	image := media.ImageAttachment{Name: "logo.png", MIMEType: "image/png", Data: []byte{1, 2, 3}}
	ctx.AddImage("/tmp/logo.png", image)

	if ctx.IsEmpty() {
		t.Fatal("image-only context is empty")
	}
	if got := ctx.ImageAttachments(); len(got) != 1 || got[0].Name != "logo.png" || string(got[0].Data) != string(image.Data) {
		t.Fatalf("ImageAttachments() = %+v, want image metadata and bytes", got)
	}
	if got := ctx.String(); got != "[Image Context]\nFile: logo.png" {
		t.Fatalf("String() = %q, want image marker", got)
	}
}
