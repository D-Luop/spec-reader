package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/D-Luop/spec-reader/internal/config"
	"gopkg.in/yaml.v3"
)

const maxBody = 10 << 20

type proxy struct {
	allowed      map[string]bool
	allowPrivate bool
	client       *http.Client
	mu           sync.Mutex
	hits         map[string][]time.Time
}

type proxyRequest struct {
	SpecID  string            `json:"specId"`
	Server  string            `json:"server"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

type proxyResponse struct {
	Status     int         `json:"status"`
	Headers    http.Header `json:"headers"`
	Body       string      `json:"body"`
	Base64     bool        `json:"base64,omitempty"`
	DurationMS int64       `json:"durationMs"`
	Size       int64       `json:"size"`
}

func newProxy(cfg config.Config) (*proxy, error) {
	allowed := map[string]bool{}
	for _, origin := range cfg.Proxy.ExtraOrigins {
		allowed[canonicalOrigin(origin)] = true
	}
	for _, spec := range cfg.Specs {
		b, err := osReadFile(spec.File)
		if err != nil {
			return nil, err
		}
		var doc struct {
			Servers []struct {
				URL string `yaml:"url"`
			} `yaml:"servers"`
		}
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return nil, fmt.Errorf("parse %s servers: %w", spec.ID, err)
		}
		for _, s := range doc.Servers {
			if u, err := url.Parse(s.URL); err == nil && u.IsAbs() {
				allowed[canonicalOrigin(s.URL)] = true
			}
		}
	}
	dialer := &safeDialer{allowPrivate: cfg.Proxy.AllowPrivateNetworks, resolver: net.DefaultResolver, dialer: &net.Dialer{Timeout: 10 * time.Second}}
	return &proxy{allowed: allowed, allowPrivate: cfg.Proxy.AllowPrivateNetworks, client: &http.Client{Timeout: time.Duration(cfg.Proxy.TimeoutSeconds) * time.Second, Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: http.ProxyFromEnvironment}}, hits: map[string][]time.Time{}}, nil
}

var osReadFile = func(name string) ([]byte, error) { return os.ReadFile(name) }

func (p *proxy) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Spec-Reader") != "1" || !sameOrigin(r) {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
		return
	}
	if !p.rateLimit(clientIP(r)) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var in proxyRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	method := strings.ToUpper(in.Method)
	if method == "" {
		method = http.MethodGet
	}
	if method == http.MethodConnect || method == http.MethodTrace {
		http.Error(w, "method not allowed", http.StatusBadRequest)
		return
	}
	target, err := buildTarget(in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !p.allowed[canonicalOrigin(target.String())] {
		http.Error(w, "origin is not allowed", http.StatusForbidden)
		return
	}
	body := strings.NewReader(string(in.Body))
	req, err := http.NewRequestWithContext(r.Context(), method, target.String(), body)
	if err != nil {
		http.Error(w, "invalid upstream request", 400)
		return
	}
	for k, v := range in.Headers {
		if safeRequestHeader(k) {
			req.Header.Set(k, v)
		}
	}
	started := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(b) > maxBody {
		http.Error(w, "upstream response too large", http.StatusBadGateway)
		return
	}
	headers := resp.Header.Clone()
	stripResponseHeaders(headers)
	out := proxyResponse{Status: resp.StatusCode, Headers: headers, DurationMS: time.Since(started).Milliseconds(), Size: int64(len(b))}
	if json.Valid(b) || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/") {
		out.Body = string(b)
	} else {
		out.Body = base64.StdEncoding.EncodeToString(b)
		out.Base64 = true
	}
	slog.Info("proxy request", "method", method, "origin", canonicalOrigin(target.String()), "path", target.EscapedPath(), "status", resp.StatusCode, "duration_ms", out.DurationMS)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func buildTarget(in proxyRequest) (*url.URL, error) {
	base, err := url.Parse(in.Server)
	if err != nil || !base.IsAbs() || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, errors.New("invalid server")
	}
	rel, err := url.Parse(in.Path)
	if err != nil || rel.IsAbs() || rel.Host != "" {
		return nil, errors.New("invalid path")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(rel.Path, "/")
	q := base.Query()
	for k, v := range in.Query {
		q.Set(k, v)
	}
	base.RawQuery = q.Encode()
	return base, nil
}

func canonicalOrigin(raw string) string {
	u, _ := url.Parse(raw)
	if u == nil {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	return canonicalOrigin(o) == canonicalOrigin("http://"+r.Host) || canonicalOrigin(o) == canonicalOrigin("https://"+r.Host)
}
func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return h
	}
	return r.RemoteAddr
}
func (p *proxy) rateLimit(ip string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	xs := p.hits[ip][:0]
	for _, t := range p.hits[ip] {
		if t.After(cutoff) {
			xs = append(xs, t)
		}
	}
	if len(xs) >= 60 {
		p.hits[ip] = xs
		return false
	}
	p.hits[ip] = append(xs, now)
	return true
}
func safeRequestHeader(k string) bool {
	switch strings.ToLower(k) {
	case "cookie", "host", "connection", "proxy-connection", "transfer-encoding", "upgrade", "content-length":
		return false
	}
	return true
}
func stripResponseHeaders(h http.Header) {
	for _, k := range []string{"Set-Cookie", "Connection", "Proxy-Connection", "Keep-Alive", "Transfer-Encoding", "Upgrade"} {
		h.Del(k)
	}
}

type safeDialer struct {
	allowPrivate bool
	resolver     *net.Resolver
	dialer       *net.Dialer
}

func (d *safeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addrs, err := d.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		if !d.allowPrivate && unsafeIP(addr) {
			return nil, fmt.Errorf("private address rejected")
		}
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("host resolved to no addresses")
	}
	return d.dialer.DialContext(ctx, network, net.JoinHostPort(addrs[0].String(), port))
}
func unsafeIP(a netip.Addr) bool {
	return !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsUnspecified() || a.IsMulticast() || netip.MustParsePrefix("100.64.0.0/10").Contains(a)
}
