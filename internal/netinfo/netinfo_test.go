package netinfo

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidIPv4(t *testing.T) {
	for _, tc := range []struct {
		in string
		ok bool
	}{
		{"203.0.113.7", true}, {"0.0.0.0", true}, {"255.255.255.255", true}, {"010.1.1.1", true},
		{"256.1.1.1", false}, {"1.2.3", false}, {"1.2.3.4.5", false}, {"a.b.c.d", false},
		{"", false}, {"203.0.113.7\n", false}, {"::1", false}, {"1.2.3.-4", false},
	} {
		if got := ValidIPv4(tc.in); got != tc.ok {
			t.Errorf("ValidIPv4(%q) = %v, want %v", tc.in, got, tc.ok)
		}
	}
}

func TestValidPort(t *testing.T) {
	for _, tc := range []struct {
		in string
		ok bool
	}{
		{"1", true}, {"8000", true}, {"65535", true},
		{"0", false}, {"65536", false}, {"", false}, {"80a", false}, {"-1", false}, {"123456", false},
	} {
		if got := ValidPort(tc.in); got != tc.ok {
			t.Errorf("ValidPort(%q) = %v, want %v", tc.in, got, tc.ok)
		}
	}
}

func TestDetectPublicIPv4(t *testing.T) {
	serve := func(status int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
	}
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{200, "203.0.113.7", "203.0.113.7"},
		{200, "203.0.113.7\n", "203.0.113.7"},
		{200, "<html>blocked</html>", ""},
		{200, "", ""},
		{500, "203.0.113.7", ""},
		{200, "2001:db8::1", ""},
	} {
		s := serve(tc.status, tc.body)
		got := DetectPublicIPv4(s.URL)
		s.Close()
		if got != tc.want {
			t.Errorf("status %d body %q: got %q want %q", tc.status, tc.body, got, tc.want)
		}
	}
	if got := DetectPublicIPv4("http://127.0.0.1:1/"); got != "" {
		t.Errorf("unreachable endpoint: got %q", got)
	}
}

// A service that answers a Go client with a web page is skipped and the
// next one is asked; the root of ifconfig.me does exactly that.
func TestDetectPublicIPv4SkipsHTMLAndFallsBack(t *testing.T) {
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>203.0.113.9</body></html>"))
	}))
	defer html.Close()
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "monad-failover" {
			http.Error(w, "unexpected agent", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("203.0.113.9\n"))
	}))
	defer plain.Close()
	if got := DetectPublicIPv4(html.URL, plain.URL); got != "203.0.113.9" {
		t.Fatalf("got %q", got)
	}
	if got := DetectPublicIPv4(html.URL); got != "" {
		t.Fatalf("HTML accepted: %q", got)
	}
}
