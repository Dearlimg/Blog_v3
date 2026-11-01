package agent

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type Searcher interface {
	Search(context.Context, string) ([]Source, error)
	Read(context.Context, string) (string, error)
}

type Web struct {
	endpoint string
	client   *http.Client
}

func publicIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96"} {
		if netip.MustParsePrefix(prefix).Contains(a) {
			return false
		}
	}
	return true
}

func validWebURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("invalid URL")
	}
	badScheme := u.Scheme != "https" && u.Scheme != "http"
	badPort := u.Port() != "" && u.Port() != "443" && u.Port() != "80"
	if badScheme || badPort || u.User != nil || u.Hostname() == "" {
		return nil, errors.New("URL not allowed")
	}
	return u, nil
}

func NewWeb(endpoint string) *Web {
	transport := &http.Transport{
		Proxy: nil, TLSHandshakeTimeout: 8 * time.Second,
		ResponseHeaderTimeout: 12 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errors.New("invalid network address")
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, errors.New("DNS lookup failed")
			}
			for _, ip := range ips {
				if !publicIP(ip) {
					return nil, errors.New("private network access denied")
				}
			}
			// Connect to the validated IP, not a second DNS lookup (rebinding defense).
			dialer := net.Dialer{Timeout: 10 * time.Second}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}
	return &Web{endpoint: endpoint, client: &http.Client{
		Transport: transport, Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return errors.New("too many redirects")
			}
			_, err := validWebURL(req.URL.String())
			return err
		},
	}}
}

func (w *Web) get(ctx context.Context, raw string) ([]byte, error) {
	if _, err := validWebURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, errors.New("invalid web request")
	}
	req.Header.Set("User-Agent", "DurlimBlogResearch/1.0")
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, errors.New("web request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil, errors.New("source unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("source too large or unreadable")
	}
	return data, nil
}

func (w *Web) Search(ctx context.Context, query string) ([]Source, error) {
	u, err := validWebURL(w.endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "rss")
	q.Set("count", "6")
	u.RawQuery = q.Encode()
	data, err := w.get(ctx, u.String())
	if err != nil {
		return nil, err
	}
	var rss struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
		} `xml:"channel>item"`
	}
	if err := xml.Unmarshal(data, &rss); err != nil {
		return nil, errors.New("search provider returned invalid RSS")
	}
	result := []Source{}
	for _, item := range rss.Items {
		if _, err := validWebURL(item.Link); err != nil {
			continue
		}
		result = append(result, Source{Title: clip(item.Title, 200), URL: item.Link,
			Snippet: clip(item.Description, 800), RetrievedAt: time.Now().UTC()})
		if len(result) == 6 {
			break
		}
	}
	if len(result) == 0 {
		return nil, errors.New("search returned no usable sources")
	}
	return result, nil
}

func (w *Web) Read(ctx context.Context, raw string) (string, error) {
	data, err := w.get(ctx, raw)
	if err != nil {
		return "", err
	}
	doc, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return "", errors.New("cannot parse page")
	}
	var text strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "svg", "nav", "footer":
				return
			}
		}
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
			text.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	return clip(strings.Join(strings.Fields(text.String()), " "), 12000), nil
}
