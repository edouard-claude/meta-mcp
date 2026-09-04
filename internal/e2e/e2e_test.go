//go:build e2e

// Package e2e runs the compiled binary against a fake Graph API and drives it
// like a real MCP client would. Run it with `make e2e`.
package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edouard/metasocial-mcp/internal/adapters/authserver"
	"github.com/edouard/metasocial-mcp/internal/adapters/crypto"
	"github.com/edouard/metasocial-mcp/internal/adapters/sqlite"
	"github.com/edouard/metasocial-mcp/internal/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	publicURL  = "https://mcp.e2e.test"
	binaryPath = "../../bin/metasocial-mcp"
	tenantID   = "11111111-2222-4333-8444-555555555555"
)

// realClock is the clock the minted token is dated with.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type harness struct {
	endpoint    string
	accessToken string
	graphCalls  func() []string
}

// start seeds a database, launches the binary against a fake Graph and waits
// for it to answer /healthz.
func start(t *testing.T) *harness {
	t.Helper()
	if _, err := os.Stat(binaryPath); err != nil {
		t.Fatalf("binaire absent, lancez `make build`: %v", err)
	}

	var calls []string
	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(graph.Close)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "e2e.db")
	cipherKey := bytes.Repeat([]byte{0x11}, 32)
	signingKey := bytes.Repeat([]byte{0x22}, 32)
	seed(t, dbPath, cipherKey)

	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	cmd := exec.Command(binaryPath)
	cmd.Env = append(os.Environ(),
		"PUBLIC_URL="+publicURL,
		"LISTEN_ADDR="+addr,
		"DB_PATH="+dbPath,
		"TOKEN_CIPHER_KEY="+base64.StdEncoding.EncodeToString(cipherKey),
		"JWT_SIGNING_KEY="+base64.StdEncoding.EncodeToString(signingKey),
		"META_APP_ID=e2e-app",
		"META_APP_SECRET=e2e-secret",
		"META_GRAPH_BASE_URL="+graph.URL,
		"META_DIALOG_BASE_URL="+graph.URL,
		"LOG_FORMAT=text",
	)
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("démarrage du binaire: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _, _ = cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
		}
		if t.Failed() {
			t.Logf("journal du serveur:\n%s", logs.String())
		}
	})

	base := "http://" + addr
	waitForHealth(t, base+"/healthz")

	// The MCP client needs a bearer token; mint one exactly as the running
	// binary would, with the same issuer, audience and signing key.
	auth := authserver.New(nil, realClock{}, authserver.Options{
		Issuer:         publicURL,
		Resource:       publicURL + "/mcp",
		SigningKey:     signingKey,
		AccessTokenTTL: time.Hour,
	}, slog.New(slog.DiscardHandler))
	token, _, err := auth.MintAccessToken(tenantID, "e2e-client")
	if err != nil {
		t.Fatalf("MintAccessToken: %v", err)
	}

	return &harness{
		endpoint:    base + "/mcp",
		accessToken: token,
		graphCalls:  func() []string { return calls },
	}
}

// seed writes one tenant with two pages straight into the database the binary
// will open.
func seed(t *testing.T, dbPath string, cipherKey []byte) {
	t.Helper()
	cipher, err := crypto.New(cipherKey)
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}
	store, err := sqlite.New(context.Background(), dbPath, cipher)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	if err := store.UpsertTenant(context.Background(), &domain.Tenant{
		ID: tenantID, MetaUserID: "meta-e2e", DisplayName: "Compte e2e",
		UserToken: "USER-TOKEN", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertTenant: %v", err)
	}
	if err := store.ReplacePages(context.Background(), tenantID, []domain.Page{
		{PageID: "page-e2e-1", Name: "Boulangerie du Port", PageToken: "PT-1",
			IGUserID: "ig-e2e", IGUsername: "boulangerieduport", SyncedAt: now},
		{PageID: "page-e2e-2", Name: "Snack Créole", PageToken: "PT-2", SyncedAt: now},
	}); err != nil {
		t.Fatalf("ReplacePages: %v", err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("port libre: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForHealth(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("le serveur n'a pas répondu sur %s", url)
}

// bearerTransport adds the access token to every request.
type bearerTransport struct{ token string }

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(clone)
}

func TestUnauthenticatedRequestAdvertisesTheAuthorizationServer(t *testing.T) {
	h := start(t)

	resp, err := http.Post(h.endpoint, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, attendu 401", resp.StatusCode)
	}
	want := publicURL + "/.well-known/oauth-protected-resource"
	if challenge := resp.Header.Get("WWW-Authenticate"); !strings.Contains(challenge, want) {
		t.Fatalf("WWW-Authenticate = %q", challenge)
	}
}

func TestListPagesThroughTheRealBinary(t *testing.T) {
	h := start(t)

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:             h.endpoint,
		HTTPClient:           &http.Client{Transport: &bearerTransport{token: h.accessToken}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connexion MCP: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) < 15 {
		t.Fatalf("%d outils exposés, attendu au moins 15", len(tools.Tools))
	}

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_pages"})
	if err != nil {
		t.Fatalf("CallTool list_pages: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_pages en erreur: %+v", res.Content)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("contenu de type %T", res.Content[0])
	}
	var pages []struct {
		PageID     string `json:"page_id"`
		Name       string `json:"name"`
		IGUsername string `json:"ig_username"`
	}
	if err := json.Unmarshal([]byte(text.Text), &pages); err != nil {
		t.Fatalf("décodage %q: %v", text.Text, err)
	}
	if len(pages) != 2 {
		t.Fatalf("%d pages: %s", len(pages), text.Text)
	}
	if pages[0].Name != "Boulangerie du Port" || pages[0].IGUsername != "boulangerieduport" {
		t.Fatalf("page = %+v", pages[0])
	}
	if strings.Contains(text.Text, "PT-1") || strings.Contains(text.Text, "PT-2") {
		t.Fatalf("un jeton de page a fuité: %s", text.Text)
	}
}

// TestInspectorCLI drives the same endpoint with the official MCP inspector,
// which is the reference client of the specification. It is skipped when npx
// is unavailable.
func TestInspectorCLI(t *testing.T) {
	npx, err := exec.LookPath("npx")
	if err != nil {
		t.Skip("npx introuvable, inspecteur MCP ignoré")
	}
	h := start(t)

	// The first run downloads the inspector, which can take minutes.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	// The published inspector does not pull /sdk, which
	// one of its own dependencies imports, so it is co-installed here.
	cmd := exec.CommandContext(ctx, npx, "-y",
		"-p", "@modelcontextprotocol/inspector",
		"-p", "@modelcontextprotocol/sdk",
		"mcp-inspector",
		"--cli", h.endpoint,
		"--transport", "http",
		"--header", "Authorization: Bearer "+h.accessToken,
		"--method", "tools/call",
		"--tool-name", "list_pages",
		"--connect-timeout", "15000",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inspecteur MCP: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "page-e2e-1") {
		t.Fatalf("sortie inattendue de l'inspecteur:\n%s", out)
	}
}
