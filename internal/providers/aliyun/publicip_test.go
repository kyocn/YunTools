package aliyun

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPublicIPParsesDomesticResponseAndRejectsSyntheticAddresses(t *testing.T) {
	for _, tc := range []struct {
		body string
		want string
	}{
		{"当前 IP：8.8.4.4  来自于：中国 测试\n", "8.8.4.4"},
		{"8.8.4.4", "8.8.4.4"},
		{"当前 IP：198.18.0.4  来自于：代理", ""},
		{"198.19.255.254", ""},
		{"100.64.0.1", ""},
		{"当前 IP：10.0.0.1  来自于：内网", ""},
		{"<html>当前 IP：8.8.4.4  来自于：测试</html>", ""},
		{"当前 IP：8.8.4.4 9.9.9.9  来自于：测试", ""},
		{"当前 IP：8.8.4.4", ""},
		{strings.Repeat(" ", 1024) + "8.8.4.4", ""},
	} {
		t.Run(tc.body[:min(len(tc.body), 50)], func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			got, err := fetchPublicIP(context.Background(), client, "https://myip.ipip.net")
			if tc.want == "" {
				if err == nil {
					t.Fatalf("accepted unsafe response: %q", got)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestPublicIPBypassesHTTPProxyWithoutChangingOtherClients(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "8.8.4.4")
	}))
	defer origin.Close()
	var proxyCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		http.Error(w, "proxy must not receive IP detection", http.StatusBadGateway)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	client := origin.Client()
	transport := client.Transport.(*http.Transport)
	transport.Proxy = http.ProxyURL(proxyURL)
	got, err := fetchPublicIP(context.Background(), client, origin.URL)
	if err != nil || got != "8.8.4.4" || proxyCalls.Load() != 0 {
		t.Fatalf("got %q err=%v proxyCalls=%d", got, err, proxyCalls.Load())
	}
	request, _ := http.NewRequest("GET", origin.URL, nil)
	if p, _ := transport.Proxy(request); p == nil || p.String() != proxy.URL {
		t.Fatal("IP lookup changed the shared client's proxy configuration")
	}
}
