// Package auth signs users in with GitHub or Google and issues the session
// cookie that identifies them on later requests. It is only used in hosted
// mode - local mode has no accounts at all.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	ghendpoint "golang.org/x/oauth2/github"

	"github.com/you/coc-progress/internal/feature"
)

// User is the identity behind a request.
type User struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Avatar string `json:"avatar"`
	Role   string `json:"role"`
}

// Store is the persistence auth needs. *postgres.Store satisfies this
// structurally; auth never imports the storage package, so it stays usable
// against anything with this shape.
type Store interface {
	UpsertUser(ctx context.Context, provider, providerID, email, name, avatarURL string) (userID int64, err error)
	CreateSession(ctx context.Context, userID int64, tokenHash []byte, expiresAt time.Time) error
	UserBySessionToken(ctx context.Context, tokenHash []byte) (userID int64, email, name, avatarURL, role string, err error)
	DeleteSessionByToken(ctx context.Context, tokenHash []byte) error
	// SetRole overwrites a user's role - used here only to bootstrap
	// adminEmail to feature.RoleAdmin on sign-in. Promoting or demoting
	// anyone else is the admin board's job (internal/server/admin.go), not
	// auth's.
	SetRole(ctx context.Context, userID int64, role string) error
}

const (
	sessionCookieName = "session"
	stateCookieName   = "oauth_state"
	sessionLifetime   = 30 * 24 * time.Hour
	stateLifetime     = 10 * time.Minute
)

type providerUser struct {
	id, email, name, avatar string
}

type provider struct {
	config    *oauth2.Config
	fetchUser func(ctx context.Context, client *http.Client) (providerUser, error)
}

// Service wires OAuth against whichever of GitHub and Google have
// credentials configured, plus session issuing and verification.
type Service struct {
	store      Store
	secure     bool // false only for a plain-http baseURL, so local https-less dev still works
	providers  map[string]provider
	adminEmail string
}

// New builds a Service. Any provider whose ID or secret is empty is simply
// left out of Providers() rather than erroring - a deployment can enable
// just one of the two. adminEmail, if set, is promoted to feature.RoleAdmin
// the moment it signs in - see Callback.
func New(st Store, baseURL, githubID, githubSecret, googleID, googleSecret, adminEmail string) *Service {
	s := &Service{store: st, secure: len(baseURL) >= 8 && baseURL[:8] == "https://", providers: map[string]provider{}, adminEmail: adminEmail}

	if githubID != "" && githubSecret != "" {
		s.providers["github"] = provider{
			config: &oauth2.Config{
				ClientID:     githubID,
				ClientSecret: githubSecret,
				Endpoint:     ghendpoint.Endpoint,
				RedirectURL:  baseURL + "/api/auth/github/callback",
				Scopes:       []string{"read:user", "user:email"},
			},
			fetchUser: fetchGitHubUser,
		}
	}
	if googleID != "" && googleSecret != "" {
		s.providers["google"] = provider{
			config: &oauth2.Config{
				ClientID:     googleID,
				ClientSecret: googleSecret,
				// Hardcoded rather than importing golang.org/x/oauth2/google,
				// which pulls in the whole GCP SDK for two URL constants.
				Endpoint: oauth2.Endpoint{
					AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
					TokenURL: "https://oauth2.googleapis.com/token",
				},
				RedirectURL: baseURL + "/api/auth/google/callback",
				Scopes:      []string{"openid", "email", "profile"},
			},
			fetchUser: fetchGoogleUser,
		}
	}
	return s
}

// Providers lists the configured provider names, sorted, for the frontend
// to render sign-in buttons for.
func (s *Service) Providers() []string {
	out := make([]string, 0, len(s.providers))
	for name := range s.providers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Start redirects to the provider's consent screen, after setting a
// short-lived random state cookie used to detect CSRF on the callback.
func (s *Service) Start(w http.ResponseWriter, r *http.Request, providerName string) error {
	p, ok := s.providers[providerName]
	if !ok {
		return fmt.Errorf("unknown or unconfigured provider %q", providerName)
	}
	state, err := randomToken(24)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: stateCookieName, Value: state, Path: "/", HttpOnly: true, Secure: s.secure,
		SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(stateLifetime),
	})
	http.Redirect(w, r, p.config.AuthCodeURL(state), http.StatusFound)
	return nil
}

// Callback exchanges the authorization code, fetches the profile, upserts
// the user and issues a session cookie.
func (s *Service) Callback(w http.ResponseWriter, r *http.Request, providerName string) (*User, error) {
	p, ok := s.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("unknown or unconfigured provider %q", providerName)
	}

	cookie, err := r.Cookie(stateCookieName)
	if err != nil || cookie.Value == "" || cookie.Value != r.URL.Query().Get("state") {
		return nil, errors.New("state mismatch - the sign-in link may have expired, try again")
	}
	clearCookie(w, stateCookieName)

	code := r.URL.Query().Get("code")
	if code == "" {
		return nil, errors.New("no authorization code returned")
	}
	tok, err := p.config.Exchange(r.Context(), code)
	if err != nil {
		return nil, fmt.Errorf("exchange: %w", err)
	}

	pu, err := p.fetchUser(r.Context(), p.config.Client(r.Context(), tok))
	if err != nil {
		return nil, fmt.Errorf("fetch profile: %w", err)
	}
	if pu.id == "" {
		return nil, errors.New("provider did not return an account id")
	}

	uid, err := s.store.UpsertUser(r.Context(), providerName, pu.id, pu.email, pu.name, pu.avatar)
	if err != nil {
		return nil, fmt.Errorf("save user: %w", err)
	}
	// The only way anyone ever becomes admin: their email matches the
	// configured ADMIN_EMAIL at sign-in time. There is no other path yet -
	// see internal/server/admin.go for how an admin promotes anyone else.
	if isAdminEmail(s.adminEmail, pu.email) {
		if err := s.store.SetRole(r.Context(), uid, feature.RoleAdmin); err != nil {
			return nil, fmt.Errorf("bootstrap admin role: %w", err)
		}
	}
	if err := s.issueSession(w, r.Context(), uid); err != nil {
		return nil, fmt.Errorf("issue session: %w", err)
	}
	return &User{ID: uid, Name: pu.name, Email: pu.email, Avatar: pu.avatar}, nil
}

// isAdminEmail is its own function so the matching rule (case-insensitive,
// both sides required) is unit-testable without faking a whole OAuth round
// trip through Callback.
func isAdminEmail(configuredAdminEmail, userEmail string) bool {
	return configuredAdminEmail != "" && userEmail != "" && strings.EqualFold(configuredAdminEmail, userEmail)
}

func (s *Service) issueSession(w http.ResponseWriter, ctx context.Context, userID int64) error {
	raw, err := randomToken(32)
	if err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(raw))
	expires := time.Now().Add(sessionLifetime)
	if err := s.store.CreateSession(ctx, userID, hash[:], expires); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: raw, Path: "/", HttpOnly: true, Secure: s.secure,
		SameSite: http.SameSiteLaxMode, Expires: expires,
	})
	return nil
}

// User returns the signed-in user for this request, or nil. An absent,
// malformed or expired session is not an error - it just means "not signed
// in", which every caller needs to handle anyway.
func (s *Service) User(r *http.Request) *User {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	hash := sha256.Sum256([]byte(cookie.Value))
	uid, email, name, avatar, role, err := s.store.UserBySessionToken(r.Context(), hash[:])
	if err != nil || uid == 0 {
		return nil
	}
	return &User{ID: uid, Email: email, Name: name, Avatar: avatar, Role: role}
}

// Logout revokes the current session, if any, and clears its cookie.
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		hash := sha256.Sum256([]byte(cookie.Value))
		s.store.DeleteSessionByToken(r.Context(), hash[:])
	}
	clearCookie(w, sessionCookieName)
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1})
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func fetchGitHubUser(ctx context.Context, client *http.Client) (providerUser, error) {
	var profile struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := getJSON(ctx, client, "https://api.github.com/user", &profile); err != nil {
		return providerUser{}, err
	}
	name := profile.Name
	if name == "" {
		name = profile.Login
	}
	email := profile.Email
	if email == "" {
		// A private primary email needs the dedicated emails endpoint.
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err := getJSON(ctx, client, "https://api.github.com/user/emails", &emails); err == nil {
			for _, e := range emails {
				if e.Primary && e.Verified {
					email = e.Email
					break
				}
			}
		}
	}
	return providerUser{id: strconv.FormatInt(profile.ID, 10), email: email, name: name, avatar: profile.AvatarURL}, nil
}

func fetchGoogleUser(ctx context.Context, client *http.Client) (providerUser, error) {
	var profile struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := getJSON(ctx, client, "https://www.googleapis.com/oauth2/v2/userinfo", &profile); err != nil {
		return providerUser{}, err
	}
	return providerUser{id: profile.ID, email: profile.Email, name: profile.Name, avatar: profile.Picture}, nil
}

func getJSON(ctx context.Context, client *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return fmt.Errorf("%s: %d %s", url, resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
