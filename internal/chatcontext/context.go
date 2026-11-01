package chatcontext

import (
	"fmt"
	"path/filepath"
	"strings"

	"duckduckgo-chat-cli/internal/media"
)

type SourceKind string

const (
	SourceKindFile    SourceKind = "file"
	SourceKindImage   SourceKind = "image"
	SourceKindURL     SourceKind = "url"
	SourceKindSearch  SourceKind = "search"
	SourceKindLibrary SourceKind = "library"
)

type SourceState string

const (
	SourceStateActive     SourceState = "active"
	SourceStateCompressed SourceState = "compressed"
	SourceStateTruncated  SourceState = "truncated"
	SourceStateOmitted    SourceState = "omitted"
)

type SourceRef struct {
	ID         string      `json:"id"`
	Kind       SourceKind  `json:"kind"`
	Name       string      `json:"name"`
	URI        string      `json:"uri,omitempty"`
	SizeBytes  int         `json:"size_bytes"`
	State      SourceState `json:"state"`
	Pinned     bool        `json:"pinned,omitempty"`
	ExchangeID string      `json:"exchange_id,omitempty"`
}

// Context holds the accumulated context from chained commands.
type Context struct {
	items   []string
	images  []media.ImageAttachment
	sources []SourceRef
}

// New creates a new Context.
func New() *Context {
	return &Context{
		items:   []string{},
		images:  []media.ImageAttachment{},
		sources: []SourceRef{},
	}
}

// AddFile adds file content to the context.
func (c *Context) AddFile(path string, content []byte) {
	c.items = append(c.items, fmt.Sprintf("[File Context]\nFile: %s\n\n%s", filepath.Base(path), string(content)))
	c.addSource(SourceRef{Kind: SourceKindFile, Name: filepath.Base(path), URI: path, SizeBytes: len(content), State: SourceStateActive})
}

// AddImage adds an image attachment to the chain and a text marker for the prompt.
func (c *Context) AddImage(path string, image media.ImageAttachment) {
	image.Name = filepath.Base(path)
	c.images = append(c.images, image)
	c.items = append(c.items, fmt.Sprintf("[Image Context]\nFile: %s", image.Name))
	c.addSource(SourceRef{Kind: SourceKindImage, Name: image.Name, URI: path, SizeBytes: len(image.Data), State: SourceStateActive})
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
	c.addSource(SourceRef{Kind: SourceKindURL, Name: url, URI: url, SizeBytes: len(content), State: SourceStateActive})
}

// AddSearch adds search results to the context and retains their references.
func (c *Context) AddSearch(query string, results string, sources ...SourceRef) {
	c.items = append(c.items, fmt.Sprintf("[Search Context]\nQuery: %s\n\n%s", query, results))
	if len(sources) == 0 {
		sources = []SourceRef{{Kind: SourceKindSearch, Name: query, SizeBytes: len(results)}}
	}
	for _, source := range sources {
		source.Kind = SourceKindSearch
		source.State = SourceStateActive
		c.addSource(source)
	}
}

func (c *Context) addSource(source SourceRef) {
	source.ID = fmt.Sprintf("source-%d", len(c.sources)+1)
	if source.State == "" {
		source.State = SourceStateActive
	}
	c.sources = append(c.sources, source)
}

// Sources returns source metadata in the order each item entered the context.
func (c *Context) Sources() []SourceRef {
	return append([]SourceRef(nil), c.sources...)
}

// String returns the full accumulated context as a single string.
func (c *Context) String() string {
	return strings.Join(c.items, "\n\n")
}

// IsEmpty returns true if the context has no items.
func (c *Context) IsEmpty() bool {
	return len(c.items) == 0 && len(c.images) == 0
}
