package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/you/coc-progress/internal/analyze"
	"github.com/you/coc-progress/internal/snapshot"
	"github.com/you/coc-progress/internal/store/postgres"
)

// digestStore is satisfied by *postgres.Store alone - see adminStore's own
// comment for why a type assertion against cfg.Store doubles as "are we
// actually hosted on Postgres." There is no local-mode equivalent: an email
// digest has nothing to do without an account to send it to.
type digestStore interface {
	DigestOptIn(ctx context.Context, userID int64) (bool, error)
	SetDigestOptIn(ctx context.Context, userID int64, optIn bool) error
	DigestCandidates(ctx context.Context) ([]postgres.DigestUser, error)
	SetDigestCheckedAt(ctx context.Context, userID int64, at time.Time) error
}

// handleDigestOptIn reads (GET) or flips (POST) the signed-in user's own
// preference for the landed-timer email digest - the one preference this
// app has, so it gets the simplest possible toggle rather than a settings
// page.
func (s *api) handleDigestOptIn(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodGet, http.MethodPost:
	default:
		httpError(w, http.StatusMethodNotAllowed, "Use GET or POST.")
		return
	}

	digests, ok := s.cfg.Store.(digestStore)
	if !ok {
		httpError(w, http.StatusNotImplemented, "This deployment's storage does not support the email digest.")
		return
	}
	u := s.cfg.Auth.User(r)
	if u == nil {
		httpError(w, http.StatusUnauthorized, "Sign in to change this.")
		return
	}

	if r.Method == http.MethodGet {
		optIn, err := digests.DigestOptIn(r.Context(), u.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"optIn": optIn})
		return
	}

	var req struct {
		OptIn bool `json:"optIn"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "That request body could not be read.")
		return
	}
	if err := digests.SetDigestOptIn(r.Context(), u.ID, req.OptIn); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"optIn": req.OptIn})
}

// handleCronDigest backs a cron that is not yet scheduled anywhere (see
// README's "Email digest" - this is deliberately not wired into
// vercel.json yet): for every opted-in user, across every village they
// have, find upgrades that landed since that user's own last check, and in
// dev mode log what would have been sent rather than actually emailing
// anyone - there is no real provider wired up. Mirrors handlePrune's
// bearer-auth convention exactly.
func (s *api) handleCronDigest(w http.ResponseWriter, r *http.Request) {
	if s.cfg.CronSecret == "" || r.Header.Get("Authorization") != "Bearer "+s.cfg.CronSecret {
		httpError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	digests, ok := s.cfg.Store.(digestStore)
	if !ok {
		httpError(w, http.StatusNotImplemented, "This deployment's storage does not support the email digest.")
		return
	}

	candidates, err := digests.DigestCandidates(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now()
	logged := 0
	for _, cand := range candidates {
		landed, err := s.landedSince(r.Context(), cand.UserID, cand.CheckedAt, now)
		if err != nil {
			log.Printf("digest: user %d: %v", cand.UserID, err)
			continue
		}
		if len(landed) > 0 {
			// TODO(real email provider): this is where an actual send call
			// goes, once one is chosen - see README's "Email digest".
			log.Printf("digest for %s: %d upgrade(s) landed since %s: %v", cand.Email, len(landed), cand.CheckedAt.Format(time.RFC3339), landed)
			logged++
		}
		if err := digests.SetDigestCheckedAt(r.Context(), cand.UserID, now); err != nil {
			log.Printf("digest: user %d: advance checked-at: %v", cand.UserID, err)
		}
	}
	writeJSON(w, map[string]any{"usersChecked": len(candidates), "digestsLogged": logged})
}

// landedSince finds every job, across every village userID has, whose
// finish time falls in (since, now] - reading each village's latest
// snapshot and re-running analyze.Run against now, the same live
// re-analysis getReport already does on every GET. This only sees a
// landing while it is still represented as a job with a past-due
// FinishesAt in the latest snapshot - if the user re-exports past it before
// this runs, the fresh snapshot has no timer left to report at all. That
// matches this project's existing "countdowns are computed from absolute
// finish times" stance rather than diffing two exports the way History
// does, since a digest is about wall-clock landings, not export cadence.
func (s *api) landedSince(ctx context.Context, userID int64, since, now time.Time) ([]string, error) {
	villages, err := s.cfg.Store.Villages(ctx, userID)
	if err != nil {
		return nil, err
	}
	var landed []string
	for _, v := range villages {
		snaps, err := s.cfg.Store.Recent(ctx, userID, v.Tag, 1)
		if err != nil || len(snaps) == 0 {
			continue
		}
		exp, err := snapshot.Parse(bytes.NewReader(snaps[0].Raw))
		if err != nil {
			continue
		}
		rep := analyze.Run(exp, s.cfg.Catalog, now)
		for _, j := range rep.Jobs {
			if j.FinishesAt.After(since) && !j.FinishesAt.After(now) {
				landed = append(landed, fmt.Sprintf("%s (%s)", j.Name, v.Tag))
			}
		}
	}
	return landed, nil
}
