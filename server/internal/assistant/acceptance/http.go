package acceptance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// The limits of an http check.
const (
	HTTPTimeout   = 10 * time.Second
	MostRedirects = 3
	MostBody      = 64 << 10
)

// HTTP is the GET an http check makes: http and https only, no credentials, no custom headers, a time limit, at most
// MostRedirects redirects and MostBody bytes of the body. Zero values mean the limits above and the default transport.
type HTTP struct {
	Timeout   time.Duration
	Transport http.RoundTripper
}

// Fetch GETs target.
func (h HTTP) Fetch(ctx context.Context, target string) (Page, error) {
	if _, err := ParseHTTP(target); err != nil {
		return Page{}, err
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = HTTPTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: timeout, Transport: h.Transport, CheckRedirect: func(next *http.Request, via []*http.Request) error {
		switch {
		case len(via) > MostRedirects:
			return fmt.Errorf("more than %d redirects", MostRedirects)
		case next.URL.Scheme != "http" && next.URL.Scheme != "https":
			return errors.New("redirected to a URL that is not http or https")
		case next.URL.User != nil:
			return errors.New("redirected to a URL with credentials")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Page{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		var failed *url.Error
		if errors.As(err, &failed) {
			err = failed.Err
		}
		return Page{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MostBody))
	if err != nil {
		return Page{}, err
	}
	return Page{Status: resp.StatusCode, Body: body}, nil
}
