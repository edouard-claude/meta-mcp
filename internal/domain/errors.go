package domain

import (
	"errors"
	"fmt"
)

// ErrNotFound is returned by the stores when a row does not exist, or exists
// but belongs to another tenant. The two cases are deliberately
// indistinguishable so that a tenant cannot probe another tenant's ids.
var ErrNotFound = errors.New("introuvable")

// ErrUnknownPage is what a tool returns when the client passed a page_id the
// authenticated tenant does not own.
var ErrUnknownPage = errors.New("page inconnue pour ce compte")

// ErrNoInstagram is returned when an Instagram tool targets a page with no
// linked Instagram Business or Creator account.
var ErrNoInstagram = errors.New("aucun compte Instagram professionnel n'est lié à cette page")

// ErrReauthorize is what every tool returns once Meta has rejected the stored
// token. The message is the one the end user reads.
var ErrReauthorize = errors.New("autorisation expirée, utilisez reconnect_url")

// ErrForbiddenUser is returned when ALLOWED_META_USER_IDS is set and the
// Facebook account that just logged in is not on the list.
var ErrForbiddenUser = errors.New("ce compte Facebook n'est pas autorisé sur ce serveur")

// GraphError is a decoded error envelope from the Meta Graph API.
type GraphError struct {
	HTTPStatus int
	Code       int
	Subcode    int
	Type       string
	Message    string
	TraceID    string
}

func (e *GraphError) Error() string {
	return fmt.Sprintf("graph api: HTTP %d code=%d subcode=%d type=%s: %s",
		e.HTTPStatus, e.Code, e.Subcode, e.Type, e.Message)
}

// IsAuth reports whether the error means the stored token can no longer be
// used: expired, revoked, or missing a permission. Code 190 covers invalid
// tokens, 10 and 200 cover permission failures, 102 covers session issues.
func (e *GraphError) IsAuth() bool {
	switch e.Code {
	case 190, 102, 10, 200:
		return true
	}
	return e.HTTPStatus == 401
}

// IsRateLimit reports whether Meta is throttling us. Codes 4 and 17 are
// application and user level rate limits, 32 is the page level one, 613 is the
// generic "calls to this api have exceeded the rate limit".
func (e *GraphError) IsRateLimit() bool {
	switch e.Code {
	case 4, 17, 32, 613:
		return true
	}
	return e.HTTPStatus == 429
}

// UserMessage turns a Graph error into the French sentence a tool returns.
func (e *GraphError) UserMessage() string {
	switch {
	case e.IsAuth():
		return ErrReauthorize.Error()
	case e.IsRateLimit():
		return "quota Meta atteint, réessayez dans quelques minutes"
	default:
		return fmt.Sprintf("erreur Meta (code %d) : %s", e.Code, e.Message)
	}
}

// AsGraphError unwraps err into a *GraphError when there is one.
func AsGraphError(err error) (*GraphError, bool) {
	var ge *GraphError
	if errors.As(err, &ge) {
		return ge, true
	}
	return nil, false
}
