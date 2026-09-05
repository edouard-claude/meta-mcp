package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// statusHarness wires a Service over a fakeGraph whose DebugToken answer the
// test controls.
func statusHarness(t *testing.T) (*Service, *fakeStore, *fakeGraph, *fakeClock) {
	t.Helper()
	return newServiceHarness(t)
}

func TestConnectionStatusHealthy(t *testing.T) {
	svc, _, graph, clk := statusHarness(t)
	graph.status = domain.TokenStatus{
		Valid:     true,
		ExpiresAt: clk.Now().Add(45 * 24 * time.Hour),
		Scopes:    []string{"pages_show_list", "instagram_basic"},
	}

	status, err := svc.ConnectionStatus(t.Context(), "tenant-a")
	if err != nil {
		t.Fatalf("ConnectionStatus: %v", err)
	}
	if !status.Healthy || !status.TokenValid {
		t.Fatalf("statut = %+v", status)
	}
	if status.Pages != 1 || status.PagesWithIG != 1 {
		t.Fatalf("pages = %d / %d", status.Pages, status.PagesWithIG)
	}
	if status.DaysRemaining != 45 {
		t.Fatalf("jours restants = %d", status.DaysRemaining)
	}
	if len(status.MissingScopes) != 0 {
		t.Fatalf("permissions manquantes = %v", status.MissingScopes)
	}
	// A healthy connection must not mint a reconnection state nobody uses.
	if status.ReconnectionURL != "" {
		t.Fatalf("lien de reconnexion inutile: %s", status.ReconnectionURL)
	}
	if !strings.Contains(status.Summary, "45") {
		t.Fatalf("résumé = %q", status.Summary)
	}
}

func TestConnectionStatusRevokedGivesAReconnectLink(t *testing.T) {
	svc, store, graph, _ := statusHarness(t)
	graph.status = domain.TokenStatus{Valid: false, Reason: "Session has been revoked"}

	status, err := svc.ConnectionStatus(t.Context(), "tenant-a")
	if err != nil {
		t.Fatalf("ConnectionStatus: %v", err)
	}
	if status.Healthy || status.TokenValid {
		t.Fatalf("statut = %+v", status)
	}
	if !strings.HasPrefix(status.ReconnectionURL, "https://mcp.example.re/meta/login?state=") {
		t.Fatalf("lien = %q", status.ReconnectionURL)
	}
	// The link must be usable, so its state has to exist in the store.
	state := strings.TrimPrefix(status.ReconnectionURL, "https://mcp.example.re/meta/login?state=")
	if _, err := store.ConsumeLoginState(t.Context(), state); err != nil {
		t.Fatalf("state non enregistré: %v", err)
	}
}

func TestConnectionStatusReportsMissingScopes(t *testing.T) {
	svc, _, graph, _ := statusHarness(t)
	// The user granted the pages permission but declined Instagram.
	graph.status = domain.TokenStatus{Valid: true, Scopes: []string{"pages_show_list"}}

	status, err := svc.ConnectionStatus(t.Context(), "tenant-a")
	if err != nil {
		t.Fatalf("ConnectionStatus: %v", err)
	}
	if status.Healthy {
		t.Fatal("une permission manquante devrait rendre la connexion dégradée")
	}
	if len(status.MissingScopes) != 1 || status.MissingScopes[0] != "instagram_basic" {
		t.Fatalf("permissions manquantes = %v", status.MissingScopes)
	}
	if !strings.Contains(status.Summary, "permission") {
		t.Fatalf("résumé = %q", status.Summary)
	}
}

// TestConnectionStatusSurvivesMetaBeingDown checks the diagnostic still says
// something useful when the diagnosis itself cannot be made.
func TestConnectionStatusSurvivesMetaBeingDown(t *testing.T) {
	svc, store, graph, clk := statusHarness(t)
	graph.statusErr = errors.New("connexion refusée")
	store.tenants["tenant-a"].UserTokenExpiresAt = clk.Now().Add(10 * 24 * time.Hour)

	status, err := svc.ConnectionStatus(t.Context(), "tenant-a")
	if err != nil {
		t.Fatalf("ConnectionStatus: %v", err)
	}
	if status.TokenValid {
		t.Fatal("le jeton ne peut pas être déclaré valide sans réponse de Meta")
	}
	if !strings.Contains(status.TokenReason, "joindre Meta") {
		t.Fatalf("raison = %q", status.TokenReason)
	}
	// The stored deadline is still worth reporting.
	if status.DaysRemaining != 10 {
		t.Fatalf("jours restants = %d", status.DaysRemaining)
	}
}

func TestConnectionStatusUnknownTenant(t *testing.T) {
	svc, _, _, _ := statusHarness(t)
	if _, err := svc.ConnectionStatus(t.Context(), "inconnu"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("erreur = %v", err)
	}
}
