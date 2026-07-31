package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/you/coc-progress/internal/feature"
	"github.com/you/coc-progress/internal/store/postgres"
)

// adminStore is satisfied by *postgres.Store alone - there is no local-mode
// equivalent, since local mode has no accounts to administer at all. A type
// assertion against cfg.Store (already polymorphic across the memory/file/
// Postgres snapshot backends) doubles as "are we actually hosted on
// Postgres," the same trick quotaStore and forgetStore already use above.
type adminStore interface {
	ListUsers(ctx context.Context) ([]postgres.AdminUser, error)
	SetRole(ctx context.Context, userID int64, role string) error
	CountAdmins(ctx context.Context) (int, error)
}

// handleAdminUsers lists every user (GET) or changes one's role (POST).
// Registered only in hosted mode (see New) - and even there, only reachable
// by an already-admin session.
func (s *api) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
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

	admins, ok := s.cfg.Store.(adminStore)
	if !ok {
		httpError(w, http.StatusNotImplemented, "This deployment's storage does not support the admin board.")
		return
	}
	u := s.cfg.Auth.User(r)
	if u == nil {
		httpError(w, http.StatusUnauthorized, "Sign in to manage users.")
		return
	}
	if u.Role != feature.RoleAdmin {
		httpError(w, http.StatusForbidden, "Admin only.")
		return
	}

	if r.Method == http.MethodGet {
		s.writeUserList(w, r, admins)
		return
	}

	var req struct {
		UserID int64  `json:"userId"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "That request body could not be read.")
		return
	}
	if req.Role != feature.RoleUser && req.Role != feature.RoleAdmin {
		httpError(w, http.StatusBadRequest, `role must be "user" or "admin".`)
		return
	}
	// Guards the one likely accident - the sole admin demoting themself and
	// locking everyone out of the board. Does not chase every other way to
	// reach zero admins; a simple count check is enough here.
	if req.UserID == u.ID && req.Role != feature.RoleAdmin {
		if n, err := admins.CountAdmins(r.Context()); err == nil && n <= 1 {
			httpError(w, http.StatusConflict, "You are the only admin - promote someone else first.")
			return
		}
	}
	if err := admins.SetRole(r.Context(), req.UserID, req.Role); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeUserList(w, r, admins)
}

func (s *api) writeUserList(w http.ResponseWriter, r *http.Request, admins adminStore) {
	users, err := admins.ListUsers(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"users": users})
}
