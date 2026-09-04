package meta

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testVersion = "v26.0"

// fakeGraph replays canned Graph API answers. Routes are keyed by
// "METHOD /path" without the version prefix; the version is stripped so the
// fixtures stay readable.
type fakeGraph struct {
	t      *testing.T
	server *httptest.Server

	mu       sync.Mutex
	requests []recordedRequest
	routes   map[string]http.HandlerFunc
}

// recordedRequest is what the tests assert on afterwards.
type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Form   url.Values
}

func newFakeGraph(t *testing.T) *fakeGraph {
	t.Helper()
	g := &fakeGraph{t: t, routes: map[string]http.HandlerFunc{}}
	g.server = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.server.Close)
	return g
}

// URL is the base URL to hand to the client under test.
func (g *fakeGraph) URL() string { return g.server.URL }

// handle registers a route, e.g. handle("GET /me", ...).
func (g *fakeGraph) handle(route string, h http.HandlerFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.routes[route] = h
}

// json replies with a fixture file from testdata, substituting __NEXT__ with
// the given absolute URL so pagination points back at this server.
func (g *fakeGraph) json(route, fixture string, next string) {
	g.handle(route, func(w http.ResponseWriter, r *http.Request) {
		body := g.fixture(fixture)
		if next != "" {
			body = strings.ReplaceAll(body, "__NEXT__", g.server.URL+next)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
}

// fail replies with an error fixture and an HTTP status.
func (g *fakeGraph) fail(route, fixture string, status int, headers map[string]string) {
	g.handle(route, func(w http.ResponseWriter, r *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(g.fixture(fixture)))
	})
}

func (g *fakeGraph) fixture(name string) string {
	g.t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		g.t.Fatalf("lecture de la fixture %s: %v", name, err)
	}
	return string(body)
}

func (g *fakeGraph) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	path := strings.TrimPrefix(r.URL.Path, "/"+testVersion)

	g.mu.Lock()
	g.requests = append(g.requests, recordedRequest{
		Method: r.Method,
		Path:   path,
		Query:  r.URL.Query(),
		Form:   r.PostForm,
	})
	handler, ok := g.routes[r.Method+" "+path]
	g.mu.Unlock()

	if !ok {
		g.t.Errorf("route inattendue: %s %s", r.Method, path)
		http.Error(w, `{"error":{"message":"unknown route","code":100}}`, http.StatusNotFound)
		return
	}
	handler(w, r)
}

// calls returns every recorded request for a path.
func (g *fakeGraph) calls(path string) []recordedRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []recordedRequest
	for _, req := range g.requests {
		if req.Path == path {
			out = append(out, req)
		}
	}
	return out
}

// newTestClient points a Client at the fake Graph, with a retry delay short
// enough not to slow the suite down.
func (g *fakeGraph) newTestClient() *Client {
	return NewClient(Options{
		AppID:      "app-id",
		AppSecret:  "app-secret",
		Version:    testVersion,
		GraphBase:  g.URL(),
		DialogBase: g.URL(),
		Scopes:     "pages_show_list",
		RetryDelay: time.Millisecond,
	})
}
