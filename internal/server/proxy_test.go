package server

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestUnsafeIP(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1": true,
		"10.0.0.1": true,
		"192.168.1.1": true,
		"169.254.1.1": true,
		"100.64.1.1": true,
		"8.8.8.8": false,
		"2606:4700:4700::1111": false,
	}
	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			if got := unsafeIP(netip.MustParseAddr(raw)); got != want { t.Fatalf("unsafeIP(%s)=%v, want %v", raw, got, want) }
		})
	}
}

func TestBuildTargetCannotChangeOrigin(t *testing.T) {
	u, err := buildTarget(proxyRequest{Server: "https://api.example.com/v1", Path: "/pets", Query: map[string]string{"q": "cat dog"}})
	if err != nil { t.Fatal(err) }
	if got, want := u.String(), "https://api.example.com/v1/pets?q=cat+dog"; got != want { t.Fatalf("got %s want %s", got, want) }
	if _, err := buildTarget(proxyRequest{Server: "https://api.example.com", Path: "https://evil.example/x"}); err == nil { t.Fatal("absolute path should be rejected") }
}

func TestHeaderFiltering(t *testing.T) {
	for _, key := range []string{"Cookie", "Host", "Connection", "Transfer-Encoding"} { if safeRequestHeader(key) { t.Fatalf("%s should be rejected", key) } }
	if !safeRequestHeader("Authorization") { t.Fatal("Authorization should be forwarded") }
	h := http.Header{"Set-Cookie": {"secret=x"}, "Connection": {"close"}, "Content-Type": {"application/json"}}
	stripResponseHeaders(h)
	if h.Get("Set-Cookie") != "" || h.Get("Connection") != "" { t.Fatal("unsafe response headers remain") }
	if h.Get("Content-Type") == "" { t.Fatal("safe response header stripped") }
}

func TestSameOrigin(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "https://reader.example/api/proxy", nil)
	r.Header.Set("Origin", "https://reader.example")
	if !sameOrigin(r) { t.Fatal("same origin rejected") }
	r.Header.Set("Origin", "https://evil.example")
	if sameOrigin(r) { t.Fatal("cross origin accepted") }
}

func TestRateLimit(t *testing.T) {
	p := &proxy{hits: map[string][]time.Time{}}
	for i:=0;i<60;i++ { if !p.rateLimit("client") { t.Fatalf("request %d rejected early", i+1) } }
	if p.rateLimit("client") { t.Fatal("61st request should be rejected") }
}
