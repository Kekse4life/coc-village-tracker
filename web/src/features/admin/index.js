// Admin has no feature_flags row of its own - it is gated directly by
// role === 'admin' (see /api/me), not by an unlockable flag like themes or
// build_now.
export { AdminBoard } from './AdminBoard.jsx'
