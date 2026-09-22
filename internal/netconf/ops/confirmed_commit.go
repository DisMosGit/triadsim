package ops

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/DisMosGit/triadsim/internal/clock"
)

// DefaultConfirmTimeout is the confirmed-commit timeout used when the client
// sends no <confirm-timeout> (RFC 4741 §8.4.1: 600 seconds).
const DefaultConfirmTimeout = 600 * time.Second

// Confirmed tracks the single confirmed commit in progress.
//
// RFC 4741 §8.4.1 defines the confirmed commit: the configuration is applied
// immediately, but it is reverted to its state before the commit unless a
// confirming commit arrives within the timeout. A follow-up confirmed commit
// resets the timer; the session that issued the confirmed commit ending before
// a confirming commit reverts it at once.
//
// The server creates one Confirmed and passes it to every session through
// Deps, because the candidate and running datastores are shared by all of them.
type Confirmed struct {
	clk  clock.Clock
	mu   sync.Mutex
	next uint64
	pend *pendingCommit
}

// pendingCommit is the confirmed commit waiting for a confirming commit.
type pendingCommit struct {
	// seq identifies this confirmed commit, so a timer that fires after the
	// commit was confirmed or replaced cannot roll anything back.
	seq uint64
	// sessionID is the session that issued the confirmed commit. Only that
	// session ending reverts the commit.
	sessionID uint32
	// timer fires the rollback when the confirm timeout expires.
	timer clock.Timer
	// rollback restores running and startup to the snapshot taken before the
	// confirmed commit was applied.
	rollback func(context.Context) error
}

// ConfirmedRequest carries what Confirmed.Apply needs from the operation layer.
type ConfirmedRequest struct {
	// SessionID is the session issuing the confirmed commit.
	SessionID uint32
	// Timeout is the confirm timeout; a non-positive value means
	// DefaultConfirmTimeout.
	Timeout time.Duration
	// Rollback restores the configuration as it was before the confirmed
	// commit. It runs with a context that is not cancelled by the end of the
	// session or the RPC that armed it.
	Rollback func(context.Context) error
}

// NewConfirmed returns a confirmed-commit state for clk. A nil clk means
// clock.RealClock{}.
func NewConfirmed(clk clock.Clock) *Confirmed {
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &Confirmed{clk: clk}
}

// Apply runs one confirmed commit. apply validates the candidate and applies it
// to running; it runs under the Confirmed lock, so a second session cannot
// interleave a confirmed commit with this one.
//
// A confirmed commit issued by another session while one is pending is refused
// with access-denied: the simulator can only revert one unconfirmed
// configuration. A follow-up confirmed commit from the same session applies its
// changes too, but keeps the rollback target of the first confirmed commit and
// only restarts the timer. A failed apply leaves any previous confirmed commit
// armed and nothing new armed.
func (c *Confirmed) Apply(ctx context.Context, req ConfirmedRequest, apply func(context.Context) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pend != nil && c.pend.sessionID != req.SessionID {
		return Denied("a confirmed commit from another session is already in progress")
	}
	if err := apply(ctx); err != nil {
		return err
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = DefaultConfirmTimeout
	}
	if c.pend != nil {
		c.pend.timer.Reset(timeout)
		return nil
	}

	c.next++
	seq := c.next
	c.pend = &pendingCommit{
		seq:       seq,
		sessionID: req.SessionID,
		rollback:  req.Rollback,
	}
	c.pend.timer = c.clk.AfterFunc(timeout, func() { c.fire(seq) })
	return nil
}

// Confirm ends a pending confirmed commit without reverting it. It is called
// after a plain <commit> applied successfully, which is the confirming commit;
// it is a no-op when no confirmed commit is in progress.
func (c *Confirmed) Confirm() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pend == nil {
		return
	}
	c.pend.timer.Stop()
	c.pend = nil
}

// SessionEnded reverts the confirmed commit of a session that ended before a
// confirming commit arrived (RFC 4741 §8.4.1). A confirmed commit of another
// session is left alone. The returned error is the rollback failure, which the
// caller logs: the client is gone and cannot be answered.
func (c *Confirmed) SessionEnded(ctx context.Context, sessionID uint32) error {
	c.mu.Lock()
	if c.pend == nil || c.pend.sessionID != sessionID {
		c.mu.Unlock()
		return nil
	}
	rollback := c.pend.rollback
	c.pend.timer.Stop()
	c.pend = nil
	c.mu.Unlock()

	// The session context is already cancelled; the rollback must still run and
	// persist the restored configuration.
	return runRollback(context.WithoutCancel(ctx), rollback)
}

// Close stops the rollback timer without reverting anything. It is for
// shutdown: every session has already ended and reverted its own confirmed
// commit, so only a stray timer can be left.
func (c *Confirmed) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pend == nil {
		return
	}
	c.pend.timer.Stop()
	c.pend = nil
}

// Pending reports whether a confirmed commit is waiting for confirmation.
func (c *Confirmed) Pending() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.pend != nil
}

// fire is the timeout callback: it reverts the confirmed commit unless it was
// confirmed or replaced in the meantime.
func (c *Confirmed) fire(seq uint64) {
	c.mu.Lock()
	if c.pend == nil || c.pend.seq != seq {
		c.mu.Unlock()
		return
	}
	rollback := c.pend.rollback
	c.pend = nil
	c.mu.Unlock()

	if err := runRollback(context.Background(), rollback); err != nil {
		slog.ErrorContext(context.Background(), "netconf: confirmed-commit rollback failed", "error", err)
	}
}

// runRollback restores the configuration through rollback.
func runRollback(ctx context.Context, rollback func(context.Context) error) error {
	if rollback == nil {
		return nil
	}
	return rollback(ctx)
}
