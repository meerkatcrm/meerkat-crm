package httputil

import (
	"context"
	"errors"
	"io"
	"meerkat/netguard"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxFetches is how many user-supplied image URLs the server fetches at once; others wait.
const maxFetches = 4

var (
	// transport dials through netguard, which checks each address when it is dialled
	// (also for redirects, so DNS rebinding cannot reach an internal host) and ignores
	// HTTP(S)_PROXY so the checked address is the one dialled. Tests replace it.
	transport  http.RoundTripper = netguard.NewTransport(errors.New("failed to resolve hostname"), errors.New("access to internal IP addresses is not allowed"))
	fetchSlots                   = make(chan struct{}, maxFetches)
)

// validateURL accepts http and https URLs on the web ports. It rejects internal host names and
// IP literals without a DNS lookup; addresses behind a name are checked by netguard when they
// are dialled, which a check before the request cannot replace (a second DNS answer can differ).
func validateURL(rawURL string) (*url.URL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("invalid URL format")
	}

	// Only allow http and https schemes
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, errors.New("only http and https URLs are allowed")
	}

	host := parsedURL.Hostname()
	if host == "" {
		return nil, errors.New("URL must have a host")
	}

	// Block common internal hostnames
	lowerHost := strings.ToLower(host)
	blockedHosts := []string{"localhost", "127.0.0.1", "0.0.0.0", "::1", "[::1]"}
	for _, blocked := range blockedHosts {
		if lowerHost == blocked {
			return nil, errors.New("access to internal hosts is not allowed")
		}
	}

	if port := parsedURL.Port(); port != "" && port != "80" && port != "443" {
		return nil, errors.New("only ports 80 and 443 are allowed")
	}

	// An IP literal is checked here; it needs no lookup. A zone (fe80::1%en0) is dropped first.
	if ip := net.ParseIP(strings.SplitN(host, "%", 2)[0]); ip != nil && netguard.IsBlockedIP(ip) {
		return nil, errors.New("access to internal IP addresses is not allowed")
	}

	return parsedURL, nil
}

// FetchImageFromURL fetches an image from a URL that a user supplied (image proxy, vCard photo
// URLs, Monica gravatars). The risk and the remaining exposure are in
// docs/deployment.md, "Outgoing requests". Returns the image data, content type, and any
// error. The URL is sanitized to remove whitespace (handles Google VCF format).
func FetchImageFromURL(ctx context.Context, imageURL string) ([]byte, string, error) {
	// Clean URL - remove spaces and newlines (Google VCF format may have these)
	cleanURL := strings.ReplaceAll(imageURL, " ", "")
	cleanURL = strings.ReplaceAll(cleanURL, "\n", "")
	cleanURL = strings.ReplaceAll(cleanURL, "\r", "")

	parsedURL, err := validateURL(cleanURL)
	if err != nil {
		return nil, "", err
	}

	select {
	case fetchSlots <- struct{}{}:
		defer func() { <-fetchSlots }()
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}

	return fetchImage(ctx, newSafeClient(), parsedURL.String())
}

// newSafeClient returns an HTTP client that refuses internal addresses, both at
// connection time and on redirects.
func newSafeClient() *http.Client {
	return &http.Client{
		Timeout:       15 * time.Second,
		Transport:     transport,
		CheckRedirect: checkImageRedirect,
	}
}

// checkImageRedirect validates each redirect target and limits the number of hops.
func checkImageRedirect(req *http.Request, via []*http.Request) error {
	if _, err := validateURL(req.URL.String()); err != nil {
		return errors.New("redirect to disallowed location")
	}
	if len(via) >= 3 {
		return errors.New("too many redirects")
	}
	return nil
}

// fetchImage downloads an already validated URL with the given client.
func fetchImage(ctx context.Context, client *http.Client, imageURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", err
	}

	// Set a user agent to avoid being blocked by some servers
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MeerkatCRM/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", errors.New("failed to fetch image: remote server returned " + resp.Status)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", errors.New("URL does not point to an image")
	}

	// Limit response size (10MB)
	const maxSize = 10 * 1024 * 1024
	limitedReader := io.LimitReader(resp.Body, maxSize+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, "", err
	}

	if len(body) > maxSize {
		return nil, "", errors.New("image is too large, maximum size is 10MB")
	}

	return body, contentType, nil
}
