package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultRetries   = 3
	defaultTimeout   = 5 * time.Second
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; rv:78.0) Gecko/20100101 Firefox/78.0"
)

type downloadContext struct {
	baseURL   string
	directory string
	client    *http.Client
	verbose   bool
	fetched   atomic.Int64
}

// logf prints informational output to stdout only in verbose mode.
// Errors and warnings go to stderr unconditionally.
func (c *downloadContext) logf(format string, args ...any) {
	if c.verbose {
		fmt.Printf(format, args...)
	}
}

func newHTTPClient() *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}
	return &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func verifyResponse(resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("responded with status code %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Length") == "0" {
		return fmt.Errorf("responded with a zero-length body")
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return fmt.Errorf("responded with HTML")
	}
	return nil
}

func (c *downloadContext) doGet(path string) (*http.Response, []byte, error) {
	return c.getWithRetry(path, true)
}

func (c *downloadContext) doGetStream(path string) (*http.Response, error) {
	resp, _, err := c.getWithRetry(path, false)
	return resp, err
}

// getWithRetry fetches path, retrying transient failures. When buffered is
// true the body is read fully into memory and closed before returning;
// otherwise the caller owns the open response body and must close it.
func (c *downloadContext) getWithRetry(path string, buffered bool) (*http.Response, []byte, error) {
	target := c.baseURL + "/" + path
	var lastErr error

	for attempt := 0; attempt < defaultRetries; attempt++ {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("User-Agent", defaultUserAgent)

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if !buffered {
			return resp, nil, nil
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		return resp, body, nil
	}
	return nil, nil, lastErr
}
