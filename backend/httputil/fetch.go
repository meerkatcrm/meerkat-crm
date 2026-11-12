package httputil

import (
	"errors"
	"io"
	"meerkat/netguard"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// validateURLForSSRF checks if a URL is safe to fetch (not pointing to internal resources)
func validateURLForSSRF(rawURL string) (*url.URL, error) {
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

	// Resolve the hostname to IP addresses
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, errors.New("failed to resolve hostname")
	}

	// Check all resolved IPs
	for _, ip := range ips {
		if netguard.IsBlockedIP(ip) {
			return nil, errors.New("access to internal IP addresses is not allowed")
		}
	}

	return parsedURL, nil
}

// FetchImageFromURL fetches an image from a URL with SSRF protection.
// Returns the image data, content type, and any error.
// The URL is sanitized to remove whitespace (handles Google VCF format).
func FetchImageFromURL(imageURL string) ([]byte, string, error) {
	// Clean URL - remove spaces and newlines (Google VCF format may have these)
	cleanURL := strings.ReplaceAll(imageURL, " ", "")
	cleanURL = strings.ReplaceAll(cleanURL, "\n", "")
	cleanURL = strings.ReplaceAll(cleanURL, "\r", "")

	// Validate the URL format and scheme
	parsedURL, err := validateURLForSSRF(cleanURL)
	if err != nil {
		return nil, "", err
	}

	return fetchImage(newSafeClient(), parsedURL.String())
}

// newSafeClient returns an HTTP client that refuses internal addresses, both at
// connection time and on redirects.
func newSafeClient() *http.Client {
	// netguard checks the address at connection time, which also prevents DNS rebinding/TOCTOU
	// attacks, and ignores HTTP(S)_PROXY so the checked address is the one dialled.
	return &http.Client{
		Timeout:       15 * time.Second,
		Transport:     netguard.NewTransport(errors.New("failed to resolve hostname"), errors.New("access to internal IP addresses is not allowed")),
		CheckRedirect: checkImageRedirect,
	}
}

// checkImageRedirect validates each redirect target and limits the number of hops.
func checkImageRedirect(req *http.Request, via []*http.Request) error {
	// Validate redirect target URL format
	_, err := validateURLForSSRF(req.URL.String())
	if err != nil {
		return errors.New("redirect to disallowed location")
	}
	if len(via) >= 3 {
		return errors.New("too many redirects")
	}
	return nil
}

// fetchImage downloads an already validated URL with the given client.
func fetchImage(client *http.Client, imageURL string) ([]byte, string, error) {
	// Fetch the image using the validated URL
	req, err := http.NewRequest("GET", imageURL, nil)
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
