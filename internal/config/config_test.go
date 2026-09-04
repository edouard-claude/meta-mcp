package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// setValid puts a complete, valid configuration in the environment, then lets
// each test break one variable.
func setValid(t *testing.T) {
	t.Helper()
	key32 := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv(EnvPublicURL, "https://mcp.example.re")
	t.Setenv(EnvTokenCipherKey, key32)
	t.Setenv(EnvJWTSigningKey, key32)
	t.Setenv(EnvMetaAppID, "1234567890")
	t.Setenv(EnvMetaAppSecret, "s3cr3t")
}

func TestLoadDefaults(t *testing.T) {
	setValid(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != defaultListenAddr || cfg.DBPath != defaultDBPath {
		t.Fatalf("défauts non appliqués: %+v", cfg)
	}
	if cfg.GraphVersion != defaultGraphVersion || cfg.MetaScopes != DefaultMetaScopes {
		t.Fatalf("défauts Meta non appliqués: %s / %s", cfg.GraphVersion, cfg.MetaScopes)
	}
	if cfg.AccessTokenTTL != time.Hour || cfg.RefreshTokenTTL != 720*time.Hour {
		t.Fatalf("TTL par défaut: %v / %v", cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	}
	if cfg.MCPResourceURL() != "https://mcp.example.re/mcp" {
		t.Fatalf("MCPResourceURL = %s", cfg.MCPResourceURL())
	}
	if cfg.MetaRedirectURI() != "https://mcp.example.re/meta/callback" {
		t.Fatalf("MetaRedirectURI = %s", cfg.MetaRedirectURI())
	}
	if !cfg.IsMetaUserAllowed("anyone") {
		t.Fatal("sans liste blanche, tout le monde doit être autorisé")
	}
}

func TestLoadTrimsTrailingSlash(t *testing.T) {
	setValid(t)
	t.Setenv(EnvPublicURL, "https://mcp.example.re/")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PublicURL != "https://mcp.example.re" {
		t.Fatalf("PublicURL = %q", cfg.PublicURL)
	}
}

func TestLoadRejects(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		expect string
	}{
		{"public_url manquant", map[string]string{EnvPublicURL: ""}, EnvPublicURL},
		{"public_url en http", map[string]string{EnvPublicURL: "http://mcp.example.re"}, "https"},
		{"public_url sans hôte", map[string]string{EnvPublicURL: "https://"}, "hôte"},
		{"clé de chiffrement trop courte", map[string]string{
			EnvTokenCipherKey: base64.StdEncoding.EncodeToString(make([]byte, 16)),
		}, "32 octets"},
		{"clé de signature trop courte", map[string]string{
			EnvJWTSigningKey: base64.StdEncoding.EncodeToString(make([]byte, 8)),
		}, "au moins 32"},
		{"clé non base64", map[string]string{EnvTokenCipherKey: "pas du base64 !!"}, "base64"},
		{"app id manquant", map[string]string{EnvMetaAppID: ""}, EnvMetaAppID},
		{"app secret manquant", map[string]string{EnvMetaAppSecret: ""}, EnvMetaAppSecret},
		{"ttl invalide", map[string]string{EnvAccessTokenTTL: "douze"}, EnvAccessTokenTTL},
		{"ttl négatif", map[string]string{EnvRefreshTokenTTL: "-1h"}, EnvRefreshTokenTTL},
		{"log format inconnu", map[string]string{EnvLogFormat: "xml"}, EnvLogFormat},
		{"version graph invalide", map[string]string{EnvMetaGraphVersion: "26.0"}, EnvMetaGraphVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setValid(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if err == nil {
				t.Fatal("Load a réussi alors qu'il devait échouer")
			}
			if !strings.Contains(err.Error(), tc.expect) {
				t.Fatalf("erreur = %v, elle devrait mentionner %q", err, tc.expect)
			}
		})
	}
}

func TestAllowedMetaUserIDs(t *testing.T) {
	setValid(t)
	t.Setenv(EnvAllowedMetaUserIDs, " 111 , 222 ,")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.IsMetaUserAllowed("111") || !cfg.IsMetaUserAllowed("222") {
		t.Fatal("un ID de la liste blanche est refusé")
	}
	if cfg.IsMetaUserAllowed("333") {
		t.Fatal("un ID hors liste blanche est accepté")
	}
}

func TestBase64AlphabetsAccepted(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i) | 0xf0
	}
	for name, encoded := range map[string]string{
		"std":    base64.StdEncoding.EncodeToString(key),
		"rawstd": base64.RawStdEncoding.EncodeToString(key),
		"url":    base64.URLEncoding.EncodeToString(key),
		"rawurl": base64.RawURLEncoding.EncodeToString(key),
	} {
		t.Run(name, func(t *testing.T) {
			setValid(t)
			t.Setenv(EnvTokenCipherKey, encoded)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if string(cfg.TokenCipherKey) != string(key) {
				t.Fatal("clé décodée différente")
			}
		})
	}
}
