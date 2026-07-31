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

// adminRequest is signedInRequest's admin-role counterpart, for testing the
// admin board and role-gated features without a live OAuth account. Returns
// the created user's ID too, since several tests need it to target
// themselves or another user in a request body.
func adminRequest(t *testing.T, pg *postgres.Store, method, target, body string) (*http.Request, int64) {
	t.Helper()
	req := signedInRequest(t, pg, method, target, body)
	hash := sha256.Sum256([]byte(req.Cookies()[0].Value))
	uid, _, _, _, _, err := pg.UserBySessionToken(context.Background(), hash[:])
	if err != nil || uid == 0 {
		t.Fatalf("resolve just-created session: %v", err)
	}
	if err := pg.SetRole(context.Background(), uid, "admin"); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return req, uid
}

func hostedMux(pg *postgres.Store) *http.ServeMux {
	mux := http.NewServeMux()
	authSvc := auth.New(pg, "https://coc-progress.example.com", "gh-id", "gh-secret", "", "", "")
	New(Config{Catalog: testCatalog(), Store: pg, Features: pg, Auth: authSvc, Hosted: true, BaseURL: "https://coc-progress.example.com", CronSecret: "test-secret"}, mux)
	return mux
}

// hostedMuxWithDevLogin is hostedMux's DevLogin-enabled counterpart, with an
// adminEmail so promotion-on-sign-in can be exercised through it too.
func hostedMuxWithDevLogin(pg *postgres.Store, adminEmail string) *http.ServeMux {
	mux := http.NewServeMux()
	authSvc := auth.New(pg, "https://coc-progress.example.com", "gh-id", "gh-secret", "", "", adminEmail)
	New(Config{Catalog: testCatalog(), Store: pg, Features: pg, Auth: authSvc, Hosted: true, BaseURL: "https://coc-progress.example.com", CronSecret: "test-secret", DevLogin: true}, mux)
	return mux
}

// The one point of DevLogin: a real, database-backed session comes out of
// it exactly like a real OAuth callback would, including the ADMIN_EMAIL
// bootstrap - nothing downstream needs to know it wasn't a real provider.
func TestHostedDevLoginCreatesRealSessionAndBootstrapsAdmin(t *testing.T) {
	pg := hostedTestPool(t)
	mux := hostedMuxWithDevLogin(pg, "admin@example.com")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/dev?email=admin@example.com&name=Admin", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302, body = %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie set")
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.AddCookie(cookies[0])
	meRec := httptest.NewRecorder()
	mux.ServeHTTP(meRec, meReq)
	var got map[string]any
	json.Unmarshal(meRec.Body.Bytes(), &got)
	user, _ := got["user"].(map[string]any)
	if user == nil || user["role"] != "admin" {
		t.Errorf("user = %+v, want role=admin (ADMIN_EMAIL match)", got["user"])
	}
}

// The other point of DevLogin: when it's off (the default), it must not
// exist as a reachable path at all, not just refuse politely.
func TestHostedDevLoginNotRegisteredWhenDisabled(t *testing.T) {
	pg := hostedTestPool(t)
	mux := hostedMux(pg)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/dev?email=x@example.com", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when DevLogin is disabled", rec.Code)
	}
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

// Both gated flags seed as admin-required (schema.sql), so a plain
// signed-in user must see neither unlocked.
func TestHostedFeaturesDefaultLockedForPlainUser(t *testing.T) {
	pg := hostedTestPool(t)
	mux := hostedMux(pg)

	req := signedInRequest(t, pg, http.MethodGet, "/api/features", "")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if unlocked, _ := got["unlocked"].([]any); len(unlocked) != 0 {
		t.Errorf("unlocked = %v, want none for a plain user", got["unlocked"])
	}
}

func TestHostedFeaturesUnlockedForAdmin(t *testing.T) {
	pg := hostedTestPool(t)
	mux := hostedMux(pg)

	req, _ := adminRequest(t, pg, http.MethodGet, "/api/features", "")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if unlocked, _ := got["unlocked"].([]any); len(unlocked) != 2 {
		t.Errorf("unlocked = %v, want both themes and build_now for an admin", got["unlocked"])
	}
}

// The end-to-end point of gating Build Now: a plain user (build_now
// defaults to admin-required) must be turned away before ever reaching
// pending.Apply, not just have the feature hidden client-side.
func TestHostedPendingRejectedWithoutBuildNowRole(t *testing.T) {
	pg := hostedTestPool(t)
	mux := hostedMux(pg)

	postReq := signedInRequest(t, pg, http.MethodPost, "/api/report", validExport)
	postRec := httptest.NewRecorder()
	mux.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("seed export: %d %s", postRec.Code, postRec.Body.String())
	}

	pendingReq := httptest.NewRequest(http.MethodPost, "/api/pending", strings.NewReader(`{"itemId":1000001,"village":"home","fromLevel":2}`))
	pendingReq.AddCookie(postReq.Cookies()[0])
	pendingRec := httptest.NewRecorder()
	mux.ServeHTTP(pendingRec, pendingReq)
	if pendingRec.Code != http.StatusForbidden {
		t.Errorf("status = %d, body = %s, want 403 - build_now defaults to admin-required", pendingRec.Code, pendingRec.Body.String())
	}
}

func TestHostedAdminCanListAndPromoteUsers(t *testing.T) {
	pg := hostedTestPool(t)
	mux := hostedMux(pg)

	adminReq, _ := adminRequest(t, pg, http.MethodGet, "/api/admin/users", "")
	plainReq := signedInRequest(t, pg, http.MethodGet, "/api/report", "") // seeds a second, plain user
	mux.ServeHTTP(httptest.NewRecorder(), plainReq)                      // 404 (no village yet) is fine - only the user row matters here

	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, adminReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var got map[string]any
	json.Unmarshal(listRec.Body.Bytes(), &got)
	users, _ := got["users"].([]any)
	if len(users) != 2 {
		t.Fatalf("users = %+v, want 2 (the admin and the plain user)", got["users"])
	}

	var plainUserID float64
	for _, u := range users {
		if m := u.(map[string]any); m["role"] == "user" {
			plainUserID = m["id"].(float64)
		}
	}
	if plainUserID == 0 {
		t.Fatalf("could not find the plain user in %+v", users)
	}

	promoteReq := httptest.NewRequest(http.MethodPost, "/api/admin/users",
		strings.NewReader(fmt.Sprintf(`{"userId":%d,"role":"admin"}`, int64(plainUserID))))
	promoteReq.AddCookie(adminReq.Cookies()[0])
	promoteRec := httptest.NewRecorder()
	mux.ServeHTTP(promoteRec, promoteReq)
	if promoteRec.Code != http.StatusOK {
		t.Fatalf("promote status = %d, body = %s", promoteRec.Code, promoteRec.Body.String())
	}
	var got2 map[string]any
	json.Unmarshal(promoteRec.Body.Bytes(), &got2)
	users2, _ := got2["users"].([]any)
	admins := 0
	for _, u := range users2 {
		if u.(map[string]any)["role"] == "admin" {
			admins++
		}
	}
	if admins != 2 {
		t.Errorf("admins after promotion = %d, want 2", admins)
	}
}

func TestHostedNonAdminCannotAccessAdminBoard(t *testing.T) {
	pg := hostedTestPool(t)
	mux := hostedMux(pg)

	req := signedInRequest(t, pg, http.MethodGet, "/api/admin/users", "")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a signed-in non-admin", rec.Code)
	}

	anonRec := httptest.NewRecorder()
	mux.ServeHTTP(anonRec, httptest.NewRequest(http.MethodGet, "/api/admin/users", nil))
	if anonRec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 with no session at all", anonRec.Code)
	}
}

// The one accident this guard exists for: the sole admin locking everyone
// out of the board by demoting themself.
func TestHostedAdminCannotSelfDemoteAsSoleAdmin(t *testing.T) {
	pg := hostedTestPool(t)
	mux := hostedMux(pg)

	seedReq, adminID := adminRequest(t, pg, http.MethodGet, "/api/admin/users", "")
	demoteReq := httptest.NewRequest(http.MethodPost, "/api/admin/users",
		strings.NewReader(fmt.Sprintf(`{"userId":%d,"role":"user"}`, adminID)))
	demoteReq.AddCookie(seedReq.Cookies()[0])
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, demoteReq)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, body = %s, want 409 - the sole admin must not be able to demote themself", rec.Code, rec.Body.String())
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
