// Package postgres is a Store backed by Postgres, for the hosted
// deployment. Snapshots are scoped to a user so villages never cross
// between accounts.
package postgres

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/you/coc-progress/internal/feature"
	"github.com/you/coc-progress/internal/store"
)

//go:embed schema.sql
var schema string

// migrationLockKey is an arbitrary constant scoped to this application, used
// as a Postgres advisory lock so concurrent cold starts cannot race the
// schema migration against each other.
const migrationLockKey = 727202601

var _ store.Store = (*Store)(nil)
var _ feature.Store = (*Store)(nil)

type Store struct {
	pool *pgxpool.Pool
}

// Open connects to Postgres and applies the schema. dsn should point at a
// transaction-pooling endpoint (e.g. Neon's "-pooler" host) when running as
// a serverless function, since every function instance opens its own pool.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	// A transaction pooler (PgBouncer, Neon's pooled endpoint) does not
	// support session-level prepared statements, so every query must be a
	// plain, unprepared one.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	cfg.ConnConfig.StatementCacheCapacity = 0
	cfg.ConnConfig.DescriptionCacheCapacity = 0
	if cfg.MaxConns <= 0 || cfg.MaxConns > 4 {
		cfg.MaxConns = 4 // each serverless instance opens its own pool
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", int64(migrationLockKey)); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", int64(migrationLockKey))

	if _, err := conn.Exec(ctx, schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

func (s *Store) Close() { s.pool.Close() }

// Pool exposes the underlying connection pool, for callers (tests, the
// auth package's structural Store implementation check) that need direct
// SQL access beyond the Store interface.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Ping confirms the pool can actually reach the database, for a startup
// check distinct from schema migration succeeding.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Store) villageID(ctx context.Context, userID int64, tag string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO villages (user_id, tag) VALUES ($1, $2)
		ON CONFLICT (user_id, tag) DO UPDATE SET tag = EXCLUDED.tag
		RETURNING id`, userID, tag).Scan(&id)
	return id, err
}

func (s *Store) Save(ctx context.Context, userID int64, tag string, capturedAt time.Time, raw []byte) error {
	vid, err := s.villageID(ctx, userID, tag)
	if err != nil {
		return fmt.Errorf("village: %w", err)
	}
	// raw must cross as text, not bytea - a bare []byte parameter otherwise
	// sends its bytea wire form, which does not cast to jsonb.
	_, err = s.pool.Exec(ctx, `
		INSERT INTO snapshots (village_id, captured_at, raw) VALUES ($1, $2, $3::jsonb)
		ON CONFLICT (village_id, captured_at) DO NOTHING`, vid, capturedAt, string(raw))
	return err
}

func (s *Store) Recent(ctx context.Context, userID int64, tag string, n int) ([]store.Snapshot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.captured_at, s.raw
		FROM snapshots s
		JOIN villages v ON v.id = s.village_id
		WHERE v.user_id = $1 AND v.tag = $2
		ORDER BY s.captured_at DESC
		LIMIT $3`, userID, tag, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.Snapshot
	for rows.Next() {
		var capturedAt time.Time
		var raw []byte
		if err := rows.Scan(&capturedAt, &raw); err != nil {
			return nil, err
		}
		out = append(out, store.Snapshot{Tag: tag, CapturedAt: capturedAt, Raw: raw})
	}
	return out, rows.Err()
}

// UpsertUser creates or updates the local record for an OAuth identity,
// keyed on (provider, providerID) so the same GitHub or Google account
// always maps to the same user row.
func (s *Store) UpsertUser(ctx context.Context, provider, providerID, email, name, avatarURL string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (provider, provider_id, email, name, avatar_url)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (provider, provider_id) DO UPDATE
		SET email = EXCLUDED.email, name = EXCLUDED.name, avatar_url = EXCLUDED.avatar_url
		RETURNING id`, provider, providerID, email, name, avatarURL).Scan(&id)
	return id, err
}

// CreateSession records a new session. tokenHash is the SHA-256 of the
// cookie value - the raw token itself is never stored.
func (s *Store) CreateSession(ctx context.Context, userID int64, tokenHash []byte, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`, tokenHash, userID, expiresAt)
	return err
}

// UserBySessionToken resolves a session cookie's hash to the account behind
// it, only if the session has not expired. A zero userID with a nil error
// means "no such session" - callers should treat that as unauthenticated,
// not as a server error.
func (s *Store) UserBySessionToken(ctx context.Context, tokenHash []byte) (userID int64, email, name, avatarURL, role string, err error) {
	var e, n, a *string
	err = s.pool.QueryRow(ctx, `
		SELECT u.id, u.email, u.name, u.avatar_url, u.role
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash).Scan(&userID, &e, &n, &a, &role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, "", "", "", "", nil
		}
		return 0, "", "", "", "", err
	}
	if e != nil {
		email = *e
	}
	if n != nil {
		name = *n
	}
	if a != nil {
		avatarURL = *a
	}
	return userID, email, name, avatarURL, role, nil
}

// DeleteSessionByToken revokes a session, e.g. on logout.
func (s *Store) DeleteSessionByToken(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// SetRole overwrites userID's role - either auth bootstrapping ADMIN_EMAIL
// on sign-in, or an admin promoting/demoting someone from the admin board.
func (s *Store) SetRole(ctx context.Context, userID int64, role string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET role = $1 WHERE id = $2`, role, userID)
	return err
}

// RequiredRole looks up which role key currently requires. A key that is
// somehow not seeded (should not happen - see schema.sql) fails closed to
// admin-only rather than silently unlocking.
func (s *Store) RequiredRole(ctx context.Context, key string) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx, `SELECT required_role FROM feature_flags WHERE key = $1`, key).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return feature.RoleAdmin, nil
	}
	return role, err
}

// AdminUser is one row of the admin user-management board.
type AdminUser struct {
	ID        int64     `json:"id"`
	Provider  string    `json:"provider"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
	Villages  int       `json:"villages"`
}

// ListUsers returns every user for the admin board, oldest first.
func (s *Store) ListUsers(ctx context.Context) ([]AdminUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.provider, u.email, u.name, u.role, u.created_at,
		       (SELECT count(*) FROM villages v WHERE v.user_id = u.id)
		FROM users u
		ORDER BY u.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AdminUser
	for rows.Next() {
		var u AdminUser
		var email, name *string
		if err := rows.Scan(&u.ID, &u.Provider, &email, &name, &u.Role, &u.CreatedAt, &u.Villages); err != nil {
			return nil, err
		}
		if email != nil {
			u.Email = *email
		}
		if name != nil {
			u.Name = *name
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountAdmins is how the admin board guards against demoting the last admin
// to zero.
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE role = $1`, feature.RoleAdmin).Scan(&n)
	return n, err
}

// VillageCount and SnapshotCount back the open-signup quotas: 5 villages per
// user, 100 snapshots per village.
func (s *Store) VillageCount(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM villages WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

func (s *Store) SnapshotCount(ctx context.Context, userID int64, tag string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM snapshots sn
		JOIN villages v ON v.id = sn.village_id
		WHERE v.user_id = $1 AND v.tag = $2`, userID, tag).Scan(&n)
	return n, err
}

// UploadsToday counts how many snapshots userID has saved (to any village)
// since midnight UTC, for the daily upload quota.
func (s *Store) UploadsToday(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM snapshots sn
		JOIN villages v ON v.id = sn.village_id
		WHERE v.user_id = $1 AND sn.created_at >= date_trunc('day', now())`, userID).Scan(&n)
	return n, err
}

// Villages lists every village userID has at least one snapshot for, newest
// capture first. The join is intentionally inner, not left: a village row
// with zero snapshots (possible if villageID's upsert succeeded but the
// following Save failed) has nothing to show and must not appear.
func (s *Store) Villages(ctx context.Context, userID int64) ([]store.Village, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT v.tag, MAX(sn.captured_at), count(*)
		FROM villages v
		JOIN snapshots sn ON sn.village_id = v.id
		WHERE v.user_id = $1
		GROUP BY v.id, v.tag
		ORDER BY MAX(sn.captured_at) DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.Village
	for rows.Next() {
		var v store.Village
		var n int64
		if err := rows.Scan(&v.Tag, &v.LastCapturedAt, &n); err != nil {
			return nil, err
		}
		v.Snapshots = int(n)
		out = append(out, v)
	}
	return out, rows.Err()
}

// Prune deletes snapshots older than cutoff, never the newest per village,
// and clears expired sessions while it has the connection open.
func (s *Store) Prune(ctx context.Context, cutoff time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM snapshots
		WHERE captured_at < $1
		  AND id NOT IN (
		    SELECT DISTINCT ON (village_id) id
		    FROM snapshots
		    ORDER BY village_id, captured_at DESC
		  )`, cutoff)
	if err != nil {
		return 0, err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`); err != nil {
		return int(tag.RowsAffected()), err
	}
	return int(tag.RowsAffected()), nil
}
