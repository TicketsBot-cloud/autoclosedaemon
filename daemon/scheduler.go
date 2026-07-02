package daemon

import (
	"context"
	"sync"
	"time"

	"github.com/TicketsBot/common/closerequest"
	"github.com/TicketsBot/database"
	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// CloseRequestScheduler fires ticket close requests at their exact close_at.
type CloseRequestScheduler struct {
	logger *zap.Logger
	db     *database.Database
	redis  *redis.Client

	mu    sync.Mutex
	armed map[ticketKey]*armedRequest
}

type ticketKey struct {
	GuildId  uint64
	TicketId int
}

type armedRequest struct {
	timer   *time.Timer
	closeAt time.Time
}

func NewCloseRequestScheduler(logger *zap.Logger, db *database.Database, redisClient *redis.Client) *CloseRequestScheduler {
	return &CloseRequestScheduler{
		logger: logger,
		db:     db,
		redis:  redisClient,
		armed:  make(map[ticketKey]*armedRequest),
	}
}

// Reconcile loads all pending close requests and brings the armed timers in
// line with them: arm new ones, re-arm those whose close_at changed, and cancel
// timers whose request is gone (denied, closed, or newly excluded).
func (s *CloseRequestScheduler) Reconcile(ctx context.Context) {
	if err := s.db.CloseRequest.Cleanup(ctx); err != nil {
		s.logger.Error("Error cleaning up close requests", zap.Error(err))
	}

	pending, err := s.getPending(ctx)
	if err != nil {
		s.logger.Error("Error querying pending close requests", zap.Error(err))
		return // keep existing timers; retry next reconcile
	}

	s.mu.Lock()
	snapshot := make(map[ticketKey]time.Time, len(s.armed))
	for key, a := range s.armed {
		snapshot[key] = a.closeAt
	}
	s.mu.Unlock()

	toArm, toCancel := diffReconcile(snapshot, pending)

	for _, key := range toCancel {
		s.remove(key)
	}
	for _, req := range toArm {
		s.arm(req)
	}

	s.logger.Debug("Reconciled close request timers",
		zap.Int("pending", len(pending)),
		zap.Int("armed", len(toArm)),
		zap.Int("cancelled", len(toCancel)),
	)
}

// diffReconcile is the pure decision core: given the currently-armed close times
// and the set of pending requests, decide which to (re-)arm and which to cancel.
func diffReconcile(armed map[ticketKey]time.Time, pending []database.CloseRequest) (toArm []database.CloseRequest, toCancel []ticketKey) {
	pendingKeys := make(map[ticketKey]struct{}, len(pending))
	for _, req := range pending {
		if req.CloseAt == nil {
			continue
		}

		key := ticketKey{req.GuildId, req.TicketId}
		pendingKeys[key] = struct{}{}

		if cur, ok := armed[key]; !ok || !cur.Equal(*req.CloseAt) {
			toArm = append(toArm, req)
		}
	}

	for key := range armed {
		if _, ok := pendingKeys[key]; !ok {
			toCancel = append(toCancel, key)
		}
	}

	return toArm, toCancel
}

// arm schedules (or reschedules) a timer that fires at req.CloseAt.
func (s *CloseRequestScheduler) arm(req database.CloseRequest) {
	if req.CloseAt == nil {
		return
	}

	key := ticketKey{req.GuildId, req.TicketId}
	delay := time.Until(*req.CloseAt)
	if delay < 0 {
		delay = 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.armed[key]; ok {
		existing.timer.Stop()
	}

	s.armed[key] = &armedRequest{
		timer:   time.AfterFunc(delay, func() { s.fire(key) }),
		closeAt: *req.CloseAt,
	}
}

// remove cancels and forgets a timer.
func (s *CloseRequestScheduler) remove(key ticketKey) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.armed[key]; ok {
		existing.timer.Stop()
		delete(s.armed, key)
	}
}

// fire runs when a timer elapses. It re-checks the database before closing so a
// request that was denied, closed, excluded, or pushed further out in the
// meantime is handled correctly.
func (s *CloseRequestScheduler) fire(key ticketKey) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	req, ok, err := s.db.CloseRequest.Get(ctx, key.GuildId, key.TicketId)
	if err != nil {
		s.logger.Error("Error re-fetching close request on fire",
			zap.Uint64("guild", key.GuildId),
			zap.Int("ticket", key.TicketId),
			zap.Error(err),
		)
		// Drop it; the next Reconcile re-arms from the database.
		s.remove(key)
		return
	}

	// Denied/cancelled/closed: the row is gone, so do nothing.
	if !ok || req.CloseAt == nil {
		s.remove(key)
		return
	}

	// close_at was pushed later (e.g. /closerequest re-run) — re-arm for the new time.
	if req.CloseAt.After(time.Now()) {
		s.arm(req)
		return
	}

	s.logger.Info("Closing ticket (close request)",
		zap.Uint64("guild", req.GuildId),
		zap.Int("ticket", req.TicketId),
		zap.Timep("close_at", req.CloseAt),
	)

	if err := closerequest.PublishMessage(s.redis, req); err != nil {
		s.logger.Error("Error publishing close request to workers",
			zap.Uint64("guild", req.GuildId),
			zap.Int("ticket", req.TicketId),
			zap.Error(err),
		)
	}

	s.remove(key)
}

// getPending returns every open, non-excluded close request that has a close_at,
// regardless of whether it is due yet. Mirrors GetCloseable without the time filter.
func (s *CloseRequestScheduler) getPending(ctx context.Context) ([]database.CloseRequest, error) {
	query := `
SELECT close_request.guild_id, close_request.ticket_id, close_request.user_id, close_request.close_at, close_request.close_reason
FROM close_request
INNER JOIN tickets
	ON tickets.guild_id = close_request.guild_id AND tickets.id = close_request.ticket_id
LEFT JOIN auto_close_exclude exclude
	ON close_request.guild_id = exclude.guild_id AND close_request.ticket_id = exclude.ticket_id
WHERE
	close_request.close_at IS NOT NULL
	AND
	exclude.guild_id IS NULL
	AND
	tickets.open
;
`

	rows, err := s.db.CloseRequest.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []database.CloseRequest
	for rows.Next() {
		var request database.CloseRequest
		if err := rows.Scan(&request.GuildId, &request.TicketId, &request.UserId, &request.CloseAt, &request.Reason); err != nil {
			return nil, err
		}

		requests = append(requests, request)
	}

	return requests, rows.Err()
}
