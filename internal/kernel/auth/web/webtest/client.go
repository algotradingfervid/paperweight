package webtest

import (
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// Response is one answer; Body is html-unescaped.
type Response struct {
	Status int
	Header http.Header
	Body   string
}

// jarState is shared by a Client and the clients derived from it.
type jarState struct {
	t          testing.TB
	hc         *http.Client
	base       string
	cookieName string

	mu     sync.Mutex
	cookie *http.Cookie // the last session cookie set, with its attributes
	csrf   string       // the last CSRF token the harness reported
}

// Client is a browser with a cookie jar that does not follow redirects.
// HTMX, NoCSRF and WithHeader return a derived client sharing the jar.
type Client struct {
	s      *jarState
	htmx   bool
	noCSRF bool
	header http.Header
}

// Client returns a new client with an empty jar.
func (h *Harness) Client() *Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		h.t.Fatalf("webtest: cookiejar: %v", err)
	}
	hc := *h.HTTP.Client()
	hc.Jar = jar
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		s:      &jarState{t: h.t, hc: &hc, base: h.HTTP.URL, cookieName: h.cookieName},
		header: http.Header{},
	}
}

func (c *Client) derive(fn func(*Client)) *Client {
	d := &Client{s: c.s, htmx: c.htmx, noCSRF: c.noCSRF, header: c.header.Clone()}
	fn(d)
	return d
}

// HTMX returns a client that sends HX-Request: true.
func (c *Client) HTMX() *Client { return c.derive(func(d *Client) { d.htmx = true }) }

// NoCSRF returns a client whose Post adds no csrf_token.
func (c *Client) NoCSRF() *Client { return c.derive(func(d *Client) { d.noCSRF = true }) }

// WithHeader returns a client that sends header k: v.
func (c *Client) WithHeader(k, v string) *Client {
	return c.derive(func(d *Client) { d.header.Set(k, v) })
}

// Cookie returns the session cookie as last set, or nil.
func (c *Client) Cookie() *http.Cookie {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	if c.s.cookie == nil {
		return nil
	}
	ck := *c.s.cookie
	return &ck
}

// Login signs in as email through the harness-only sign-in route.
func (c *Client) Login(email, pw string) Response {
	return c.do(http.MethodPost, loginPath, url.Values{"email": {email}, "password": {pw}})
}

// Get sends a GET.
func (c *Client) Get(path string) Response { return c.do(http.MethodGet, path, nil) }

// Post sends form urlencoded, adding the last CSRF token seen as csrf_token
// unless NoCSRF.
func (c *Client) Post(path string, form url.Values) Response {
	f := url.Values{}
	for k, vs := range form {
		f[k] = append([]string(nil), vs...)
	}
	if !c.noCSRF {
		c.s.mu.Lock()
		if c.s.csrf != "" {
			f.Set("csrf_token", c.s.csrf)
		}
		c.s.mu.Unlock()
	}
	return c.do(http.MethodPost, path, f)
}

func (c *Client) do(method, path string, form url.Values) Response {
	t := c.s.t
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, c.s.base+path, body)
	if err != nil {
		t.Fatalf("webtest: %s %s: %v", method, path, err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if c.htmx {
		req.Header.Set("HX-Request", "true")
	}
	for k, vs := range c.header {
		req.Header[k] = vs
	}
	resp, err := c.s.hc.Do(req)
	if err != nil {
		t.Fatalf("webtest: %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("webtest: %s %s: reading body: %v", method, path, err)
	}
	if tok := resp.Header.Get(csrfEchoHeader); tok != "" {
		c.s.mu.Lock()
		c.s.csrf = tok
		c.s.mu.Unlock()
	}
	for _, ck := range resp.Cookies() {
		if ck.Name == c.s.cookieName {
			c.s.mu.Lock()
			c.s.cookie = ck
			if ck.MaxAge < 0 {
				c.s.cookie = nil
			}
			c.s.mu.Unlock()
		}
	}
	return Response{Status: resp.StatusCode, Header: resp.Header, Body: html.UnescapeString(string(b))}
}
