package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultRetries   = 3
	defaultTimeout   = 3 * time.Second
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; rv:78.0) Gecko/20100101 Firefox/78.0"
)

type downloadContext struct {
	baseURL   string
	directory string
	client    *http.Client
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

func (c *downloadContext) doGetStream(path string) (*http.Response, error) {
	target := c.baseURL + "/" + path
	var lastErr error

	for attempt := 0; attempt < defaultRetries; attempt++ {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", defaultUserAgent)

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}
