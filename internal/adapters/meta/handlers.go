package meta

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/edouard/metasocial-mcp/internal/app"
	"github.com/edouard/metasocial-mcp/internal/domain"
	"github.com/edouard/metasocial-mcp/web"
)

// CodeIssuer mints the MCP authorization code once Facebook has told us who
// the user is. It is declared here, at the point of use, so this adapter
// never imports the authorization server.
type CodeIssuer interface {
	IssueAuthCode(r *http.Request, req domain.OAuthRequest, tenantID string) (string, error)
}

// HandlerOptions configures the Meta facing HTTP surface.
type HandlerOptions struct {
	// PublicURL is the base URL of this server.
	PublicURL string
	// RedirectURI is PUBLIC_URL/meta/callback, registered in the Meta app.
	RedirectURI string
	// AppSecret verifies the signed_request of the data deletion callback.
	AppSecret string
}

// Handlers serves the pages and callbacks of the Facebook login flow.
type Handlers struct {
	login  *app.LoginService
	issuer CodeIssuer
	opts   HandlerOptions
	logger *slog.Logger
}

// NewHandlers wires the Meta HTTP surface.
func NewHandlers(login *app.LoginService, issuer CodeIssuer, opts HandlerOptions, logger *slog.Logger) *Handlers {
	return &Handlers{login: login, issuer: issuer, opts: opts, logger: logger}
}

// LoginHandler serves GET /meta/login?state=… : the page with the "continue
// with Facebook" button. The state is the one parked by /oauth/authorize.
func (h *Handlers) LoginHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		if state == "" {
			h.renderError(w, http.StatusBadRequest, "Lien incomplet",
				"Cette page doit être ouverte depuis votre client MCP.", "")
			return
		}
		h.render(w, http.StatusOK, web.PageLogin, web.LoginData{
			Title:        "Connecter votre compte Facebook",
			AuthorizeURL: template.URL(h.login.AuthorizeURL(h.opts.RedirectURI, state)),
			PrivacyURL:   h.opts.PublicURL + "/privacy",
		})
	})
}

// CallbackHandler serves GET /meta/callback, the redirect target registered
// in the Meta app. It completes the login and hands an authorization code
// back to the MCP client that started the flow.
func (h *Handlers) CallbackHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		state := q.Get("state")
		if state == "" {
			h.renderError(w, http.StatusBadRequest, "Connexion impossible",
				"Le paramètre state est absent.", "")
			return
		}

		// The state is single use: consuming it here closes the CSRF window
		// whatever happens next.
		login, err := h.login.ConsumeState(r.Context(), state)
		if errors.Is(err, domain.ErrNotFound) {
			h.renderError(w, http.StatusBadRequest, "Session expirée",
				"La demande de connexion a expiré ou a déjà été utilisée.",
				"Relancez la connexion depuis votre client MCP.")
			return
		}
		if err != nil {
			h.logger.Error("lecture du state de login", "error", err)
			h.renderError(w, http.StatusInternalServerError, "Erreur interne",
				"Impossible de retrouver votre demande de connexion.", "")
			return
		}

		// A reconnection link carries no MCP client: the flow ends on a
		// confirmation page instead of a redirect.
		isReconnect := login.Request.ClientID == ""

		// The user declined the Facebook dialog.
		if fbErr := q.Get("error"); fbErr != "" {
			h.logger.Info("autorisation Facebook refusée", "reason", q.Get("error_reason"))
			if isReconnect {
				h.renderError(w, http.StatusOK, "Autorisation refusée",
					"Vous avez refusé l'accès à Facebook, rien n'a été modifié.", "")
				return
			}
			h.redirectToClient(w, r, login.Request, map[string]string{
				"error":             "access_denied",
				"error_description": "autorisation Facebook refusée",
			})
			return
		}

		code := q.Get("code")
		if code == "" {
			h.renderError(w, http.StatusBadRequest, "Connexion impossible",
				"Facebook n'a renvoyé aucun code d'autorisation.", "")
			return
		}

		result, err := h.login.Complete(r.Context(), code, h.opts.RedirectURI)
		if errors.Is(err, domain.ErrForbiddenUser) {
			h.renderError(w, http.StatusForbidden, "Compte non autorisé",
				domain.ErrForbiddenUser.Error(),
				"Demandez à l'administrateur d'ajouter votre compte.")
			return
		}
		if err != nil {
			h.logger.Error("finalisation du login Meta", "error", err)
			h.renderError(w, http.StatusBadGateway, "Connexion impossible",
				"Meta a refusé la demande de connexion.", userDetail(err))
			return
		}
		h.logger.Info("tenant connecté", "tenant_id", result.TenantID, "pages", result.Pages)

		if isReconnect {
			h.render(w, http.StatusOK, web.PageReconnected, web.ReconnectedData{
				Title:       "Compte reconnecté",
				DisplayName: result.DisplayName,
				Pages:       result.Pages,
			})
			return
		}

		target, err := h.issuer.IssueAuthCode(r, login.Request, result.TenantID)
		if err != nil {
			h.logger.Error("émission du code d'autorisation", "error", err)
			h.renderError(w, http.StatusInternalServerError, "Erreur interne",
				"Impossible de finaliser l'autorisation.", "")
			return
		}
		http.Redirect(w, r, target, http.StatusFound)
	})
}

// PrivacyHandler serves the static privacy policy the Meta app review needs.
func (h *Handlers) PrivacyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.render(w, http.StatusOK, web.PagePrivacy, web.PrivacyData{Title: "Politique de confidentialité"})
	})
}

// DeauthorizeHandler serves the page Meta links to after a data deletion
// request.
func (h *Handlers) DeauthorizeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.render(w, http.StatusOK, web.PageDeauthorize, web.DeauthorizeData{
			Title:            "Suppression des données",
			ConfirmationCode: r.URL.Query().Get("code"),
		})
	})
}

// deletionResponse is what Meta expects from the data deletion callback.
type deletionResponse struct {
	URL              string `json:"url"`
	ConfirmationCode string `json:"confirmation_code"`
}

// DataDeletionHandler serves POST /meta/data-deletion. Meta signs the request
// with the app secret; an unsigned or badly signed request is rejected.
func (h *Handlers) DataDeletionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "requête illisible", http.StatusBadRequest)
			return
		}
		payload, err := h.parseSignedRequest(r.PostFormValue("signed_request"))
		if err != nil {
			h.logger.Warn("signed_request invalide", "error", err)
			http.Error(w, "signed_request invalide", http.StatusBadRequest)
			return
		}
		if payload.UserID == "" {
			http.Error(w, "signed_request sans user_id", http.StatusBadRequest)
			return
		}

		if err := h.login.DeleteByMetaUserID(r.Context(), payload.UserID); err != nil {
			h.logger.Error("suppression des données", "error", err)
			http.Error(w, "suppression impossible", http.StatusInternalServerError)
			return
		}
		h.logger.Info("données supprimées à la demande de Meta")

		code := confirmationCode()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(deletionResponse{
			URL:              h.opts.PublicURL + "/meta/deauthorize?code=" + url.QueryEscape(code),
			ConfirmationCode: code,
		})
	})
}

// signedRequestPayload is the JSON Meta signs.
type signedRequestPayload struct {
	Algorithm string `json:"algorithm"`
	IssuedAt  int64  `json:"issued_at"`
	UserID    string `json:"user_id"`
}

// parseSignedRequest verifies the HMAC-SHA256 signature of a Meta
// signed_request and returns its payload.
func (h *Handlers) parseSignedRequest(raw string) (*signedRequestPayload, error) {
	encodedSig, encodedPayload, ok := strings.Cut(raw, ".")
	if !ok || encodedSig == "" || encodedPayload == "" {
		return nil, errors.New("format attendu: signature.payload")
	}
	sig, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(encodedSig, "="))
	if err != nil {
		return nil, errors.New("signature illisible")
	}
	rawPayload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(encodedPayload, "="))
	if err != nil {
		return nil, errors.New("payload illisible")
	}

	mac := hmac.New(sha256.New, []byte(h.opts.AppSecret))
	mac.Write([]byte(encodedPayload))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, errors.New("signature invalide")
	}

	var payload signedRequestPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, errors.New("payload JSON invalide")
	}
	if payload.Algorithm != "" && !strings.EqualFold(payload.Algorithm, "HMAC-SHA256") {
		return nil, errors.New("algorithme non supporté")
	}
	return &payload, nil
}

// confirmationCode is the reference Meta shows the user for their deletion
// request. It is informational only.
func confirmationCode() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// redirectToClient sends the MCP client back to its redirect_uri with the
// given parameters, preserving the client state.
func (h *Handlers) redirectToClient(w http.ResponseWriter, r *http.Request, req domain.OAuthRequest, params map[string]string) {
	u, err := url.Parse(req.RedirectURI)
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Connexion impossible",
			"L'adresse de retour du client MCP est invalide.", "")
		return
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	if req.ClientState != "" {
		q.Set("state", req.ClientState)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (h *Handlers) render(w http.ResponseWriter, status int, page string, data any) {
	if err := web.Render(w, status, page, data); err != nil {
		h.logger.Error("rendu HTML", "page", page, "error", err)
	}
}

func (h *Handlers) renderError(w http.ResponseWriter, status int, title, message, detail string) {
	h.render(w, status, web.PageError, web.ErrorData{Title: title, Message: message, Detail: detail})
}

// userDetail turns a Graph failure into a sentence the end user can act on,
// without leaking anything sensitive.
func userDetail(err error) string {
	if ge, ok := domain.AsGraphError(err); ok {
		return ge.UserMessage()
	}
	return ""
}
