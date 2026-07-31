package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeStore is a tiny in-memory Store for testing everything that does not
// require a live OAuth provider: session issuing, verification, expiry and
// logout, and the state-mismatch CSRF guard on Callback.
type fakeStore struct {
	nextID   int64
	users    map[string]int64 // "provider|providerID" -> userID
	sessions map[string]struct {
		userID  int64
		expires time.Time
	}
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users: map[string]int64{},
		sessions: map[string]struct {
			userID  int64
			expires time.Time
		}{},
	}
}

func (f *fakeStore) UpsertUser(_ context.Context, provider, providerID, _, _, _ string) (int64, error) {
	key := provider + "|" + providerID
	if id, ok := f.users[key]; ok {
		return id, nil
	}
	f.nextID++
	f.users[key] = f.nextID
	return f.nextID, nil
}

func (f *fakeStore) CreateSession(_ context.Context, userID int64, tokenHash []byte, expiresAt time.Time) error {
	f.sessions[string(tokenHash)] = struct {
		userID  int64
		expires time.Time
	}{userID, expiresAt}
	return nil
}

func (f *fakeStore) UserBySessionToken(_ context.Context, tokenHash []byte) (int64, string, string, string, error) {
	s, ok := f.sessions[string(tokenHash)]
	if !ok || time.Now().After(s.expires) {
		return 0, "", "", "", nil
	}
	return s.userID, "user@example.com", "Test User", "", nil
}

func (f *fakeStore) DeleteSessionByToken(_ context.Context, tokenHash []byte) error {
	delete(f.sessions, string(tokenHash))
	return nil
}

func TestIssueSessionThenUserRoundTrips(t *testing.T) {
	s := &Service{store: newFakeStore(), secure: true}
	rec := httptest.NewRecorder()

	if err := s.issueSession(rec, context.Background(), 42); err != nil {
		t.Fatalf("issueSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}

	u := s.User(req)
	if u == nil {
		t.Fatal("User() = nil, want the session's user")
	}
	if u.ID != 42 {
		t.Errorf("user ID = %d, want 42", u.ID)
	}
}

func TestUserWithNoCookieIsNilNotError(t *testing.T) {
	s := &Service{store: newFakeStore(), secure: true}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if u := s.User(req); u != nil {
		t.Errorf("User() = %+v, want nil with no session cookie", u)
	}
}

func TestUserWithForgedCookieIsNil(t *testing.T) {
	s := &Service{store: newFakeStore(), secure: true}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "not-a-real-token"})
	if u := s.User(req); u != nil {
		t.Errorf("User() = %+v, want nil for a token that was never issued", u)
	}
}

func TestLogoutRevokesTheSession(t *testing.T) {
	s := &Service{store: newFakeStore(), secure: true}
	rec := httptest.NewRecorder()
	s.issueSession(rec, context.Background(), 7)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	if s.User(req) == nil {
		t.Fatal("session should be valid before logout")
	}

	logoutRec := httptest.NewRecorder()
	s.Logout(logoutRec, req)

	// The session token itself must be gone server-side, not just the cookie
	// cleared client-side - re-presenting the same cookie must fail too.
	if s.User(req) != nil {
		t.Error("session should be invalid after logout, even resubmitting the old cookie")
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	fs := newFakeStore()
	s := &Service{store: fs, secure: true}
	rec := httptest.NewRecorder()
	s.issueSession(rec, context.Background(), 1)

	// Back-date the session to simulate expiry without waiting 30 days.
	for k, v := range fs.sessions {
		v.expires = time.Now().Add(-time.Hour)
		fs.sessions[k] = v
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	if u := s.User(req); u != nil {
		t.Errorf("User() = %+v, want nil for an expired session", u)
	}
}

func TestCallbackRejectsStateMismatch(t *testing.T) {
	s := New(newFakeStore(), "https://example.com", "id", "secret", "", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback?state=attacker-supplied&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "the-real-state"})

	if _, err := s.Callback(rec, req, "github"); err == nil {
		t.Error("Callback should reject a state that does not match the cookie")
	}
}

func TestCallbackRejectsMissingStateCookie(t *testing.T) {
	s := New(newFakeStore(), "https://example.com", "id", "secret", "", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback?state=whatever&code=abc", nil)

	if _, err := s.Callback(rec, req, "github"); err == nil {
		t.Error("Callback should reject a request with no state cookie at all")
	}
}

func TestProvidersOnlyListsConfiguredOnes(t *testing.T) {
	s := New(newFakeStore(), "https://example.com", "gh-id", "gh-secret", "", "")
	got := s.Providers()
	if len(got) != 1 || got[0] != "github" {
		t.Errorf("providers = %v, want [github] (Google has no credentials)", got)
	}
}

func TestSecureFlagFollowsBaseURLScheme(t *testing.T) {
	s := New(newFakeStore(), "http://localhost:8080", "gh-id", "gh-secret", "", "")
	if s.secure {
		t.Error("secure should be false for a plain-http baseURL, so local dev cookies still work")
	}
	s2 := New(newFakeStore(), "https://coc-progress.example.com", "gh-id", "gh-secret", "", "")
	if !s2.secure {
		t.Error("secure should be true for an https baseURL")
	}
}
