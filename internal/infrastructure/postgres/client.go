package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/example/subscription-reconciler/internal/infrastructure/postgres/gen"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql
)

// Client wraps the generated sqlc Queries and exposes the Database interface.
type Client struct {
	db      *sql.DB
	queries *gen.Queries
}

var _ ExpirationReconciler = (*Client)(nil)

// Connect opens a database/sql connection using the pgx stdlib driver and
// returns a Client ready to use. Callers must defer Client.Close().
func Connect(ctx context.Context, dsn string) (*Client, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	return &Client{
		db:      db,
		queries: gen.New(db),
	}, nil
}

// Close closes the underlying database connection pool.
func (c *Client) Close() {
	if c.db != nil {
		c.db.Close()
	}
}

// Health pings the database.
func (c *Client) Health(ctx context.Context) error {
	return c.db.PingContext(ctx)
}

// ---------------------------------------------------------------------------
// EntitlementRepository
// ---------------------------------------------------------------------------

func (c *Client) GetEntitlementByUserAndSource(ctx context.Context, userID string, source string) (*Entitlement, error) {
	row, err := c.queries.GetEntitlementByUserAndSource(ctx, gen.GetEntitlementByUserAndSourceParams{
		UserID: userID,
		Source: source,
	})
	if err != nil {
		return nil, err
	}
	return entitlementFromGen(row), nil
}

func (c *Client) GetEntitlementsByUser(ctx context.Context, userID string) ([]Entitlement, error) {
	rows, err := c.queries.GetEntitlementsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Entitlement, len(rows))
	for i, r := range rows {
		out[i] = *entitlementFromGen(r)
	}
	return out, nil
}

func (c *Client) UpsertEntitlement(ctx context.Context, userID string, source string, active bool, expiresAt *time.Time, reason *string, lastEventTime int64, triggeringEventID *string) (bool, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	q := c.queries.WithTx(tx)

	// 1. Fetch current entitlement state
	var prevActive *bool
	var prevExpiresAt *time.Time
	var existingLastEventTime int64

	row, err := q.GetEntitlementByUserAndSource(ctx, gen.GetEntitlementByUserAndSourceParams{
		UserID: userID,
		Source: source,
	})
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == nil {
		ent := entitlementFromGen(row)
		prevActive = &ent.Active
		prevExpiresAt = ent.ExpiresAt
		existingLastEventTime = ent.LastEventTime
	}

	// 2. If new event is not newer than existing, skip update
	if err == nil && lastEventTime <= existingLastEventTime {
		return false, nil
	}

	// 3. Perform upsert
	err = q.UpsertEntitlement(ctx, gen.UpsertEntitlementParams{
		UserID:        userID,
		Source:        source,
		Active:        active,
		ExpiresAt:     nullTime(expiresAt),
		Reason:        nullString(reason),
		LastEventTime: lastEventTime,
	})
	if err != nil {
		return false, err
	}

	// 4. Insert audit log
	err = q.InsertAuditLog(ctx, gen.InsertAuditLogParams{
		UserID:            userID,
		Source:            source,
		PreviousActive:    nullBool(prevActive),
		NextActive:        active,
		PreviousExpiresAt: nullTime(prevExpiresAt),
		NextExpiresAt:     nullTime(expiresAt),
		TriggeringEventID: nullString(triggeringEventID),
		Reason:            nullString(reason),
	})
	if err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) UpdateEntitlementCarrierPolledAt(ctx context.Context, userID string, source string) error {
	return c.queries.UpdateEntitlementCarrierPolledAt(ctx, gen.UpdateEntitlementCarrierPolledAtParams{
		UserID: userID,
		Source: source,
	})
}

func (c *Client) GetLastEventTimeFromStore(ctx context.Context, userID string) (int64, error) {
	v, err := c.queries.GetLastEventTimeFromStore(ctx, userID)
	if err != nil {
		return 0, err
	}
	// COALESCE returns int64 from the DB driver; assert safely.
	switch val := v.(type) {
	case int64:
		return val, nil
	case nil:
		return 0, nil
	default:
		return 0, nil
	}
}

func (c *Client) GetEntitlementsExpiringWithin24h(ctx context.Context) ([]Entitlement, error) {
	rows, err := c.queries.GetEntitlementsExpiringWithin24h(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Entitlement, len(rows))
	for i, r := range rows {
		out[i] = *entitlementFromGen(r)
	}
	return out, nil
}

// GetExpiredEntitlementsForReconciliation lists expired candidates for
// repository implementations that do not provide the atomic reconciler. The
// concrete PostgreSQL client uses ReconcileExpiredEntitlements below so row
// locks are held through the projection and audit writes.
func (c *Client) GetExpiredEntitlementsForReconciliation(ctx context.Context, limit int32) ([]Entitlement, error) {
	rows, err := c.queries.ListExpiredEntitlementsForReconciliation(ctx, limit)
	if err != nil {
		return nil, err
	}

	out := make([]Entitlement, len(rows))
	for i, r := range rows {
		out[i] = *entitlementFromGen(r)
	}
	return out, nil
}

// ReconcileExpiredEntitlements atomically claims and expires one batch of
// entitlements. Selected rows remain locked until both the projection update
// and its EXPIRATION audit row are committed.
func (c *Client) ReconcileExpiredEntitlements(ctx context.Context, limit int32) (int32, error) {
	if limit <= 0 {
		return 0, nil
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	q := c.queries.WithTx(tx)
	rows, err := q.GetExpiredEntitlementsForReconciliation(ctx, limit)
	if err != nil {
		return 0, err
	}

	reason := "EXPIRATION"
	var reconciled int32
	for _, row := range rows {
		if !row.ExpiresAt.Valid {
			continue
		}

		expirationTime := row.ExpiresAt.Time
		affected, err := q.ExpireEntitlement(ctx, gen.ExpireEntitlementParams{
			UserID:        row.UserID,
			Source:        row.Source,
			Reason:        nullString(&reason),
			LastEventTime: expirationTime.UnixMilli(),
		})
		if err != nil {
			return 0, err
		}
		if affected == 0 {
			continue
		}

		previousActive := true
		if err := q.InsertAuditLog(ctx, gen.InsertAuditLogParams{
			UserID:            row.UserID,
			Source:            row.Source,
			PreviousActive:    nullBool(&previousActive),
			NextActive:        false,
			PreviousExpiresAt: row.ExpiresAt,
			NextExpiresAt:     sql.NullTime{},
			TriggeringEventID: sql.NullString{},
			Reason:            nullString(&reason),
		}); err != nil {
			return 0, err
		}

		reconciled++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return reconciled, nil
}

func (c *Client) GetCarrierEntitlementsForPolling(ctx context.Context, limit int32) ([]Entitlement, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	q := c.queries.WithTx(tx)

	rows, err := q.GetCarrierEntitlementsForPolling(ctx, limit)
	if err != nil {
		return nil, err
	}

	out := make([]Entitlement, len(rows))
	for i, r := range rows {
		out[i] = *entitlementFromGen(r)

		err = q.UpdateEntitlementCarrierPolledAt(ctx, gen.UpdateEntitlementCarrierPolledAtParams{
			UserID: r.UserID,
			Source: "CARRIER",
		})
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return out, nil
}

// ---------------------------------------------------------------------------
// StoreEventRepository
// ---------------------------------------------------------------------------

func (c *Client) InsertStoreEvent(ctx context.Context, eventID string, userID string, eventType string, eventTimeMs int64, productID *string) (bool, error) {
	result, err := c.queries.InsertStoreEvent(ctx, gen.InsertStoreEventParams{
		EventID:     eventID,
		UserID:      userID,
		Type:        eventType,
		EventTimeMs: eventTimeMs,
		ProductID:   nullString(productID),
	})
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (c *Client) GetStoreEventsByUser(ctx context.Context, userID string) ([]StoreEvent, error) {
	rows, err := c.queries.GetStoreEventsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]StoreEvent, len(rows))
	for i, r := range rows {
		out[i] = storeEventFromGen(r)
	}
	return out, nil
}

func (c *Client) GetStoreEventByID(ctx context.Context, eventID string) (*StoreEvent, error) {
	row, err := c.queries.GetStoreEventByID(ctx, eventID)
	if err != nil {
		return nil, err
	}
	e := storeEventFromGen(row)
	return &e, nil
}

// ---------------------------------------------------------------------------
// MarketplaceRevocationRepository
// ---------------------------------------------------------------------------

func (c *Client) InsertMarketplaceRevocation(ctx context.Context, eventID string, userID string) (bool, error) {
	result, err := c.queries.InsertMarketplaceRevocation(ctx, gen.InsertMarketplaceRevocationParams{
		EventID: eventID,
		UserID:  userID,
	})
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (c *Client) GetMarketplaceRevocationByEventID(ctx context.Context, eventID string) (*MarketplaceRevocation, error) {
	row, err := c.queries.GetMarketplaceRevocationByEventID(ctx, eventID)
	if err != nil {
		return nil, err
	}
	return &MarketplaceRevocation{
		ID:        row.ID,
		EventID:   row.EventID,
		UserID:    row.UserID,
		CreatedAt: row.CreatedAt,
	}, nil
}

// ---------------------------------------------------------------------------
// ProcessedEventRepository
// ---------------------------------------------------------------------------

func (c *Client) IsEventProcessed(ctx context.Context, eventID string, source string) (bool, error) {
	return c.queries.IsEventProcessed(ctx, gen.IsEventProcessedParams{
		EventID: eventID,
		Source:  source,
	})
}

func (c *Client) MarkEventProcessed(ctx context.Context, eventID string, source string) error {
	return c.queries.MarkEventProcessed(ctx, gen.MarkEventProcessedParams{
		EventID: eventID,
		Source:  source,
	})
}

func (c *Client) GetProcessedEvent(ctx context.Context, eventID string, source string) (*ProcessedEvent, error) {
	row, err := c.queries.GetProcessedEvent(ctx, gen.GetProcessedEventParams{
		EventID: eventID,
		Source:  source,
	})
	if err != nil {
		return nil, err
	}
	return &ProcessedEvent{
		EventID:     row.EventID,
		Source:      row.Source,
		ProcessedAt: row.ProcessedAt,
	}, nil
}

// ---------------------------------------------------------------------------
// NotificationRepository
// ---------------------------------------------------------------------------

func (c *Client) ScheduleNotification(ctx context.Context, userID string, notificationType string, scheduledFor time.Time) error {
	return c.queries.ScheduleNotification(ctx, gen.ScheduleNotificationParams{
		UserID:       userID,
		Type:         notificationType,
		ScheduledFor: scheduledFor,
	})
}

func (c *Client) GetDueNotifications(ctx context.Context, limit int32) ([]Notification, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	q := c.queries.WithTx(tx)

	rows, err := q.GetDueNotifications(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Notification, len(rows))
	for i, r := range rows {
		out[i] = notificationFromGen(r)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) MarkNotificationSent(ctx context.Context, id int64) error {
	return c.queries.MarkNotificationSent(ctx, id)
}

func (c *Client) GetNotificationByUserTypeAndDate(ctx context.Context, userID string, notificationType string, date time.Time) (*Notification, error) {
	row, err := c.queries.GetNotificationByUserTypeAndDate(ctx, gen.GetNotificationByUserTypeAndDateParams{
		UserID: userID,
		Type:   notificationType,
		Date:   date, // interface{}: pgx/stdlib accepts time.Time directly for DATE($3)
	})
	if err != nil {
		return nil, err
	}
	n := notificationFromGen(row)
	return &n, nil
}

// ---------------------------------------------------------------------------
// AuditLogRepository
// ---------------------------------------------------------------------------

func (c *Client) InsertAuditLog(ctx context.Context, userID string, source string, previousActive *bool, nextActive bool, previousExpiresAt *time.Time, nextExpiresAt *time.Time, triggeringEventID *string, reason *string) error {
	return c.queries.InsertAuditLog(ctx, gen.InsertAuditLogParams{
		UserID:            userID,
		Source:            source,
		PreviousActive:    nullBool(previousActive),
		NextActive:        nextActive,
		PreviousExpiresAt: nullTime(previousExpiresAt),
		NextExpiresAt:     nullTime(nextExpiresAt),
		TriggeringEventID: nullString(triggeringEventID),
		Reason:            nullString(reason),
	})
}

func (c *Client) GetAuditLogsByUser(ctx context.Context, userID string, limit int32, offset int32) ([]AuditLog, error) {
	rows, err := c.queries.GetAuditLogsByUser(ctx, gen.GetAuditLogsByUserParams{
		UserID: userID,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AuditLog, len(rows))
	for i, r := range rows {
		out[i] = auditLogFromGen(r)
	}
	return out, nil
}

func (c *Client) CountAuditLogsByUser(ctx context.Context, userID string) (int64, error) {
	return c.queries.CountAuditLogsByUser(ctx, userID)
}

// ---------------------------------------------------------------------------
// Type conversion helpers: gen (sql.Null*) <-> application (*T pointers)
// ---------------------------------------------------------------------------

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullBool(b *bool) sql.NullBool {
	if b == nil {
		return sql.NullBool{}
	}
	return sql.NullBool{Bool: *b, Valid: true}
}

func ptrTime(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}

func ptrString(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	s := n.String
	return &s
}

func ptrBool(n sql.NullBool) *bool {
	if !n.Valid {
		return nil
	}
	b := n.Bool
	return &b
}

func entitlementFromGen(r gen.UserEntitlement) *Entitlement {
	return &Entitlement{
		UserID:          r.UserID,
		Source:          r.Source,
		Active:          r.Active,
		ExpiresAt:       ptrTime(r.ExpiresAt),
		Reason:          ptrString(r.Reason),
		UpdatedAt:       r.UpdatedAt,
		LastEventTime:   r.LastEventTime,
		CarrierPolledAt: ptrTime(r.CarrierPolledAt),
	}
}

func storeEventFromGen(r gen.StoreEvent) StoreEvent {
	return StoreEvent{
		ID:          r.ID,
		EventID:     r.EventID,
		UserID:      r.UserID,
		Type:        r.Type,
		EventTimeMs: r.EventTimeMs,
		ProductID:   ptrString(r.ProductID),
		CreatedAt:   r.CreatedAt,
	}
}

func notificationFromGen(r gen.Notification) Notification {
	return Notification{
		ID:           r.ID,
		UserID:       r.UserID,
		Type:         r.Type,
		ScheduledFor: r.ScheduledFor,
		SentAt:       ptrTime(r.SentAt),
		CreatedAt:    r.CreatedAt,
	}
}

func auditLogFromGen(r gen.AuditLog) AuditLog {
	return AuditLog{
		ID:                r.ID,
		UserID:            r.UserID,
		Source:            r.Source,
		PreviousActive:    ptrBool(r.PreviousActive),
		NextActive:        r.NextActive,
		PreviousExpiresAt: ptrTime(r.PreviousExpiresAt),
		NextExpiresAt:     ptrTime(r.NextExpiresAt),
		TriggeringEventID: ptrString(r.TriggeringEventID),
		Reason:            ptrString(r.Reason),
		CreatedAt:         r.CreatedAt,
	}
}
