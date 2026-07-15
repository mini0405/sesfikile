package boarding

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrCodeNotFound covers both "no such code was ever issued" and "the code's
// record has already been swept as expired" — deliberately not distinguished
// from the caller's point of view (see PassStore.Lookup's doc comment) so an
// unknown code and a long-expired one fail identically and don't leak which
// codes were ever real.
var ErrCodeNotFound = errors.New("boarding code not found")

// IssuedPass is a stored short-code -> signed-pass-token mapping (an airline
// PNR-style handle). The signed token remains the canonical, verified
// artifact — this row only exists so POST /boarding/scan can resolve an
// 8-character code back to it before running the token through the exact
// same Stage 5 verification path.
type IssuedPass struct {
	Code       string
	PassToken  string
	Nonce      string
	CommuterID uuid.UUID
	RouteID    uuid.UUID
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

// PassStore persists issued short-code -> pass-token mappings in Postgres
// (not in-memory, unlike Stage 4/6's telemetry/stop-request state) — see the
// migration's comment for why: a boarding pass is a financial artifact worth
// surviving a server restart mid-demo and being auditable afterward, even
// though it's short-lived.
type PassStore struct {
	pool *pgxpool.Pool
}

func NewPassStore(pool *pgxpool.Pool) *PassStore {
	return &PassStore{pool: pool}
}

// sweepGrace is how far past a code's own expiry CleanupExpired/opportunistic
// sweeps wait before actually deleting its row. The pass TTL is ~3 minutes;
// waiting well beyond that before deleting means a driver who scans a code
// shortly after it expired still gets the informative "pass has expired"
// (410) response — driven by the stored token's own Verify+Expired check —
// rather than the row having already vanished and reading as "unknown code."
const sweepGrace = 10 * time.Minute

// SweepGrace is the exported form of sweepGrace, for callers outside this
// package that report on the same cutoff (cmd/cleanup's dry-run count).
const SweepGrace = sweepGrace

// SweepCutoff returns the timestamp used by both Lookup's opportunistic
// sweep and CleanupExpired: rows expired before this instant are eligible
// for deletion.
func SweepCutoff() time.Time {
	return time.Now().Add(-sweepGrace)
}

// ExpiredCodesCountQuery counts boarding_pass_codes rows expired before the
// given timestamp (use SweepCutoff()) — exposed for cmd/cleanup's dry-run
// report, which shows a count without deleting anything.
const ExpiredCodesCountQuery = `SELECT count(*) FROM boarding_pass_codes WHERE expires_at < $1`

// Store inserts a new issued-pass row. Returns an error satisfying
// IsUniqueViolation if the code collides with an existing one (astronomically
// unlikely at 1.1e12 combinations, but IssuePass retries on it rather than
// assuming it can never happen).
func (s *PassStore) Store(ctx context.Context, p IssuedPass) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO boarding_pass_codes (code, pass_token, nonce, commuter_id, route_id, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		p.Code, p.PassToken, p.Nonce, p.CommuterID, p.RouteID, p.ExpiresAt,
	)
	return err
}

// IsUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505) — used by IssuePass to retry code generation on
// the vanishingly rare collision, without hardcoding the pgx error shape
// anywhere else in the package.
func IsUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

// Lookup resolves a short code (normalized: case-insensitive, hyphen/whitespace-
// tolerant) back to its stored pass. It does NOT itself check expiry — the
// caller runs the returned PassToken through the same Signer.Verify +
// PassPayload.Expired path used for a directly-submitted token, so an
// expired-but-known code fails with the exact same 410 a stale token would.
// An unknown code returns ErrCodeNotFound, which callers must map to the
// same response as a tampered/invalid token (401) — never a distinct
// "no such code" message, so brute-forcing can't distinguish a live code
// space from an exhausted one.
func (s *PassStore) Lookup(ctx context.Context, rawCode string) (IssuedPass, error) {
	// Opportunistic sweep of long-expired rows. Runs before the real lookup
	// but only ever removes rows already well past sweepGrace, so it can
	// never delete the very row this call is about to look up (that row, if
	// expired at all, expired at most a few minutes ago — see sweepGrace).
	_, _ = s.pool.Exec(ctx, `DELETE FROM boarding_pass_codes WHERE expires_at < $1`, SweepCutoff())

	code := NormalizeCode(rawCode)
	var p IssuedPass
	err := s.pool.QueryRow(ctx,
		`SELECT code, pass_token, nonce, commuter_id, route_id, expires_at, created_at
		 FROM boarding_pass_codes WHERE code = $1`,
		code,
	).Scan(&p.Code, &p.PassToken, &p.Nonce, &p.CommuterID, &p.RouteID, &p.ExpiresAt, &p.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IssuedPass{}, ErrCodeNotFound
		}
		return IssuedPass{}, err
	}
	return p, nil
}

// CleanupExpired deletes every boarding_pass_codes row expired more than
// sweepGrace ago. Lookup already does this opportunistically on every scan-by-
// code, so this is only needed to reclaim rows for codes that were issued but
// never scanned again (the common case) — exposed separately so it can also
// be run standalone (e.g. from a periodic job or cmd/cleanup) without waiting
// for the next code-based scan to trigger it.
func (s *PassStore) CleanupExpired(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM boarding_pass_codes WHERE expires_at < $1`, time.Now().Add(-sweepGrace))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
