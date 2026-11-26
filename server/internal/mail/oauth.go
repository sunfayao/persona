package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	netmail "net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-sasl"
)

const AuthOAuth2 = "oauth2"

var (
	googleTokenURL = "https://oauth2.googleapis.com/token"
	httpClient     = &http.Client{Timeout: networkTimeout}
)

// OAuthCreds is the long-lived credential of an OAuth account, stored encrypted.
type OAuthCreds struct {
	// Provider is whose sign-in it is: "" Google, "microsoft" (microsoft.go).
	Provider     string `json:"provider,omitempty"`
	ClientID     string `json:"client_id"`
	RefreshToken string `json:"refresh_token"`
}

// accessCache holds one refresh token's current access token.
type accessCache struct {
	mu      sync.Mutex
	token   string
	expires time.Time
}

// tokenResponse is the subset of Google's token endpoint reply we use.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (s *Service) googleToken(ctx context.Context, form url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.googleHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oauth: read token response: %w", err)
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("oauth: parse token response (HTTP %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		if tr.Error == "invalid_grant" && form.Get("grant_type") == "refresh_token" {
			return nil, errGoogleRevoked
		}
		msg := tr.Error
		if tr.ErrorDesc != "" {
			msg += ": " + tr.ErrorDesc
		}
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		return nil, fmt.Errorf("oauth: token endpoint returned HTTP %d: %s", resp.StatusCode, msg)
	}
	return &tr, nil
}

func (s *Service) accessEntry(creds *OAuthCreds) *accessCache {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accessEntryLocked(creds)
}

func (s *Service) accessEntryLocked(creds *OAuthCreds) *accessCache {
	c := s.access[creds.RefreshToken]
	if c == nil {
		c = &accessCache{}
		s.access[creds.RefreshToken] = c
	}
	return c
}

func (s *Service) storeAccess(creds *OAuthCreds, token string, expiresIn int) {
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	c := s.accessEntry(creds)
	c.mu.Lock()
	c.token, c.expires = token, s.now().Add(time.Duration(expiresIn)*time.Second)
	c.mu.Unlock()
}

// accessToken returns a valid access token, refreshing it when it expires within a minute.
func (s *Service) accessToken(ctx context.Context, id string, creds *OAuthCreds) (string, error) {
	if creds == nil || creds.RefreshToken == "" {
		return "", errors.New("mail: oauth2 account without credentials")
	}
	if s.refreshAccess != nil {
		return s.refreshAccess(creds)
	}
	// Operations own detached credentials. A read queued before a rotation uses
	// the latest committed token and its cache when it finally authenticates.
	s.mu.Lock()
	if a := s.accts[id]; a != nil && a.OAuth != nil {
		creds = a.clone().OAuth
	}
	c := s.accessEntryLocked(creds)
	s.mu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && s.now().Add(time.Minute).Before(c.expires) {
		return c.token, nil
	}
	form := url.Values{
		"client_id":     {creds.ClientID},
		"refresh_token": {creds.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	var tr *tokenResponse
	var err error
	if creds.Provider == oauthMicrosoft {
		form.Set("scope", microsoftScopes)
		tr, err = s.microsoftToken(ctx, form)
	} else {
		tr, err = s.googleToken(ctx, form)
	}
	if err != nil {
		return "", err
	}
	// Microsoft hands out a new refresh token with every refresh (the old one lapses): keep it.
	if tr.RefreshToken != "" && tr.RefreshToken != creds.RefreshToken {
		s.rotateRefresh(id, creds, tr.RefreshToken, c)
	}
	expiresIn := tr.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	c.token, c.expires = tr.AccessToken, s.now().Add(time.Duration(expiresIn)*time.Second)
	return c.token, nil
}

// rotateRefresh replaces an account's refresh token (and the key of its access cache) and saves the accounts.
func (s *Service) rotateRefresh(id string, creds *OAuthCreds, refresh string, c *accessCache) {
	s.accountMu.Lock()
	defer s.accountMu.Unlock()
	s.mu.Lock()
	var updated *MailAccount
	if a := s.accts[id]; a != nil && a.OAuth != nil && *a.OAuth == *creds {
		updated = a.clone()
		updated.OAuth.RefreshToken = refresh
	}
	s.mu.Unlock()
	publish := func() {
		delete(s.access, creds.RefreshToken)
		creds.RefreshToken = refresh
		s.access[refresh] = c
	}
	// Verification can rotate credentials before the account has been committed.
	// A stale refresh after deletion/re-authorization must not recreate that account.
	if updated != nil {
		if err := s.commitAccount(updated.ID, updated, false, publish); err != nil {
			logf("mail: save the rotated refresh token: %v", err)
			return
		}
		return
	}
	s.mu.Lock()
	publish()
	s.mu.Unlock()
}

// xoauth2Payload is the SASL XOAUTH2 initial response: "user=<email>^Aauth=Bearer <token>^A^A".
func xoauth2Payload(user, token string) []byte {
	return []byte("user=" + user + "\x01auth=Bearer " + token + "\x01\x01")
}

// xoauth2Client implements SASL XOAUTH2 for IMAP AUTHENTICATE and SMTP AUTH.
type xoauth2Client struct {
	user, token string
	done        bool
}

func (c *xoauth2Client) Start() (string, []byte, error) {
	return "XOAUTH2", xoauth2Payload(c.user, c.token), nil
}

// Next answers the server's error challenge (a JSON status) with an empty response, which makes
// the server finish with a normal NO / 535 carrying the reason.
func (c *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	if c.done {
		return nil, errors.New("mail: XOAUTH2 server sent too many challenges")
	}
	c.done = true
	return []byte{}, nil
}

func newXOAUTH2Client(user, token string) sasl.Client {
	return &xoauth2Client{user: user, token: token}
}

// gmailAPIBase is the Gmail API. OAuth Gmail accounts send through it rather than SMTP: some hub machines cannot
// reach smtp.gmail.com on 465 or 587, while HTTPS to Google works, and https://mail.google.com/ covers both.
var gmailAPIBase = "https://gmail.googleapis.com"

func (s *Service) gmailAPI(ctx context.Context, a *MailAccount, method, path string, body any, out any) error {
	token, err := s.oauthToken(ctx, a)
	if err != nil {
		return err
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, gmailAPIBase+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.googleHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("gmail api: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("gmail api: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := strings.TrimSpace(string(data))
		if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		return fmt.Errorf("gmail api: HTTP %d: %s", resp.StatusCode, msg)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// gmailAPICheck stands in for the SMTP check of OAuth Gmail accounts: the token works for the Gmail API
// and belongs to the account's address.
func (s *Service) gmailAPICheck(ctx context.Context, a *MailAccount) error {
	var p struct {
		EmailAddress string `json:"emailAddress"`
	}
	if err := s.gmailAPI(ctx, a, http.MethodGet, "/gmail/v1/users/me/profile", nil, &p); err != nil {
		return err
	}
	if !strings.EqualFold(p.EmailAddress, a.Email) {
		return fmt.Errorf("gmail api: token belongs to %s, not %s", p.EmailAddress, a.Email)
	}
	return nil
}

// gmailAPISend sends a rendered message. Gmail delivers to the Bcc header and strips it, and files the message
// in Sent itself.
func (s *Service) gmailAPISend(ctx context.Context, a *MailAccount, bcc []Address, raw []byte) error {
	if len(bcc) > 0 {
		list := make([]string, 0, len(bcc))
		for _, b := range bcc {
			list = append(list, (&netmail.Address{Name: b.Name, Address: strings.TrimSpace(b.Email)}).String())
		}
		raw = append([]byte("Bcc: "+strings.Join(list, ", ")+"\r\n"), raw...)
	}
	body := map[string]string{"raw": base64.RawURLEncoding.EncodeToString(raw)}
	return s.gmailAPI(ctx, a, http.MethodPost, "/gmail/v1/users/me/messages/send", body, nil)
}
