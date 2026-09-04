package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestGraphErrorClassification(t *testing.T) {
	cases := []struct {
		name       string
		err        GraphError
		auth       bool
		rateLimit  bool
		userSubstr string
	}{
		{"jeton invalide", GraphError{HTTPStatus: 400, Code: 190}, true, false, "reconnect_url"},
		{"session", GraphError{HTTPStatus: 400, Code: 102}, true, false, "reconnect_url"},
		{"permission 10", GraphError{HTTPStatus: 403, Code: 10}, true, false, "reconnect_url"},
		{"permission 200", GraphError{HTTPStatus: 403, Code: 200}, true, false, "reconnect_url"},
		{"401 sans code", GraphError{HTTPStatus: 401}, true, false, "reconnect_url"},
		{"quota app", GraphError{HTTPStatus: 400, Code: 4}, false, true, "quota"},
		{"quota utilisateur", GraphError{HTTPStatus: 400, Code: 17}, false, true, "quota"},
		{"quota page", GraphError{HTTPStatus: 400, Code: 32}, false, true, "quota"},
		{"quota générique", GraphError{HTTPStatus: 400, Code: 613}, false, true, "quota"},
		{"429", GraphError{HTTPStatus: 429}, false, true, "quota"},
		{"autre", GraphError{HTTPStatus: 400, Code: 100, Message: "champ inconnu"}, false, false, "champ inconnu"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.IsAuth(); got != tc.auth {
				t.Errorf("IsAuth = %v, attendu %v", got, tc.auth)
			}
			if got := tc.err.IsRateLimit(); got != tc.rateLimit {
				t.Errorf("IsRateLimit = %v, attendu %v", got, tc.rateLimit)
			}
			if msg := tc.err.UserMessage(); !strings.Contains(msg, tc.userSubstr) {
				t.Errorf("UserMessage = %q, attendu contenant %q", msg, tc.userSubstr)
			}
		})
	}
}

func TestGraphErrorMessageCarriesTheCodes(t *testing.T) {
	err := &GraphError{HTTPStatus: 400, Code: 190, Subcode: 463, Type: "OAuthException", Message: "expired"}
	msg := err.Error()
	for _, want := range []string{"400", "190", "463", "OAuthException", "expired"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Error() = %q, attendu contenant %q", msg, want)
		}
	}
}

func TestAsGraphError(t *testing.T) {
	ge := &GraphError{Code: 190}
	wrapped := fmt.Errorf("appel de l'API: %w", ge)

	got, ok := AsGraphError(wrapped)
	if !ok || got != ge {
		t.Fatalf("AsGraphError = %v, %v", got, ok)
	}
	if _, ok := AsGraphError(errors.New("autre chose")); ok {
		t.Fatal("une erreur quelconque a été prise pour une erreur Graph")
	}
	if _, ok := AsGraphError(nil); ok {
		t.Fatal("nil a été pris pour une erreur Graph")
	}
}

func TestRetryAfterIsCarried(t *testing.T) {
	err := &GraphError{Code: 4, RetryAfter: 30 * time.Second}
	if !err.IsRateLimit() || err.RetryAfter != 30*time.Second {
		t.Fatalf("erreur = %+v", err)
	}
}

func TestPageHasInstagram(t *testing.T) {
	if (&Page{}).HasInstagram() {
		t.Fatal("une page sans ig_user_id ne devrait pas avoir Instagram")
	}
	if !(&Page{IGUserID: "ig-1"}).HasInstagram() {
		t.Fatal("une page avec ig_user_id devrait avoir Instagram")
	}
}

func TestOAuthClientAllowsRedirectURI(t *testing.T) {
	client := &OAuthClient{RedirectURIs: []string{
		"https://claude.ai/api/mcp/auth_callback",
		"http://localhost:6274/oauth/callback",
	}}
	for _, uri := range client.RedirectURIs {
		if !client.AllowsRedirectURI(uri) {
			t.Fatalf("URI enregistrée refusée: %s", uri)
		}
	}
	// The match is exact: no prefix, no suffix, no case folding.
	for _, uri := range []string{
		"https://claude.ai/api/mcp/auth_callback/",
		"https://claude.ai/api/mcp",
		"https://CLAUDE.ai/api/mcp/auth_callback",
		"https://evil.example/cb",
		"",
	} {
		if client.AllowsRedirectURI(uri) {
			t.Fatalf("URI non enregistrée acceptée: %q", uri)
		}
	}
}

func TestPublishPostRequestShape(t *testing.T) {
	if (PublishPostRequest{Message: "x"}).IsPhoto() {
		t.Fatal("un message simple n'est pas une photo")
	}
	if !(PublishPostRequest{PhotoURL: "https://cdn/x.jpg"}).IsPhoto() {
		t.Fatal("photo_url devrait donner une photo")
	}
	if (PublishPostRequest{Message: "x"}).IsScheduled() {
		t.Fatal("sans date, la publication est immédiate")
	}
	if !(PublishPostRequest{ScheduledAt: time.Now()}).IsScheduled() {
		t.Fatal("avec une date, la publication est programmée")
	}
}
