package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors so the web layer can map a failed verification onto an
// honest connection state instead of guessing from error strings.
var (
	// ErrNotConfigured means APP_ID/APP_HASH are absent: tDocs has never been
	// pointed at a Telegram API application.
	ErrNotConfigured = errors.New("telegram api credentials not configured (set TDOCS_TG_APP_ID / TDOCS_TG_APP_HASH, then restart)")

	// ErrNotAuthorized means credentials exist but no usable MTProto session
	// is available (never logged in, session expired, or secret rotated).
	ErrNotAuthorized = errors.New("telegram session is not authorized (run `tdocs login` and restart)")

	// ErrStorageChannelUnavailable means the account is authenticated but the
	// Storage Channel could not be resolved, so objects cannot be stored.
	ErrStorageChannelUnavailable = errors.New("telegram storage channel is not accessible")
)

// Health performs a live verification of the Telegram storage backend.
//
// It deliberately does three real network operations in order:
//
//  1. auth.Status — is the stored session still authorized?
//  2. EnsureStorageChannel — is a Storage Channel bound or discoverable?
//  3. ResolveChannelByID — does that channel actually respond over MTProto?
//
// A backend that merely has credentials configured in the database fails this
// check, which is what keeps the dashboard from claiming "connected" when the
// account is not usable.
func (m *ClientManager) Health(ctx context.Context) error {
	if m == nil {
		return ErrNotConfigured
	}
	if m.appID == 0 || m.appHash == "" {
		return ErrNotConfigured
	}

	err := m.Do(ctx, func(runCtx context.Context) error {
		if !m.CheckAuthorized(runCtx) {
			return ErrNotAuthorized
		}
		if err := m.EnsureStorageChannel(runCtx); err != nil {
			return fmt.Errorf("%w: %v", ErrStorageChannelUnavailable, err)
		}
		channelID, _, err := m.StorageChannel()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrStorageChannelUnavailable, err)
		}
		if _, err := m.ResolveChannelByID(runCtx, channelID); err != nil {
			return fmt.Errorf("%w: %v", ErrStorageChannelUnavailable, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// ClassifyHealth maps a Health/Do error onto a coarse, non-secret reason.
// Callers render this verbatim, so it must never leak Telegram internals.
func ClassifyHealth(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNotConfigured):
		return "Telegram API credentials are not configured on this server."
	case errors.Is(err, ErrNotAuthorized):
		return "Telegram is configured but this server is not authenticated. Run `tdocs login`, then restart."
	case errors.Is(err, ErrStorageChannelUnavailable):
		return "Telegram is authenticated but the Storage Channel is not accessible."
	default:
		// A call that could not reach the session at all is an auth problem
		// from the operator's point of view — never surface the raw RPC text.
		if strings.Contains(err.Error(), "is not running") {
			return "Telegram is configured but this server is not authenticated. Run `tdocs login`, then restart."
		}
		return "Telegram could not be verified right now. Check the server log and retry."
	}
}
