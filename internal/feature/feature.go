// Package feature gates optional capabilities - currently Themes and
// BuildNow - behind a signed-in user's role. Local mode has no accounts and
// wires no Store here at all; every caller in internal/server treats a nil
// Store the same as "everything unlocked," so local mode is untouched by
// any of this.
package feature

import "context"

// Themes and BuildNow are the only two gated capabilities. Core tracking
// itself is never gated - it has no key here at all.
const (
	Themes   = "themes"
	BuildNow = "build_now"
)

// RoleUser is every signed-in account by default. RoleAdmin is the only
// other role, and satisfies every flag - see Unlocked.
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// Keys lists every gated flag, for a caller building the full unlocked set
// (GET /api/features) rather than checking one key at a time.
func Keys() []string { return []string{Themes, BuildNow} }

// Store looks up which role a flag currently requires. Only hosted mode's
// Postgres store implements it.
type Store interface {
	RequiredRole(ctx context.Context, key string) (string, error)
}

// Unlocked reports whether userRole satisfies requiredRole. Admin satisfies
// every flag; a plain user only satisfies a flag that itself requires
// nothing more than RoleUser.
func Unlocked(userRole, requiredRole string) bool {
	return userRole == RoleAdmin || requiredRole == RoleUser
}
