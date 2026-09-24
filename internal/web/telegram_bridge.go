package web

import (
	"context"
	"errors"
	"net/http"
	"time"

	"tdocs/internal/telegram"
)

// startWebWizard kicks off the browser-based Telegram auth wizard.
// The wizard walks through phone → SMS code → optional 2FA password
// using non-blocking server polling.
func (s *Server) startWebWizard(ctx context.Context, phone string) (string, error) {
	if s.tg == nil {
		return "", errTelegramNotReady{}
	}
	mgr, ok := s.tg.(*telegram.ClientManager)
	if !ok || mgr == nil {
		return "", errTelegramNotReady{}
	}
	return telegram.WizardStart(ctx, mgr, phone)
}

// snapshotWebWizard returns the latest phase + error state of a wizard.
func (s *Server) snapshotWebWizard(id string) telegram.WizardSnapshot {
	return telegram.WizardSnapshotJSON(id)
}

// submitWebWizard pushes user-entered code or password into the wizard.
func (s *Server) submitWebWizard(id, code, password string) (bool, string) {
	return telegram.WizardSubmit(id, code, password)
}

// discardWebWizard cleans up a finished/expired wizard.
func (s *Server) discardWebWizard(id string) {
	telegram.WizardDiscard(id)
}

// cancelWebWizard abandons an in-flight wizard so a fresh code request starts
// from a clean phase instead of "wizard is not waiting for that input".
func (s *Server) cancelWebWizard(id string) {
	telegram.WizardCancel(id)
}

// telegramAuthorized reports whether the paired Telegram account is
// currently authenticated (used by the setup-wizard banner).
func (s *Server) telegramAuthorized() bool {
	if s.tg == nil {
		return false
	}
	mgr, ok := s.tg.(*telegram.ClientManager)
	if !ok || mgr == nil {
		return false
	}
	return mgr.IsAuthorized()
}

// telegramStorageChannel reports whether a Storage Channel is bound.
func (s *Server) telegramStorageChannel() bool {
	_, err := s.db.GetSetting("storage_channel_id")
	return err == nil
}

// ------------------------------------------------------- honest state ---

// TelegramState is the verified status of the Telegram storage backend.
// The dashboard only renders CONNECTED after a real network check succeeded;
// configuration values alone are never enough to claim a working connection.
type TelegramState string

const (
	TgNotConfigured TelegramState = "not_configured"
	TgAuthRequired  TelegramState = "authentication_required"
	TgConnecting    TelegramState = "connecting"
	TgConnected     TelegramState = "connected"
	TgDisconnected  TelegramState = "disconnected"
	TgError         TelegramState = "error"
)

// tgHealthTTL bounds how often a passive page load may hit the Telegram API.
// An explicit "Test Connection" always bypasses the cache.
const tgHealthTTL = 30 * time.Second

type tgHealthSnapshot struct {
	State  TelegramState
	Detail string
	At     time.Time
}

// telegramConfigured reports whether this server has Telegram API credentials
// (from the environment, .env or the settings written by the setup wizard).
func (s *Server) telegramConfigured() bool {
	if s.cfg != nil && s.cfg.TelegramAppID != 0 && s.cfg.TelegramAppHash != "" {
		return true
	}
	id, errID := s.db.GetSetting("telegram_app_id")
	hash, errHash := s.db.GetSetting("telegram_app_hash")
	return errID == nil && errHash == nil && id != "" && hash != ""
}

// telegramState returns the current connection state.
//
// force=true performs a live verification (used by "Test Connection" and by
// the wizard's final step); force=false reuses a recent successful probe so
// ordinary page loads do not hammer the Telegram API.
func (s *Server) telegramState(ctx context.Context, force bool) (TelegramState, string) {
	if !s.telegramConfigured() {
		return TgNotConfigured, "Telegram API credentials are not configured on this server."
	}
	if s.tg == nil {
		return TgNotConfigured, "Telegram storage backend is not initialized."
	}

	s.tgHealthMu.Lock()
	cached := s.tgHealth
	s.tgHealthMu.Unlock()
	if !force && cached.State == TgConnected && time.Since(cached.At) < tgHealthTTL {
		return cached.State, cached.Detail
	}

	if !s.tg.IsAuthorized() {
		state, detail := TgAuthRequired, "Telegram is configured but this server is not authenticated. Run `tdocs login`, then restart."
		s.recordTelegramHealth(state, detail)
		return state, detail
	}

	probeCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	err := s.tg.Health(probeCtx)
	if err == nil {
		s.recordTelegramHealth(TgConnected, "")
		return TgConnected, ""
	}

	state := TgError
	switch {
	case errors.Is(err, telegram.ErrNotConfigured):
		state = TgNotConfigured
	case errors.Is(err, telegram.ErrNotAuthorized):
		state = TgAuthRequired
	case errors.Is(err, telegram.ErrStorageChannelUnavailable):
		state = TgDisconnected
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		state = TgDisconnected
	}
	s.recordTelegramHealth(state, telegram.ClassifyHealth(err))
	return state, telegram.ClassifyHealth(err)
}

func (s *Server) recordTelegramHealth(state TelegramState, detail string) {
	s.tgHealthMu.Lock()
	s.tgHealth = tgHealthSnapshot{State: state, Detail: detail, At: time.Now()}
	s.tgHealthMu.Unlock()
}

// lastTelegramHealth exposes the last recorded state without touching the
// network (for cheap UI status renders).
func (s *Server) lastTelegramHealth() tgHealthSnapshot {
	s.tgHealthMu.Lock()
	defer s.tgHealthMu.Unlock()
	return s.tgHealth
}

// requireTelegram writes an honest 503 JSON payload when the storage backend
// cannot serve a request, so callers never see a raw 500 or a fake success.
// It returns false when the caller must stop handling the request.
func (s *Server) requireTelegram(w http.ResponseWriter, r *http.Request) bool {
	state, detail := s.telegramState(r.Context(), false)
	if state == TgConnected {
		return true
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error":   "telegram_storage_unavailable",
		"state":   string(state),
		"message": detail,
	})
	return false
}

// classifyHealth keeps handler files free of a direct telegram import while
// still producing operator-safe (non-secret) failure text.
var classifyHealth = telegram.ClassifyHealth

// telegramUnavailableState maps a backend error onto a coarse connection
// state for the UI. Mirrors telegramState's classification so handlers and the
// status endpoint never disagree.
func telegramUnavailableState(err error) TelegramState {
	switch {
	case err == nil:
		return TgConnected
	case errors.Is(err, telegram.ErrNotConfigured):
		return TgNotConfigured
	case errors.Is(err, telegram.ErrNotAuthorized):
		return TgAuthRequired
	case errors.Is(err, telegram.ErrStorageChannelUnavailable):
		return TgDisconnected
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return TgDisconnected
	default:
		return TgError
	}
}

// telegramSessionPersisted reports whether an encrypted MTProto session is
// stored, i.e. a restart will not require a fresh pairing.
func (s *Server) telegramSessionPersisted() bool {
	v, err := s.db.GetSetting("telegram_session")
	return err == nil && v != ""
}

// errTelegramNotReady signals the server is missing APP_ID/APP_HASH or
// the Telegram client wasn't loaded. Surfaced to the wizard UI so the
// admin gets an actionable message.
type errTelegramNotReady struct{}

func (errTelegramNotReady) Error() string {
	return "telegram client not initialized on server (set TDOCS_TG_APP_ID / TDOCS_TG_APP_HASH, then restart)"
}
