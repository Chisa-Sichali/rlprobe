package probe

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ParseHeader splits one "Name: Value" line. An empty line is not an error
// and returns empty strings, which means "no header".
func ParseHeader(line string) (string, string, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", nil
	}
	name, value, ok := strings.Cut(line, ":")
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if !ok || name == "" {
		return "", "", fmt.Errorf("header must look like Name: Value")
	}
	return name, value, nil
}

func newClient(cfg Config) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Insecure {
		// Only set when the user passes --insecure, for local certificates.
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	transport.MaxIdleConns = cfg.Concurrency
	transport.MaxIdleConnsPerHost = cfg.Concurrency
	return &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
	}
}

func perform(ctx context.Context, client *http.Client, cfg Config, target float64) Result {
	result := Result{At: time.Now(), TargetRate: target}
	reqCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	var body io.Reader
	if len(cfg.Body) > 0 {
		body = bytes.NewReader(cfg.Body)
	}
	req, err := http.NewRequestWithContext(reqCtx, cfg.Method, cfg.URL, body)
	if err != nil {
		result.Err = err
		return result
	}
	for key, values := range cfg.Headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "rlprobe/1.0")
	}

	start := time.Now()
	resp, err := client.Do(req)
	result.Latency = time.Since(start)
	result.At = time.Now()
	if err != nil {
		result.Err = err
		return result
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	retry, headers := pickRateHeaders(resp.Header)
	result.Status = resp.StatusCode
	result.RetryAfter = retry
	result.RateHeaders = headers
	result.Limited = resp.StatusCode == http.StatusTooManyRequests || retry != "" || cfg.LimitStatuses[resp.StatusCode]
	return result
}

func pickRateHeaders(header http.Header) (string, map[string]string) {
	found := make(map[string]string)
	retry := ""
	for key, values := range header {
		if len(values) == 0 || !isRateHeader(key) {
			continue
		}
		found[key] = values[0]
		if http.CanonicalHeaderKey(key) == "Retry-After" {
			retry = values[0]
		}
	}
	return retry, found
}

func isRateHeader(key string) bool {
	canonical := http.CanonicalHeaderKey(key)
	return canonical == "Retry-After" ||
		strings.HasPrefix(canonical, "X-Ratelimit-") ||
		strings.HasPrefix(canonical, "Ratelimit-")
}
