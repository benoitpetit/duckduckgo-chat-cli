package chatcontext

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Context holds the accumulated context from chained commands.
type Context struct {
	items []string
}

// New creates a new Context.
func New() *Context {
	return &Context{
		items: []string{},
	}
}

// AddFile adds file content to the context.
func (c *Context) AddFile(path string, content []byte) {
	c.items = append(c.items, fmt.Sprintf("[File Context]\nFile: %s\n\n%s", filepath.Base(path), string(content)))
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
	return len(c.items) == 0
}
