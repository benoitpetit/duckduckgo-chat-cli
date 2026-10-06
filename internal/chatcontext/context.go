package chatcontext

import (
	"fmt"
	"path/filepath"
	"strings"

	"duckduckgo-chat-cli/internal/media"
)

// Context holds the accumulated context from chained commands.
type Context struct {
	items  []string
	images []media.ImageAttachment
}

// New creates a new Context.
func New() *Context {
	return &Context{
		items:  []string{},
		images: []media.ImageAttachment{},
	}
}

// AddFile adds file content to the context.
func (c *Context) AddFile(path string, content []byte) {
	c.items = append(c.items, fmt.Sprintf("[File Context]\nFile: %s\n\n%s", filepath.Base(path), string(content)))
}

// AddImage adds an image attachment to the chain and a text marker for the prompt.
func (c *Context) AddImage(path string, image media.ImageAttachment) {
	image.Name = filepath.Base(path)
	c.images = append(c.images, image)
	c.items = append(c.items, fmt.Sprintf("[Image Context]\nFile: %s", image.Name))
}

// ImageAttachments returns the images accumulated by this command chain.
func (c *Context) ImageAttachments() []media.ImageAttachment {
	images := make([]media.ImageAttachment, len(c.images))
	for i, image := range c.images {
		images[i] = image
		images[i].Data = append([]byte(nil), image.Data...)
	}
	return images
}

// AddURL adds URL content to the context.
func (c *Context) AddURL(url string, content string) {
	c.items = append(c.items, fmt.Sprintf("[URL Context]\nURL: %s\n\n%s", url, content))
}

// AddSearch adds search results to the context.
func (c *Context) AddSearch(query string, results string) {
	c.items = append(c.items, fmt.Sprintf("[Search Context]\nQuery: %s\n\n%s", query, results))
}

// String returns the full accumulated context as a single string.
func (c *Context) String() string {
	return strings.Join(c.items, "\n\n")
}

// IsEmpty returns true if the context has no items.
func (c *Context) IsEmpty() bool {
	return len(c.items) == 0 && len(c.images) == 0
}
