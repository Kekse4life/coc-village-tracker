package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/you/coc-progress/internal/auth"
	"github.com/you/coc-progress/internal/store/postgres"
)

// These hit a real Postgres and are skipped unless TEST_DATABASE_URL is set,
// matching internal/store/postgres's own tests:
//
//	TEST_DATABASE_URL=postgres://postgres:test@localhost:15432/cocprogress?sslmode=disable go test ./internal/server/... -run Hosted -v
//
// Running this alongside internal/store/postgres's tests via `go test ./...`
// needs -p 1 - see the note on that package's testStore for why.
func hostedTestPool(t *testing.T) *postgres.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pg, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		pg.Pool().Exec(context.Background(), "TRUNCATE users, sessions, villages, snapshots CASCADE")
		pg.Close()
	})
	return pg
}

var signedInRequestCounter int

// signedInRequest builds a request carrying a real, database-backed session
// cookie for a fresh user - the same shape a browser presents after a
// completed OAuth round trip, without needing a live GitHub/Google account.
// Each call creates a distinct user, even within the same test.
func signedInRequest(t *testing.T, pg *postgres.Store, method, target, body string) *http.Request {
	t.Helper()
	signedInRequestCounter++
	ctx := context.Background()
	providerID := fmt.Sprintf("%s-%d", t.Name(), signedInRequestCounter)
	uid, err := pg.UpsertUser(ctx, "github", providerID, "user@example.com", "Test User", "")
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	if err := pg.CreateSession(ctx, uid, hash[:], time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}

	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	return req
}

func hostedMux(pg *postgres.Store) *http.ServeMux {
	mux := http.NewServeMux()
	authSvc := auth.New(pg, "https://coc-progress.example.com", "gh-id", "gh-secret", "", "")
	New(Config{Catalog: testCatalog(), Store: pg, Auth: authSvc, Hosted: true, BaseURL: "https://coc-progress.example.com", CronSecret: "test-secret"}, mux)
	return mux
}

func TestHostedPostThenGetRoundTrips(t *testing.T) {
	pg := hostedTestPool(t)
	mux := hostedMux(pg)

	postReq := signedInRequest(t, pg, http.MethodPost, "/api/report", validExport)
	postRec := httptest.NewRecorder()
	mux.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body = %s", postRec.Code, postRec.Body.String())
	}

	// Reuse the SAME cookie for the GET - it must resolve to the village
	// that cookie's user just saved, with no tag specified.
	getReq := httptest.NewRequest(http.MethodGet, "/api/report", nil)
	getReq.AddCookie(postReq.Cookies()[0])
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	var rep map[string]any
	json.Unmarshal(getRec.Body.Bytes(), &rep)
	if rep["tag"] != "#TEST" {
		t.Errorf("tag = %v, want #TEST", rep["tag"])
	}
}

func TestHostedUsersCannotSeeEachOthersVillages(t *testing.T) {
	pg := hostedTestPool(t)
	mux := hostedMux(pg)

	aReq := signedInRequest(t, pg, http.MethodPost, "/api/report", validExport)
	aRec := httptest.NewRecorder()
	mux.ServeHTTP(aRec, aReq)
	if aRec.Code != http.StatusOK {
		t.Fatalf("user A post: %d %s", aRec.Code, aRec.Body.String())
	}

	// A different signed-in user, with no village of their own, must get 404 - never user A's data.
	bReq := signedInRequest(t, pg, http.MethodGet, "/api/report", "")
	bRec := httptest.NewRecorder()
	mux.ServeHTTP(bRec, bReq)
	if bRec.Code != http.StatusNotFound {
		t.Errorf("user B GET status = %d, want 404 (must not see user A's village)", bRec.Code)
	}
}

func TestHostedVillageQuotaRejectsSixthVillage(t *testing.T) {
	pg := hostedTestPool(t)
	mux := hostedMux(pg)

	uid, _ := pg.UpsertUser(context.Background(), "github", t.Name(), "", "", "")
	raw := make([]byte, 32)
	rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	pg.CreateSession(context.Background(), uid, hash[:], time.Now().Add(time.Hour))
	cookie := &http.Cookie{Name: "session", Value: token}

	post := func(tag string) int {
		body := `{"tag":"` + tag + `","timestamp":1700000000,"buildings":[{"data":1000001,"lvl":1,"cnt":1}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader(body))
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	for i, tag := range []string{"#V1", "#V2", "#V3", "#V4", "#V5"} {
		if code := post(tag); code != http.StatusOK {
			t.Fatalf("village %d (%s): status = %d, want 200", i+1, tag, code)
		}
	}
	if code := post("#V6"); code != http.StatusTooManyRequests {
		t.Errorf("6th village: status = %d, want 429 (5-village limit)", code)
	}
}

func TestHostedCronPruneRequiresSecret(t *testing.T) {
	pg := hostedTestPool(t)
	mux := hostedMux(pg)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/cron/prune", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no bearer token: status = %d, want 401", rec.Code)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/cron/prune", nil)
	req2.Header.Set("Authorization", "Bearer wrong-secret")
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("wrong bearer token: status = %d, want 401", rec2.Code)
	}

	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/cron/prune", nil)
	req3.Header.Set("Authorization", "Bearer test-secret")
	mux.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Errorf("correct bearer token: status = %d, want 200, body = %s", rec3.Code, rec3.Body.String())
	}
}

func TestHostedLogoutRevokesSession(t *testing.T) {
	pg := hostedTestPool(t)
	mux := hostedMux(pg)

	req := signedInRequest(t, pg, http.MethodGet, "/api/me", "")
	cookie := req.Cookies()[0]

	meRec := httptest.NewRecorder()
	mux.ServeHTTP(meRec, req)
	var me map[string]any
	json.Unmarshal(meRec.Body.Bytes(), &me)
	if me["user"] == nil {
		t.Fatalf("expected a signed-in user before logout, got %v", me)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutReq.AddCookie(cookie)
	mux.ServeHTTP(httptest.NewRecorder(), logoutReq)

	afterReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	afterReq.AddCookie(cookie)
	afterRec := httptest.NewRecorder()
	mux.ServeHTTP(afterRec, afterReq)
	var after map[string]any
	json.Unmarshal(afterRec.Body.Bytes(), &after)
	if after["user"] != nil {
		t.Errorf("user should be nil after logout, got %v", after["user"])
	}
}
