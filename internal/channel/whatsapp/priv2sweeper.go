package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// PRIV2Sweeper purges `whatsmeow_message_secrets` rows for senders that are
// NOT on each A0 tenant's customer allowlist (and not the operator's own
// self-chat). Implements spec §5.7 PRIV-2 / OQ-1 RESOLVED.
//
// Why it matters: in A0 mode whatsmeow decrypts every message that arrives
// on the operator's personal account — including friends, groups, anything
// — and stores per-message decryption keys in `whatsmeow_message_secrets`.
// Victoria's app layer drops the bodies, but those keys persist until
// purged. With the keys + relay-side message data, bodies can in principle
// be reconstructed. The sweep bounds the worst-case window during which
// that's possible to `retention_minutes` (default 30; cadence = 15 min).
type PRIV2Sweeper struct {
	pool    *pgxpool.Pool
	log     waLog.Logger
	cadence time.Duration
}

// NewPRIV2Sweeper builds a sweeper bound to the shared Postgres pool.
// Cadence defaults to 15 minutes (half of the 30-min retention default).
func NewPRIV2Sweeper(pool *pgxpool.Pool, log waLog.Logger) *PRIV2Sweeper {
	if log == nil {
		log = waLog.Noop
	}
	return &PRIV2Sweeper{pool: pool, log: log, cadence: 15 * time.Minute}
}

// SetCadence overrides the default sweep interval. Useful for tests.
func (s *PRIV2Sweeper) SetCadence(d time.Duration) {
	if d > 0 {
		s.cadence = d
	}
}

// Run blocks until ctx is cancelled, sweeping every cadence. Errors are
// logged but never propagated — the sweep is best-effort and a single
// failed pass shouldn't stop later passes.
func (s *PRIV2Sweeper) Run(ctx context.Context) {
	if err := s.SweepNow(ctx); err != nil {
		s.log.Warnf("priv2 sweep startup pass: %v", err)
	}
	t := time.NewTicker(s.cadence)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.SweepNow(ctx); err != nil {
				s.log.Warnf("priv2 sweep: %v", err)
			}
		}
	}
}

// SweepNow runs one pass synchronously. Returns the total number of rows
// deleted across all A0 tenants. Exposed for tests + manual ops.
func (s *PRIV2Sweeper) SweepNow(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
		SELECT data->>'inbound_mode'    AS inbound_mode,
		       data->>'operator_jid'    AS operator_jid,
		       data->>'provider_number' AS provider_number,
		       data->'customer_allowlist' AS allowlist
		FROM channel_bindings
		WHERE channel = 'whatsapp'`)
	if err != nil {
		return fmt.Errorf("priv2: list bindings: %w", err)
	}
	defer rows.Close()

	var totalDeleted int64
	for rows.Next() {
		var mode, operatorJID, providerNumber string
		var allowlistJSON []byte
		if err := rows.Scan(&mode, &operatorJID, &providerNumber, &allowlistJSON); err != nil {
			return fmt.Errorf("priv2: scan: %w", err)
		}
		// Only A0 (read_only) tenants need this — A1 only ever sees customer
		// traffic on its dedicated number, no personal exposure to purge.
		if mode != "read_only" {
			continue
		}
		device := normalizeJIDString(operatorJID)
		if device == "" {
			device = normalizeJIDString(providerNumber)
		}
		if device == "" {
			continue
		}
		var allowlist []string
		if len(allowlistJSON) > 0 {
			_ = json.Unmarshal(allowlistJSON, &allowlist)
		}
		// Keep: operator's own JID (Message Yourself / draft delivery chat)
		// + every customer JID the operator has explicitly allowlisted.
		keep := []string{device}
		for _, j := range allowlist {
			if jn := normalizeJIDString(j); jn != "" {
				keep = append(keep, jn)
			}
		}
		n, err := s.purgeForDevice(ctx, device, keep)
		if err != nil {
			s.log.Warnf("priv2 device=%s: %v", device, err)
			continue
		}
		if n > 0 {
			s.log.Infof("priv2 device=%s purged=%d (kept allowlist of %d)", device, n, len(keep))
		}
		totalDeleted += n
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("priv2: iterate bindings: %w", err)
	}
	if totalDeleted > 0 {
		s.log.Infof("priv2 sweep complete: total purged %d rows", totalDeleted)
	}
	return nil
}

// purgeForDevice deletes message_secrets rows for the given device whose
// chat_jid is NOT in the keep list. Group chats (`@g.us` JIDs) are never
// in the keep list, so their secrets always get purged.
func (s *PRIV2Sweeper) purgeForDevice(ctx context.Context, deviceJID string, keep []string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM whatsmeow_message_secrets
		WHERE our_jid = $1 AND chat_jid <> ALL($2)`,
		deviceJID, keep)
	if err != nil {
		// Tolerate "table does not exist" gracefully — a fresh deployment
		// without whatsmeow yet won't have the table.
		if strings.Contains(err.Error(), "whatsmeow_message_secrets") &&
			strings.Contains(err.Error(), "does not exist") {
			return 0, nil
		}
		return 0, err
	}
	return tag.RowsAffected(), nil
}
