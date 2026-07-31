// Package server builds the API mux for both of this project's modes:
// local (no accounts, villages held in an in-process store unless -history
// asks for a durable one) and hosted (accounts required, Postgres-backed,
// quota-guarded).
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/you/coc-progress/internal/analyze"
	"github.com/you/coc-progress/internal/auth"
	"github.com/you/coc-progress/internal/catalog"
	"github.com/you/coc-progress/internal/feature"
	"github.com/you/coc-progress/internal/pending"
	"github.com/you/coc-progress/internal/snapshot"
	"github.com/you/coc-progress/internal/store"
	"github.com/you/coc-progress/internal/store/memory"
)

const (
	// Exports are a few kilobytes; anything far larger is a mistake or an
	// attack. Hosted mode caps far tighter since signup is open.
	maxUploadLocal  = 8 << 20
	maxUploadHosted = 512 << 10

	maxVillagesPerUser     = 5
	maxSnapshotsPerVillage = 100
	maxUploadsPerDay       = 40

	retentionWindow = 14 * 24 * time.Hour

	// localSnapshotLimit caps how many snapshots local mode's default
	// in-memory store keeps per village, so leaving the dashboard open
	// across many exports of the same village cannot grow without bound.
	localSnapshotLimit = 20
)

// Config configures which mode New builds.
type Config struct {
	Catalog *catalog.Catalog
	// Store holds every uploaded export. Nil means "local mode's plain
	// in-memory default" - New fills it in. Hosted mode always sets its own
	// Postgres-backed Store explicitly.
	Store store.Store
	// Durable tells the frontend whether Store survives a restart, so it
	// can say so honestly instead of implying every load is guaranteed to
	// persist. True for hosted mode and for local mode under -history;
	// false for local mode's in-memory default.
	Durable bool
	// Pending holds hand-declared "build now" upgrades, layered onto a
	// village's latest snapshot on the way into analysis. Nil means an
	// in-memory default - New fills it in the same way it does Store.
	Pending pending.Store
	// Features resolves which role each gated flag (internal/feature)
	// currently requires. Nil means "everything unlocked" - local mode
	// never sets this, since it has no accounts or roles to gate by.
	Features feature.Store
	// Auth is nil in local mode, where there are no accounts at all.
	Auth   *auth.Service
	Hosted bool
	// BaseURL is this deployment's own origin (e.g. https://coc-progress.example.com),
	// used for CORS matching and OAuth redirect URLs. Local mode ignores it.
	BaseURL string
	// CronSecret is the bearer token /api/cron/prune requires. Empty
	// disables the endpoint's ability to do anything even if reachable.
	CronSecret string
	// DevLogin registers POST/GET /api/auth/dev, a no-OAuth sign-in
	// shortcut for local development - see handleDevLogin. main.go only
	// ever sets this true from an explicit DEV_LOGIN=1 *and* a localhost
	// BaseURL, never against a real deployment.
	DevLogin bool
	// InitialSnapshotPaths, local mode only, seeds the store at startup -
	// the -snapshot flag, one village per path.
	InitialSnapshotPaths []string
}

type api struct {
	cfg Config
}

// New registers the API routes onto mux. The caller (main.go) registers the
// SPA static handler on the same mux afterwards, since the embedded
// frontend lives at the module root and //go:embed paths cannot reach
// across packages.
func New(cfg Config, mux *http.ServeMux) {
	if cfg.Store == nil {
		cfg.Store = memory.NewWithLimit(localSnapshotLimit)
	}
	if cfg.Pending == nil {
		cfg.Pending = pending.NewMemoryStore()
	}
	s := &api{cfg: cfg}
	for _, path := range cfg.InitialSnapshotPaths {
		if path == "" {
			continue
		}
		if err := s.loadInitialSnapshot(path); err != nil {
			log.Fatalf("snapshot %s: %v", path, err)
		}
	}

	mux.HandleFunc("/api/report", s.handleReport)
	mux.HandleFunc("/api/villages", s.handleVillages)
	mux.HandleFunc("/api/pending", s.handlePending)
	mux.HandleFunc("/api/catalog", s.handleCatalog)
	mux.HandleFunc("/api/history", s.handleHistory)
	mux.HandleFunc("/api/features", s.handleFeatures)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	if cfg.Hosted {
		mux.HandleFunc("/api/config", s.handleConfig)
		mux.HandleFunc("/api/me", s.handleMe)
		mux.HandleFunc("/api/admin/users", s.handleAdminUsers)
		mux.HandleFunc("/api/auth/logout", s.handleLogout)
		mux.HandleFunc("/api/cron/prune", s.handlePrune)
		if cfg.DevLogin {
			mux.HandleFunc("/api/auth/dev", s.handleDevLogin)
		}
		// Both providers are always routed, configured or not: an
		// unregistered path would otherwise fall through to the SPA
		// catch-all and return the frontend shell with a 200 instead of a
		// clear error. handleAuthStart/handleAuthCallback already reject an
		// unconfigured provider with 400.
		for _, name := range []string{"github", "google"} {
			name := name
			mux.HandleFunc("/api/auth/"+name, func(w http.ResponseWriter, r *http.Request) {
				s.handleAuthStart(w, r, name)
			})
			mux.HandleFunc("/api/auth/"+name+"/callback", func(w http.ResponseWriter, r *http.Request) {
				s.handleAuthCallback(w, r, name)
			})
		}
	}
}

// storeTag is the key an export is filed under - not necessarily exp.Tag
// verbatim. snapshot.Parse never requires a tag, so an export can
// legitimately carry "" - without normalizing that, it would save under
// the empty string, which resolveTag instead treats as "the caller
// expressed no preference," making the village permanently unrecoverable
// by tag. "unknown" deliberately matches file.Store's own sanitize
// fallback for an empty tag, so the file and memory stores agree.
func storeTag(exp *snapshot.Export) string {
	if exp.Tag == "" {
		return "unknown"
	}
	return exp.Tag
}

// resolveTag decides which village a request resolves to: whatever ?tag=
// asked for, or otherwise whichever village userID captured most recently.
// Returns "" with a nil error when userID has no villages at all yet.
func (s *api) resolveTag(r *http.Request, userID int64) (string, error) {
	if t := r.URL.Query().Get("tag"); t != "" {
		return t, nil
	}
	vs, err := s.cfg.Store.Villages(r.Context(), userID)
	if err != nil || len(vs) == 0 {
		return "", err
	}
	return vs[0].Tag, nil
}

func (s *api) loadInitialSnapshot(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	exp, err := snapshot.Parse(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if err := s.cfg.Store.Save(context.Background(), 0, storeTag(exp), exp.CapturedAt(), raw); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	rep := analyze.Run(exp, s.cfg.Catalog, time.Now())
	log.Printf("loaded %s: %s, Town Hall %d, %d upgrades running", path, rep.Tag, rep.Gates.TownHall, len(rep.Jobs))
	return nil
}

// quotaStore is the subset of *postgres.Store the open-signup guards need.
// Local stores (memory, file) do not implement it, so a type assertion
// against it doubles as "are we actually talking to Postgres".
type quotaStore interface {
	VillageCount(ctx context.Context, userID int64) (int, error)
	SnapshotCount(ctx context.Context, userID int64, tag string) (int, error)
	UploadsToday(ctx context.Context, userID int64) (int, error)
}

func (s *api) handleReport(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		s.getReport(w, r)
	case http.MethodPost:
		s.postReport(w, r)
	default:
		httpError(w, http.StatusMethodNotAllowed, "Use GET or POST.")
	}
}

func (s *api) getReport(w http.ResponseWriter, r *http.Request) {
	var userID int64
	if s.cfg.Hosted {
		u := s.cfg.Auth.User(r)
		if u == nil {
			httpError(w, http.StatusUnauthorized, "Sign in to see your villages.")
			return
		}
		userID = u.ID
	}

	tag, err := s.resolveTag(r, userID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tag == "" {
		httpError(w, http.StatusNotFound, "No village loaded yet. Send an export to this endpoint to analyse one.")
		return
	}
	resp, err := s.buildReportResponse(r.Context(), userID, tag)
	if err != nil {
		s.writeReportError(w, err)
		return
	}
	writeJSON(w, resp)
}

// errNoSnapshot means (userID, tag) has no snapshot to analyse - distinct
// from any other failure so callers can map it to 404 rather than 500.
var errNoSnapshot = errors.New("no snapshot found for that village")

// reportResponse is the analysed report plus the pending-action extras the
// analyser has no business knowing about. The embedded pointer's fields
// marshal inline, so the response shape the frontend already reads
// (report.tag, report.jobs, ...) is unchanged - Declared is additive.
type reportResponse struct {
	*analyze.Report
	// Declared is every outstanding hand-declared upgrade currently
	// overlaid onto this report - not a full audit log, just "what you
	// clicked that a fresh export hasn't confirmed yet."
	Declared []pending.Action `json:"declared,omitempty"`
	// Mismatches is set only on the POST response for an export that just
	// got checked against outstanding declared actions: what you said you
	// started that this export shows no sign of. It is not persisted or
	// re-derivable later - miss it here and it is gone, the same way the
	// declared action itself is gone once retired.
	Mismatches []pending.Mismatch `json:"mismatches,omitempty"`
}

// loadOverlay loads (userID, tag)'s latest snapshot and overlays its
// outstanding pending actions, retiring any Apply finds it can no longer
// place - which can only happen if something else changed since an action
// was added, such as two build-now clicks racing for the same idle copy.
// exp is also returned since callers validating or adding a new action
// need the un-overlaid original, not just the result.
func (s *api) loadOverlay(ctx context.Context, userID int64, tag string) (exp, overlaid *snapshot.Export, applied []pending.Action, err error) {
	snaps, err := s.cfg.Store.Recent(ctx, userID, tag, 1)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(snaps) == 0 {
		return nil, nil, nil, errNoSnapshot
	}
	exp, err = snapshot.Parse(bytes.NewReader(snaps[0].Raw))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("a stored snapshot is corrupt: %w", err)
	}

	existing, err := s.cfg.Pending.List(ctx, userID, tag)
	if err != nil {
		return nil, nil, nil, err
	}
	var stale []pending.Action
	overlaid, applied, stale = pending.Apply(exp, existing, s.cfg.Catalog)
	if len(stale) > 0 {
		ids := make([]string, len(stale))
		for i, a := range stale {
			ids[i] = a.ID
		}
		if err := s.cfg.Pending.Remove(ctx, userID, tag, ids); err != nil {
			log.Printf("retire stale pending actions: %v", err)
		}
	}
	return exp, overlaid, applied, nil
}

// buildReportResponse loads (userID, tag)'s latest snapshot, overlays its
// outstanding pending actions, and analyses the result as of now.
func (s *api) buildReportResponse(ctx context.Context, userID int64, tag string) (*reportResponse, error) {
	_, overlaid, applied, err := s.loadOverlay(ctx, userID, tag)
	if err != nil {
		return nil, err
	}
	rep := analyze.Run(overlaid, s.cfg.Catalog, time.Now())
	return &reportResponse{Report: rep, Declared: applied}, nil
}

func (s *api) writeReportError(w http.ResponseWriter, err error) {
	if errors.Is(err, errNoSnapshot) {
		httpError(w, http.StatusNotFound, "No snapshot found for that village.")
		return
	}
	httpError(w, http.StatusInternalServerError, err.Error())
}

// forgetStore is implemented by the local stores, where "stop tracking
// this village" is a plain user-facing action: loading someone else's
// export "just to look" now persists in the switcher for the session
// instead of being evicted by your next upload, so there needs to be a way
// back. Postgres deletion is a hosted retention concern with its own rules
// (Prune, the 14-day window) and deliberately does not implement this.
type forgetStore interface {
	Forget(ctx context.Context, userID int64, tag string) error
}

// handleVillages lists every village the caller has a snapshot for (GET),
// newest first, for a switcher UI, or drops one entirely (DELETE).
// Mode-agnostic: local mode's userID is always 0.
func (s *api) handleVillages(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodDelete:
		s.forgetVillage(w, r)
		return
	case http.MethodGet:
		// falls through to the listing below
	default:
		httpError(w, http.StatusMethodNotAllowed, "Use GET or DELETE.")
		return
	}

	var userID int64
	if s.cfg.Hosted {
		u := s.cfg.Auth.User(r)
		if u == nil {
			httpError(w, http.StatusUnauthorized, "Sign in to see your villages.")
			return
		}
		userID = u.ID
	}

	vs, err := s.cfg.Store.Villages(r.Context(), userID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// tag is the store's own key - not necessarily what the export itself
	// called the village (the file store's key is a sanitized directory
	// name). label is for display, read back off the newest snapshot this
	// endpoint already has to fetch, falling back to tag if that export is
	// somehow unparsable.
	type village struct {
		Tag            string    `json:"tag"`
		Label          string    `json:"label"`
		LastCapturedAt time.Time `json:"lastCapturedAt"`
		Snapshots      int       `json:"snapshots"`
	}
	out := make([]village, 0, len(vs))
	for _, v := range vs {
		label := v.Tag
		if snaps, err := s.cfg.Store.Recent(r.Context(), userID, v.Tag, 1); err == nil && len(snaps) > 0 {
			if exp, err := snapshot.Parse(bytes.NewReader(snaps[0].Raw)); err == nil && exp.Tag != "" {
				label = exp.Tag
			}
		}
		out = append(out, village{Tag: v.Tag, Label: label, LastCapturedAt: v.LastCapturedAt, Snapshots: v.Snapshots})
	}
	writeJSON(w, map[string]any{"villages": out})
}

// forgetVillage drops every snapshot for one village, and anything
// currently declared for it (there is nothing left to overlay that onto).
// Only available where the underlying store supports it - see forgetStore.
func (s *api) forgetVillage(w http.ResponseWriter, r *http.Request) {
	var userID int64
	if s.cfg.Hosted {
		u := s.cfg.Auth.User(r)
		if u == nil {
			httpError(w, http.StatusUnauthorized, "Sign in to manage your villages.")
			return
		}
		userID = u.ID
	}
	tag := r.URL.Query().Get("tag")
	if tag == "" {
		httpError(w, http.StatusBadRequest, "?tag= is required.")
		return
	}

	fs, ok := s.cfg.Store.(forgetStore)
	if !ok {
		httpError(w, http.StatusNotImplemented, "This deployment's storage does not support forgetting a village.")
		return
	}
	if err := fs.Forget(r.Context(), userID, tag); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing, err := s.cfg.Pending.List(r.Context(), userID, tag); err == nil && len(existing) > 0 {
		ids := make([]string, len(existing))
		for i, a := range existing {
			ids[i] = a.ID
		}
		if err := s.cfg.Pending.Remove(r.Context(), userID, tag, ids); err != nil {
			log.Printf("drop pending actions for forgotten village: %v", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *api) postReport(w http.ResponseWriter, r *http.Request) {
	maxBytes := int64(maxUploadLocal)
	var userID int64

	if s.cfg.Hosted {
		if !s.sameOriginOK(r) {
			httpError(w, http.StatusForbidden, "Cross-origin upload rejected.")
			return
		}
		u := s.cfg.Auth.User(r)
		if u == nil {
			httpError(w, http.StatusUnauthorized, "Sign in to save an export.")
			return
		}
		userID = u.ID
		maxBytes = maxUploadHosted
	}

	body := http.MaxBytesReader(w, r.Body, maxBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		httpError(w, http.StatusRequestEntityTooLarge, "That file is too large to be a village export.")
		return
	}
	exp, err := snapshot.Parse(bytes.NewReader(raw))
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	if s.cfg.Hosted {
		if err := s.checkQuota(r.Context(), userID, storeTag(exp)); err != nil {
			httpError(w, http.StatusTooManyRequests, err.Error())
			return
		}
	}

	tag := storeTag(exp)

	// Whatever this village's snapshot was immediately before saving the
	// new one - the "before" side of both the change log and pending-action
	// reconciliation, neither of which should ever compare against a
	// synthetic overlay. Best-effort: a first-ever upload for this village,
	// or one somehow corrupt, just means there is nothing to reconcile yet.
	var prev *snapshot.Export
	if prevSnaps, err := s.cfg.Store.Recent(r.Context(), userID, tag, 1); err == nil && len(prevSnaps) > 0 {
		prev, _ = snapshot.Parse(bytes.NewReader(prevSnaps[0].Raw))
	}

	if err := s.cfg.Store.Save(r.Context(), userID, tag, exp.CapturedAt(), raw); err != nil {
		if s.cfg.Hosted {
			httpError(w, http.StatusInternalServerError, "Could not save that export.")
			return
		}
		// Local mode's default store is in-process memory, which in
		// practice cannot fail to Save - but if something still goes
		// wrong, don't throw away the analysis by refusing to respond.
		log.Printf("save: %v", err)
	}

	// A real export landing is the one moment declared-by-hand actions get
	// checked against reality rather than just re-overlaid. Retired either
	// way - confirmed or not - since a real export now covers whatever
	// window it was declared in; only mismatches get reported back, and
	// only for this one response, not persisted anywhere.
	var mismatches []pending.Mismatch
	if prev != nil {
		if existing, err := s.cfg.Pending.List(r.Context(), userID, tag); err == nil && len(existing) > 0 {
			retired, mm := pending.Reconcile(existing, prev, exp, s.cfg.Catalog)
			mismatches = mm
			if len(retired) > 0 {
				ids := make([]string, len(retired))
				for i, a := range retired {
					ids[i] = a.ID
				}
				if err := s.cfg.Pending.Remove(r.Context(), userID, tag, ids); err != nil {
					log.Printf("retire reconciled pending actions: %v", err)
				}
			}
		}
	}

	// Routed back through buildReportResponse (rather than analyzing exp
	// directly) so any pending actions this village still has outstanding
	// stay overlaid, and any this new export has already overtaken get
	// retired the same way a plain GET would retire them.
	resp, err := s.buildReportResponse(r.Context(), userID, tag)
	if err != nil {
		s.writeReportError(w, err)
		return
	}
	resp.Mismatches = mismatches
	writeJSON(w, resp)
}

// handlePending declares or cancels a hand-declared "build now" upgrade -
// see pending.Apply for how it is overlaid onto analysis.
func (s *api) handlePending(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPost:
		s.postPending(w, r)
	case http.MethodDelete:
		s.deletePending(w, r)
	default:
		httpError(w, http.StatusMethodNotAllowed, "Use POST or DELETE.")
	}
}

func (s *api) pendingUserAndTag(w http.ResponseWriter, r *http.Request) (userID int64, tag string, ok bool) {
	if s.cfg.Hosted {
		u := s.cfg.Auth.User(r)
		if u == nil {
			httpError(w, http.StatusUnauthorized, "Sign in to manage a declared upgrade.")
			return 0, "", false
		}
		userID = u.ID
	}
	tag, err := s.resolveTag(r, userID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return 0, "", false
	}
	if tag == "" {
		httpError(w, http.StatusNotFound, "No village loaded yet. Send an export to this endpoint to analyse one.")
		return 0, "", false
	}
	return userID, tag, true
}

// postPending declares one upgrade started. The request only gets to say
// WHICH upgrade (item, village, the level it is starting from) - cost,
// seconds, lane, name and icon are re-derived from the current report's
// own NextUp list rather than trusted from the client, which reuses every
// gate/ceiling/catalog rule for free and is the only thing standing
// between a client and inventing an upgrade that was never actually
// available.
func (s *api) postPending(w http.ResponseWriter, r *http.Request) {
	userID, tag, ok := s.pendingUserAndTag(w, r)
	if !ok {
		return
	}
	if !s.featureUnlocked(r, feature.BuildNow) {
		httpError(w, http.StatusForbidden, "Build Now isn't available on your account yet - ask an admin to unlock it.")
		return
	}

	var req struct {
		ItemID    int    `json:"itemId"`
		Village   string `json:"village"`
		FromLevel int    `json:"fromLevel"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "That request body could not be read.")
		return
	}

	_, overlaid, _, err := s.loadOverlay(r.Context(), userID, tag)
	if err != nil {
		s.writeReportError(w, err)
		return
	}
	rep := analyze.Run(overlaid, s.cfg.Catalog, time.Now())
	var suggestion *analyze.Suggestion
	for i := range rep.NextUp {
		sug := rep.NextUp[i]
		if sug.ID == req.ItemID && sug.Village == req.Village && sug.FromLevel == req.FromLevel {
			suggestion = &sug
			break
		}
	}
	if suggestion == nil {
		httpError(w, http.StatusConflict, "That upgrade is not currently startable - it may already be claimed, already at its ceiling, or not owned at that level.")
		return
	}

	candidate := pending.Action{
		ID:        pending.NewID(),
		Tag:       tag,
		ItemID:    suggestion.ID,
		Name:      suggestion.Name,
		Icon:      suggestion.Icon,
		Village:   suggestion.Village,
		Lane:      suggestion.Lane,
		FromLevel: suggestion.FromLevel,
		ToLevel:   suggestion.ToLevel,
		Seconds:   suggestion.Seconds,
		StartedAt: time.Now(),
	}
	// NextUp alone does not know a copy is already claimed by an
	// outstanding action - it always names the lowest owned bucket
	// regardless of what is already mid-upgrade. Confirm the candidate can
	// actually be placed against the already-overlaid export (not the raw
	// one) before persisting it, so a second claim on the same copy is
	// rejected here instead of being silently accepted and then evaporating
	// as stale the moment anything reads it back.
	if _, placed, _ := pending.Apply(overlaid, []pending.Action{candidate}, s.cfg.Catalog); len(placed) == 0 {
		httpError(w, http.StatusConflict, "That upgrade is not currently startable - it may already be claimed, already at its ceiling, or not owned at that level.")
		return
	}

	if err := s.cfg.Pending.Add(r.Context(), userID, tag, candidate); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp, err := s.buildReportResponse(r.Context(), userID, tag)
	if err != nil {
		s.writeReportError(w, err)
		return
	}
	writeJSON(w, resp)
}

// deletePending cancels one declared upgrade - a misclick's only way back,
// especially for an instant bump (Walls, equipment), which has no other
// undo. Removing an id that is not present (already retired, or simply
// wrong) is not an error - the caller gets the same fresh report either way.
func (s *api) deletePending(w http.ResponseWriter, r *http.Request) {
	userID, tag, ok := s.pendingUserAndTag(w, r)
	if !ok {
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		httpError(w, http.StatusBadRequest, "?id= is required.")
		return
	}
	if err := s.cfg.Pending.Remove(r.Context(), userID, tag, []string{id}); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp, err := s.buildReportResponse(r.Context(), userID, tag)
	if err != nil {
		s.writeReportError(w, err)
		return
	}
	writeJSON(w, resp)
}

func (s *api) checkQuota(ctx context.Context, userID int64, tag string) error {
	q, ok := s.cfg.Store.(quotaStore)
	if !ok {
		return nil
	}
	n, err := q.SnapshotCount(ctx, userID, tag)
	if err == nil && n >= maxSnapshotsPerVillage {
		return fmt.Errorf("this village already has %d saved snapshots, the limit - older ones are pruned automatically after 14 days", maxSnapshotsPerVillage)
	}
	if err == nil && n == 0 {
		vc, err := q.VillageCount(ctx, userID)
		if err == nil && vc >= maxVillagesPerUser {
			return fmt.Errorf("you already have %d villages saved, the limit for now", maxVillagesPerUser)
		}
	}
	today, err := q.UploadsToday(ctx, userID)
	if err == nil && today >= maxUploadsPerDay {
		return fmt.Errorf("you've hit today's upload limit (%d) - try again tomorrow", maxUploadsPerDay)
	}
	return nil
}

// handleFeatures reports which gated capabilities (internal/feature) the
// caller currently has, so the frontend knows whether to show the theme
// picker or the Build Now button. Registered in both modes: local mode
// always returns every key unlocked (no accounts, nothing to gate against);
// hosted mode resolves them from the signed-in user's role, treating nobody
// signed in as the lowest role rather than an error.
func (s *api) handleFeatures(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	unlocked := []string{}
	for _, key := range feature.Keys() {
		if s.featureUnlocked(r, key) {
			unlocked = append(unlocked, key)
		}
	}
	writeJSON(w, map[string]any{"unlocked": unlocked})
}

// roleOf is the signed-in caller's role, or feature.RoleUser (the lowest)
// if nobody is signed in - hosted mode only, and only meaningful there.
func (s *api) roleOf(r *http.Request) string {
	if u := s.cfg.Auth.User(r); u != nil {
		return u.Role
	}
	return feature.RoleUser
}

// featureUnlocked is the one place that decides whether the caller may use a
// gated feature - both handleFeatures (a read) and postPending (a write) go
// through it so they can never disagree the way they once did. Local mode,
// or hosted mode with no Features store configured, is always unlocked -
// there is nothing to gate against. Otherwise it fails closed: any error
// resolving the required role is treated as locked, not unlocked, and
// logged rather than swallowed - postPending guards a real write and must
// never fail open just because a lookup errored.
func (s *api) featureUnlocked(r *http.Request, key string) bool {
	if !s.cfg.Hosted || s.cfg.Features == nil {
		return true
	}
	required, err := s.cfg.Features.RequiredRole(r.Context(), key)
	if err != nil {
		log.Printf("feature %q: resolve required role: %v", key, err)
		return false
	}
	return feature.Unlocked(s.roleOf(r), required)
}

func (s *api) handleCatalog(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	writeJSON(w, s.cfg.Catalog)
}

func (s *api) handleHistory(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var userID int64
	if s.cfg.Hosted {
		u := s.cfg.Auth.User(r)
		if u == nil {
			httpError(w, http.StatusUnauthorized, "Sign in to see history.")
			return
		}
		userID = u.ID
	}

	tag, err := s.resolveTag(r, userID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tag == "" {
		writeJSON(w, map[string]any{"enabled": true, "durable": s.cfg.Durable, "changeLog": nil})
		return
	}

	snaps, err := s.cfg.Store.Recent(r.Context(), userID, tag, 2)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(snaps) < 2 {
		writeJSON(w, map[string]any{"enabled": true, "durable": s.cfg.Durable, "changeLog": nil})
		return
	}
	cur, err := snapshot.Parse(bytes.NewReader(snaps[0].Raw))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "a stored snapshot is corrupt: "+err.Error())
		return
	}
	prev, err := snapshot.Parse(bytes.NewReader(snaps[1].Raw))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "a stored snapshot is corrupt: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"enabled": true, "durable": s.cfg.Durable, "changeLog": analyze.Diff(prev, cur, s.cfg.Catalog)})
}

func (s *api) handleConfig(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	writeJSON(w, map[string]any{"hosted": true, "providers": s.cfg.Auth.Providers(), "devLogin": s.cfg.DevLogin})
}

// handleDevLogin signs in as an arbitrary email with no OAuth exchange at
// all - registered only when cfg.DevLogin is true (see Config.DevLogin).
// Exists so roles, the admin board, and gated features can be exercised
// locally without registering a real OAuth app first.
func (s *api) handleDevLogin(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	email := r.URL.Query().Get("email")
	name := r.URL.Query().Get("name")
	if _, err := s.cfg.Auth.DevLogin(w, r, email, name); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *api) handleMe(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	writeJSON(w, map[string]any{"user": s.cfg.Auth.User(r)})
}

func (s *api) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.cfg.Auth.Logout(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *api) handleAuthStart(w http.ResponseWriter, r *http.Request, provider string) {
	if err := s.cfg.Auth.Start(w, r, provider); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *api) handleAuthCallback(w http.ResponseWriter, r *http.Request, provider string) {
	if _, err := s.cfg.Auth.Callback(w, r, provider); err != nil {
		http.Redirect(w, r, "/?auth_error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// handlePrune backs the daily Vercel cron: delete snapshots older than the
// 14-day retention window, always keeping each village's newest, and clear
// expired sessions.
func (s *api) handlePrune(w http.ResponseWriter, r *http.Request) {
	if s.cfg.CronSecret == "" || r.Header.Get("Authorization") != "Bearer "+s.cfg.CronSecret {
		httpError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	n, err := s.cfg.Store.Prune(r.Context(), time.Now().Add(-retentionWindow))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"removed": n})
}

// cors allows the Vite dev server to talk to the API during local
// development, and in hosted mode allows only this deployment's own origin
// (plus a local frontend dev server pointed at it) - a wildcard origin is
// invalid once cookies are involved.
func (s *api) cors(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Hosted {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		return
	}
	origin := r.Header.Get("Origin")
	if origin == "" || (origin != s.cfg.BaseURL && origin != "http://localhost:5173") {
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Vary", "Origin")
}

// sameOriginOK is a defense-in-depth CSRF check for state-changing hosted
// requests: a browser always sends Origin on a cross-origin POST, so an
// absent header means a non-browser client (curl, a server) rather than a
// forged form post from another site.
func (s *api) sameOriginOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return origin == s.cfg.BaseURL || origin == "http://localhost:5173"
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
