package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/you/coc-progress/internal/auth"
	"github.com/you/coc-progress/internal/catalog"
	"github.com/you/coc-progress/internal/store/file"
)

// noProviderAuth builds an auth.Service with no OAuth credentials and no
// backing store - safe for tests that never present a session cookie, since
// Service.User returns nil on a missing cookie before ever touching the store.
func noProviderAuth() *auth.Service {
	return auth.New(nil, "https://coc-progress.example.com", "", "", "", "")
}

func testCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		GeneratedAt: "2026-01-01T00:00:00Z",
		Entries: map[string]catalog.Entry{
			"1000001": {Name: "Town Hall", Kind: "building", Class: "Town Hall", MaxLevel: 3,
				Levels: []catalog.Level{{Requires: map[string]int{"th": 1}}, {Requires: map[string]int{"th": 2}}, {Requires: map[string]int{"th": 3}}}},
			"1000008": {Name: "Cannon", Kind: "building", Class: "Defense", Resource: "Gold", MaxLevel: 3, Levels: []catalog.Level{
				{Requires: map[string]int{"th": 1}, Cost: 250, Seconds: 10},
				{Requires: map[string]int{"th": 2}, Cost: 1000, Seconds: 60},
				{Requires: map[string]int{"th": 3}, Cost: 2000, Seconds: 600},
			}},
		},
	}
}

const validExport = `{"tag":"#TEST","timestamp":1700000000,"buildings":[{"data":1000001,"lvl":2,"cnt":1}]}`

func newLocalMux(t *testing.T, historyDir string) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	cfg := Config{Catalog: testCatalog()}
	if historyDir != "" {
		fh, err := file.New(historyDir)
		if err != nil {
			t.Fatalf("file store: %v", err)
		}
		cfg.Store = fh
		cfg.Durable = true
	}
	New(cfg, mux)
	return mux
}

func TestLocalReportGetBeforeAnyPostIs404(t *testing.T) {
	mux := newLocalMux(t, "")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/report", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestLocalPostThenGetRoundTrips(t *testing.T) {
	mux := newLocalMux(t, "")

	postRec := httptest.NewRecorder()
	postReq := httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader(validExport))
	mux.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body = %s", postRec.Code, postRec.Body.String())
	}

	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/api/report", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	var rep map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &rep); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rep["tag"] != "#TEST" {
		t.Errorf("tag = %v, want #TEST", rep["tag"])
	}
}

func TestLocalPostRejectsInvalidJSON(t *testing.T) {
	mux := newLocalMux(t, "")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader("not json")))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestLocalHistoryIsEnabledButNotDurableWithoutHistoryFlag(t *testing.T) {
	mux := newLocalMux(t, "")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["enabled"] != true {
		t.Errorf("enabled = %v, want true - two exports of the same village in one session can still diff without -history", got["enabled"])
	}
	if got["durable"] != false {
		t.Errorf("durable = %v, want false with no -history configured", got["durable"])
	}
}

func TestLocalHistoryIsDurableWithHistoryFlag(t *testing.T) {
	mux := newLocalMux(t, t.TempDir())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["durable"] != true {
		t.Errorf("durable = %v, want true with -history configured", got["durable"])
	}
}

// Local mode holds more than one village at once now, each independently
// addressable by ?tag=, with no accounts involved.
func TestLocalVillagesResolveIndependentlyByTag(t *testing.T) {
	mux := newLocalMux(t, "")
	post := func(body string) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("POST status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}
	post(`{"tag":"#AAA","timestamp":1700000000,"buildings":[{"data":1000001,"lvl":2,"cnt":1}]}`)
	post(`{"tag":"#BBB","timestamp":1700000000,"buildings":[{"data":1000001,"lvl":3,"cnt":1}]}`)

	get := func(tag string) map[string]any {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/report?tag="+tag, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, body = %s", tag, rec.Code, rec.Body.String())
		}
		var rep map[string]any
		json.Unmarshal(rec.Body.Bytes(), &rep)
		return rep
	}
	if rep := get("%23AAA"); rep["tag"] != "#AAA" {
		t.Errorf("tag = %v, want #AAA", rep["tag"])
	}
	if rep := get("%23BBB"); rep["tag"] != "#BBB" {
		t.Errorf("tag = %v, want #BBB", rep["tag"])
	}

	villagesRec := httptest.NewRecorder()
	mux.ServeHTTP(villagesRec, httptest.NewRequest(http.MethodGet, "/api/villages", nil))
	var got map[string]any
	json.Unmarshal(villagesRec.Body.Bytes(), &got)
	villages, _ := got["villages"].([]any)
	if len(villages) != 2 {
		t.Fatalf("villages = %+v, want 2", got["villages"])
	}
}

// An export with no tag at all is legal (snapshot.Parse never requires
// one) and must not become unrecoverable just because the empty string is
// also resolveTag's "caller expressed no preference" signal.
func TestUntaggedExportRoundTrips(t *testing.T) {
	mux := newLocalMux(t, "")
	postRec := httptest.NewRecorder()
	mux.ServeHTTP(postRec, httptest.NewRequest(http.MethodPost, "/api/report",
		strings.NewReader(`{"timestamp":1700000000,"buildings":[{"data":1000001,"lvl":2,"cnt":1}]}`)))
	if postRec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body = %s", postRec.Code, postRec.Body.String())
	}

	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/api/report", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s (an untagged export must still be the default village)", getRec.Code, getRec.Body.String())
	}
}

func TestLocalHistoryReflectsSavedSnapshots(t *testing.T) {
	dir := t.TempDir()
	mux := newLocalMux(t, dir)

	post := func(body string) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("POST status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}
	post(validExport)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["enabled"] != true {
		t.Fatalf("enabled = %v, want true", got["enabled"])
	}
	if got["changeLog"] != nil {
		t.Errorf("changeLog = %v, want nil with only one snapshot saved", got["changeLog"])
	}

	// A second, later export of the same village should produce a change log.
	post(`{"tag":"#TEST","timestamp":1700003600,"buildings":[{"data":1000001,"lvl":3,"cnt":1}]}`)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	var got2 map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &got2)
	cl, ok := got2["changeLog"].(map[string]any)
	if !ok {
		t.Fatalf("changeLog = %v, want a populated change log after two snapshots", got2["changeLog"])
	}
	changes, _ := cl["changes"].([]any)
	if len(changes) != 1 {
		t.Fatalf("changes = %v, want exactly one (Town Hall 2->3 landed)", changes)
	}

	// And the files must actually be on disk, not just in memory.
	entries, _ := os.ReadDir(filepath.Join(dir, "TEST"))
	if len(entries) != 2 {
		t.Errorf("snapshot files on disk = %d, want 2", len(entries))
	}
}

// postJSON is a small helper for the pending-endpoint tests below, which
// all need to POST/DELETE and decode a JSON response.
func postJSON(t *testing.T, mux *http.ServeMux, method, target, body string) (int, map[string]any) {
	t.Helper()
	var r *strings.Reader
	if body != "" {
		r = strings.NewReader(body)
	} else {
		r = strings.NewReader("")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, r))
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	return rec.Code, got
}

// Declaring an upgrade must show up immediately in the POST response, and
// keep showing up on a later plain GET - not just in the instant the
// action was created. Regression coverage for exactly the thing this
// feature exists for: not having to re-export just to see it reflected.
func TestLocalPendingDeclaresAndPersistsAcrossGet(t *testing.T) {
	mux := newLocalMux(t, "")
	postJSON(t, mux, http.MethodPost, "/api/report",
		`{"tag":"#TEST","timestamp":1700000000,"buildings":[{"data":1000001,"lvl":2,"cnt":1},{"data":1000008,"lvl":1,"cnt":1}]}`)

	code, resp := postJSON(t, mux, http.MethodPost, "/api/pending", `{"itemId":1000008,"village":"home","fromLevel":1}`)
	if code != http.StatusOK {
		t.Fatalf("POST /api/pending status = %d, body = %+v", code, resp)
	}
	declared, _ := resp["declared"].([]any)
	if len(declared) != 1 {
		t.Fatalf("declared = %+v, want exactly 1", resp["declared"])
	}
	jobs, _ := resp["jobs"].([]any)
	if len(jobs) != 1 {
		t.Fatalf("jobs = %+v, want exactly 1 (the declared upgrade overlaid as an in-flight job)", resp["jobs"])
	}

	getCode, getResp := postJSON(t, mux, http.MethodGet, "/api/report", "")
	if getCode != http.StatusOK {
		t.Fatalf("GET status = %d, body = %+v", getCode, getResp)
	}
	if declared2, _ := getResp["declared"].([]any); len(declared2) != 1 {
		t.Errorf("a later plain GET lost the declared upgrade: declared = %+v", getResp["declared"])
	}
}

// A claim that does not match anything the current report considers
// startable right now must be rejected outright, not silently accepted -
// the server derives cost/seconds/lane itself and never trusts the client
// on whether the upgrade is even legitimate.
func TestLocalPendingRejectsUnavailableUpgrade(t *testing.T) {
	mux := newLocalMux(t, "")
	postJSON(t, mux, http.MethodPost, "/api/report",
		`{"tag":"#TEST","timestamp":1700000000,"buildings":[{"data":1000001,"lvl":2,"cnt":1},{"data":1000008,"lvl":1,"cnt":1}]}`)

	// Level 5 was never owned, let alone startable.
	code, resp := postJSON(t, mux, http.MethodPost, "/api/pending", `{"itemId":1000008,"village":"home","fromLevel":5}`)
	if code != http.StatusConflict {
		t.Errorf("status = %d, body = %+v, want 409 for an upgrade that isn't actually startable", code, resp)
	}
}

// A second claim on the same already-claimed copy must fail cleanly - it
// is exactly the scenario pending.Apply's idle-copy matching exists to
// catch, since NextUp itself does not know a copy is already spoken for.
func TestLocalPendingRejectsClaimingTheSameCopyTwice(t *testing.T) {
	mux := newLocalMux(t, "")
	postJSON(t, mux, http.MethodPost, "/api/report",
		`{"tag":"#TEST","timestamp":1700000000,"buildings":[{"data":1000001,"lvl":2,"cnt":1},{"data":1000008,"lvl":1,"cnt":1}]}`)

	code1, resp1 := postJSON(t, mux, http.MethodPost, "/api/pending", `{"itemId":1000008,"village":"home","fromLevel":1}`)
	if code1 != http.StatusOK {
		t.Fatalf("first claim: status = %d, body = %+v", code1, resp1)
	}
	code2, resp2 := postJSON(t, mux, http.MethodPost, "/api/pending", `{"itemId":1000008,"village":"home","fromLevel":1}`)
	if code2 != http.StatusConflict {
		t.Errorf("second claim on the sole copy: status = %d, body = %+v, want 409", code2, resp2)
	}
}

// Undoing a declared upgrade must remove it from the overlay entirely -
// the cancelled action's copy goes back to being idle and startable again.
func TestLocalPendingDeleteCancelsDeclaredUpgrade(t *testing.T) {
	mux := newLocalMux(t, "")
	postJSON(t, mux, http.MethodPost, "/api/report",
		`{"tag":"#TEST","timestamp":1700000000,"buildings":[{"data":1000001,"lvl":2,"cnt":1},{"data":1000008,"lvl":1,"cnt":1}]}`)

	_, resp := postJSON(t, mux, http.MethodPost, "/api/pending", `{"itemId":1000008,"village":"home","fromLevel":1}`)
	declared, _ := resp["declared"].([]any)
	if len(declared) != 1 {
		t.Fatalf("declared = %+v, want exactly 1", resp["declared"])
	}
	first, _ := declared[0].(map[string]any)
	id, _ := first["id"].(string)
	if id == "" {
		t.Fatalf("declared action has no id: %+v", first)
	}

	delCode, delResp := postJSON(t, mux, http.MethodDelete, "/api/pending?id="+id, "")
	if delCode != http.StatusOK {
		t.Fatalf("DELETE status = %d, body = %+v", delCode, delResp)
	}
	if declared2, _ := delResp["declared"].([]any); len(declared2) != 0 {
		t.Errorf("declared = %+v after cancelling the only action, want none", delResp["declared"])
	}
	if jobs, _ := delResp["jobs"].([]any); len(jobs) != 0 {
		t.Errorf("jobs = %+v after cancelling, want none", delResp["jobs"])
	}

	// The copy must be claimable again now that it is back to idle.
	code3, resp3 := postJSON(t, mux, http.MethodPost, "/api/pending", `{"itemId":1000008,"village":"home","fromLevel":1}`)
	if code3 != http.StatusOK {
		t.Errorf("re-claiming after undo: status = %d, body = %+v", code3, resp3)
	}
}

// The end-to-end point of the whole feature: declare an upgrade, then have
// a real export show no sign it ever happened. The mismatch must come back
// on the POST response, and the declared action must stop being tracked
// either way (confirmed or not, a real export now covers that window).
//
// Reconciliation only judges an action once a real export is captured
// after the action's own StartedAt (time.Now() at the moment it was
// declared) - so unlike the other fixtures in this file, which use a fixed
// historical timestamp, the exports here are anchored to the real clock.
func TestLocalPendingMismatchSurfacesOnNextRealExport(t *testing.T) {
	mux := newLocalMux(t, "")
	firstTS := time.Now().Add(-time.Hour).Unix()
	postJSON(t, mux, http.MethodPost, "/api/report",
		fmt.Sprintf(`{"tag":"#TEST","timestamp":%d,"buildings":[{"data":1000001,"lvl":2,"cnt":1},{"data":1000008,"lvl":1,"cnt":1}]}`, firstTS))

	code, resp := postJSON(t, mux, http.MethodPost, "/api/pending", `{"itemId":1000008,"village":"home","fromLevel":1}`)
	if code != http.StatusOK {
		t.Fatalf("declare: status = %d, body = %+v", code, resp)
	}

	// A later real export, captured after the action was declared, where
	// the Cannon never actually started - it is still sitting idle at
	// level 1, exactly as before.
	secondTS := time.Now().Add(time.Hour).Unix()
	code2, resp2 := postJSON(t, mux, http.MethodPost, "/api/report",
		fmt.Sprintf(`{"tag":"#TEST","timestamp":%d,"buildings":[{"data":1000001,"lvl":2,"cnt":1},{"data":1000008,"lvl":1,"cnt":1}]}`, secondTS))
	if code2 != http.StatusOK {
		t.Fatalf("second export: status = %d, body = %+v", code2, resp2)
	}
	mismatches, _ := resp2["mismatches"].([]any)
	if len(mismatches) != 1 {
		t.Fatalf("mismatches = %+v, want exactly 1", resp2["mismatches"])
	}
	if declared, _ := resp2["declared"].([]any); len(declared) != 0 {
		t.Errorf("declared = %+v after reconciliation, want none - the action must stop being tracked either way", resp2["declared"])
	}
}

// The confirming counterpart: a real export that DOES show the declared
// upgrade landed must retire it silently, with no mismatch reported.
func TestLocalPendingConfirmedUpgradeProducesNoMismatch(t *testing.T) {
	mux := newLocalMux(t, "")
	firstTS := time.Now().Add(-time.Hour).Unix()
	postJSON(t, mux, http.MethodPost, "/api/report",
		fmt.Sprintf(`{"tag":"#TEST","timestamp":%d,"buildings":[{"data":1000001,"lvl":2,"cnt":1},{"data":1000008,"lvl":1,"cnt":1}]}`, firstTS))
	postJSON(t, mux, http.MethodPost, "/api/pending", `{"itemId":1000008,"village":"home","fromLevel":1}`)

	// A later real export, captured after the action was declared, where
	// the Cannon genuinely landed at level 2.
	secondTS := time.Now().Add(time.Hour).Unix()
	code, resp := postJSON(t, mux, http.MethodPost, "/api/report",
		fmt.Sprintf(`{"tag":"#TEST","timestamp":%d,"buildings":[{"data":1000001,"lvl":2,"cnt":1},{"data":1000008,"lvl":2,"cnt":1}]}`, secondTS))
	if code != http.StatusOK {
		t.Fatalf("second export: status = %d, body = %+v", code, resp)
	}
	if mismatches, _ := resp["mismatches"].([]any); len(mismatches) != 0 {
		t.Errorf("mismatches = %+v, want none - the upgrade genuinely landed", resp["mismatches"])
	}
	if declared, _ := resp["declared"].([]any); len(declared) != 0 {
		t.Errorf("declared = %+v, want none - confirmed and retired", resp["declared"])
	}
}

// "Forget this village" undoes loading something "just to look" - local
// mode's stand-in for the eviction a single-slot cache used to give for
// free, now that several villages persist for the session at once.
func TestLocalForgetVillageRemovesItFromTheSwitcher(t *testing.T) {
	mux := newLocalMux(t, "")
	post := func(body string) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("POST status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}
	post(`{"tag":"#KEEP","timestamp":1700000000,"buildings":[{"data":1000001,"lvl":2,"cnt":1}]}`)
	post(`{"tag":"#DROP","timestamp":1700000000,"buildings":[{"data":1000001,"lvl":2,"cnt":1}]}`)

	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, httptest.NewRequest(http.MethodDelete, "/api/villages?tag=%23DROP", nil))
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, body = %s", delRec.Code, delRec.Body.String())
	}

	villagesRec := httptest.NewRecorder()
	mux.ServeHTTP(villagesRec, httptest.NewRequest(http.MethodGet, "/api/villages", nil))
	var got map[string]any
	json.Unmarshal(villagesRec.Body.Bytes(), &got)
	villages, _ := got["villages"].([]any)
	if len(villages) != 1 {
		t.Fatalf("villages = %+v, want exactly 1 (#KEEP) after forgetting #DROP", got["villages"])
	}

	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/api/report?tag=%23DROP", nil))
	if getRec.Code != http.StatusNotFound {
		t.Errorf("GET forgotten village status = %d, want 404", getRec.Code)
	}
}

func TestLocalCorsAllowsAnyOrigin(t *testing.T) {
	mux := newLocalMux(t, "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/report", nil)
	req.Header.Set("Origin", "http://anything.example.com")
	mux.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want * in local mode", got)
	}
}

func TestHostedRoutesRequireAuth(t *testing.T) {
	// A Config with Hosted:true but no working Store/Auth still lets us
	// verify the auth gate fires before anything else is touched.
	mux := http.NewServeMux()
	New(Config{Catalog: testCatalog(), Hosted: true, Auth: noProviderAuth()}, mux)

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/report", nil),
		httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader(validExport)),
		httptest.NewRequest(http.MethodGet, "/api/history", nil),
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401 (no session cookie presented)", req.Method, req.URL.Path, rec.Code)
		}
	}
}

// A provider with no credentials configured must still be routed to a clear
// 400, not silently fall through to main.go's SPA catch-all mounted on the
// same mux (reproduced here with a stand-in) and returned as a 200 HTML shell.
func TestUnconfiguredProviderReturns400NotTheSPAShell(t *testing.T) {
	mux := http.NewServeMux()
	authSvc := auth.New(nil, "https://coc-progress.example.com", "gh-id", "gh-secret", "", "") // only github configured
	New(Config{Catalog: testCatalog(), Hosted: true, BaseURL: "https://coc-progress.example.com", Auth: authSvc}, mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<!doctype html><html>the SPA shell</html>"))
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/google", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an unconfigured provider", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<!doctype") || strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("body looks like the SPA shell, not a JSON error: %s", rec.Body.String())
	}
}

func TestHostedCorsRejectsUnknownOrigin(t *testing.T) {
	mux := http.NewServeMux()
	New(Config{Catalog: testCatalog(), Hosted: true, BaseURL: "https://coc-progress.example.com", Auth: noProviderAuth()}, mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/catalog", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	mux.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for an unrecognised origin", got)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/catalog", nil)
	req2.Header.Set("Origin", "https://coc-progress.example.com")
	mux.ServeHTTP(rec2, req2)
	if got := rec2.Header().Get("Access-Control-Allow-Origin"); got != "https://coc-progress.example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q, want the matching deployment origin echoed back", got)
	}
}
