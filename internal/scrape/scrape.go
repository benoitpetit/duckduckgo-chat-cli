package scrape

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

type WebContentResult struct {
	URL     string `json:"url"`
	Content string `json:"content"`
}

func WebContent(urlStr string) (*WebContentResult, error) {
	return WebContentContext(context.Background(), urlStr)
}

func WebContentContext(parent context.Context, urlStr string) (*WebContentResult, error) {
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		urlStr = "https://" + urlStr
	}
	if err := validateURL(urlStr); err != nil {
		return nil, err
	}

	ctx, cancel := chromedp.NewContext(parent)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var content string
	err := chromedp.Run(ctx,
		chromedp.Navigate(urlStr),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(extractionScript, &content),
	)
	if err != nil {
		log.Printf("Error fetching content for URL %s: %v\n", urlStr, err)
		return nil, err
	}

	cleaned := cleanContent(content)
	if len(cleaned) > 200_000 {
		cleaned = cleaned[:200_000]
	}
	result := &WebContentResult{
		URL:     urlStr,
		Content: cleaned,
	}
	return result, nil
}

func validateURL(raw string) error {
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("invalid URL: %s", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("URLs with embedded credentials are not allowed")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("local URLs are not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isRestrictedIP(ip) {
			return fmt.Errorf("private or local URLs are not allowed")
		}
		return nil
	}

	// Validate resolved addresses as well as literal IPs. This prevents a
	// public-looking hostname from resolving to localhost, private networks,
	// link-local addresses, or cloud metadata endpoints.
	lookupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return fmt.Errorf("could not resolve URL host: %w", err)
	}
	for _, address := range addresses {
		if isRestrictedIP(address.IP) {
			return fmt.Errorf("private or local URLs are not allowed")
		}
	}
	return nil
}

func isRestrictedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsLinkLocalMulticast()
}

func (wcr *WebContentResult) ToJSON() (string, error) {
	jsonData, err := json.MarshalIndent(wcr, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error marshalling JSON: %v", err)
	}
	return string(jsonData), nil
}

const extractionScript = `(function() {
	[...document.querySelectorAll('script, style')].forEach(e => e.remove());
	return document.body.innerText;
})()`

func cleanContent(text string) string {
	cleaned := regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	cleaned = strings.TrimSpace(cleaned)
	return cleaned
}
