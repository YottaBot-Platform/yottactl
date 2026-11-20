package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Plan #244 WP2b-3: the response form exposes headers (an ETag) without
// changing Do/DoWithHeaders for their existing callers.
func TestDoResponseReturnsHeadersAndLeavesDoUnchanged(t *testing.T) {
	var gotAuth, gotVersion, gotIfMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotVersion, gotIfMatch = r.Header.Get("Authorization"), r.Header.Get("X-Yotta-Client-Version"), r.Header.Get("If-Match")
		w.Header().Set("ETag", `"draft-7-abc"`)
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")

	resp, err := c.DoResponse(http.MethodGet, "/x", nil, map[string]string{
		"If-Match": `"draft-6-abc"`, "Authorization": "Bearer stolen", "X-Yotta-Client-Version": "forged"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != http.StatusTeapot || string(resp.Body) != `{"ok":true}` || resp.Header.Get("ETag") != `"draft-7-abc"` {
		t.Errorf("response = %d %s ETag %q", resp.Status, resp.Body, resp.Header.Get("ETag"))
	}
	if gotAuth != "Bearer tok" || gotVersion != Version || gotIfMatch != `"draft-6-abc"` {
		t.Errorf("sent Authorization %q, version %q, If-Match %q: the client owns the first two", gotAuth, gotVersion, gotIfMatch)
	}

	status, body, err := c.Do(http.MethodGet, "/x", nil)
	if err != nil || status != http.StatusTeapot || string(body) != `{"ok":true}` {
		t.Errorf("Do = %d %s %v", status, body, err)
	}
	status, body, err = c.DoWithHeaders(http.MethodGet, "/x", nil, map[string]string{"If-Match": "m"})
	if err != nil || status != http.StatusTeapot || string(body) != `{"ok":true}` || gotIfMatch != "m" {
		t.Errorf("DoWithHeaders = %d %s %v (If-Match %q)", status, body, err, gotIfMatch)
	}
	if _, err := New("", "tok").DoResponse(http.MethodGet, "/x", nil, nil); err == nil {
		t.Error("no endpoint must be an error, as for Do")
	}
}

func TestRedirectDoesNotReplayCredentialsOrBody(t *testing.T) {
	for _, code := range []int{301, 302, 307, 308} {
		reached := false
		destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, code) }))
		c := New(origin.URL, "secret")
		status, _, err := c.Do("POST", "/v1/test", map[string]string{"secret": "body"})
		origin.Close()
		destination.Close()
		if err != nil || status != code || reached {
			t.Fatalf("redirect %d replayed=%v status=%d err=%v", code, reached, status, err)
		}
	}
}

func TestTraversalPathIsRejected(t *testing.T) {
	c := New("http://localhost:1", "")
	for _, path := range []string{"/v1/users/..", "/v1/users/./groups"} {
		if _, _, err := c.Do("DELETE", path, nil); err == nil || !strings.Contains(err.Error(), "traversal") {
			t.Fatalf("path %s: %v", path, err)
		}
	}
}

// Services record this as how a change was reported (Updated Method).
func TestClientName(t *testing.T) {
	t.Setenv("YOTTA_CLIENT", "")
	if got := clientName(); got != "yottactl" {
		t.Errorf("clientName = %q, want yottactl", got)
	}
	t.Setenv("YOTTA_CLIENT", "albus")
	if got := clientName(); got != "albus" {
		t.Errorf("clientName = %q, want albus", got)
	}
}
