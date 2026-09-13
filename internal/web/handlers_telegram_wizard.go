package web

import (
	"encoding/json"
	"net/http"
	"strings"
)

// ------------------------------------------------------------ telegram wizard ---

// /api/telegram/wizard/start   POST {phone} → {id}
// /api/telegram/wizard/status  GET  ?id=…  → {phase, success, error}
// /api/telegram/wizard/submit  POST {id, code, password} → {ok, error}
// /api/telegram/wizard/discard POST {id}             → {ok}

func (s *Server) handleTelegramWizardStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	phone := strings.TrimSpace(body.Phone)
	if phone == "" {
		http.Error(w, "phone is required", http.StatusBadRequest)
		return
	}
	if s.tg == nil {
		http.Error(w, "telegram client not initialized on server", http.StatusServiceUnavailable)
		return
	}
	id, err := s.startWebWizard(r.Context(), phone)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = s.db.Audit("admin", "telegram_wizard_start", phone, clientIP(r))
	// Report the real starting phase so the UI never assumes a code was sent:
	// a still-valid session yields "done" without any SMS.
	snap := s.snapshotWebWizard(id)
	jsonOut(w, http.StatusOK, map[string]any{
		"id":      id,
		"phase":   snap.Phase,
		"success": snap.Success,
		"error":   snap.Error,
	})
}

func (s *Server) handleTelegramWizardStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	s.discardWebWizard(id)
	jsonOut(w, http.StatusOK, s.snapshotWebWizard(id))
}

func (s *Server) handleTelegramWizardSubmit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.ID == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	ok, errStr := s.submitWebWizard(body.ID, body.Code, body.Password)
	if !ok {
		http.Error(w, errStr, http.StatusBadRequest)
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTelegramWizardDiscard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	s.discardWebWizard(body.ID)
	jsonOut(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTelegramWizardNeeded(w http.ResponseWriter, r *http.Request) {
	jsonOut(w, http.StatusOK, map[string]any{
		"app_id_set":          s.cfg.TelegramAppID != 0,
		"app_hash_set":        s.cfg.TelegramAppHash != "",
		"configured":          s.telegramConfigured(),
		"client_available":    s.tg != nil,
		"telegram_authorized": s.telegramAuthorized(),
		"storage_channel":     s.telegramStorageChannel(),
		"superuser":           true, // reaching this handler already proves it
	})
}

// handleTelegramState reports the verified backend state. Cheap by default
// (cached probe); ?live=1 forces a real end-to-end check.
func (s *Server) handleTelegramState(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("live") == "1"
	state, detail := s.telegramState(r.Context(), force)
	last := s.lastTelegramHealth()
	checks := map[string]bool{
		"api_credentials":   s.telegramConfigured(),
		"authentication":    state == TgConnected,
		"storage_channel":   s.telegramStorageChannel(),
		"index_healthy":     state == TgConnected,
		"session_persisted": s.telegramSessionPersisted(),
	}
	jsonOut(w, http.StatusOK, map[string]any{
		"state":       string(state),
		"message":     detail,
		"verified_at": last.At,
		"checks":      checks,
	})
}

// handleTelegramTest performs the explicit, real connection test used by the
// setup wizard's verification step. It never reports success unless the
// backend probe actually contacted Telegram and the Storage Channel answered.
func (s *Server) handleTelegramTest(w http.ResponseWriter, r *http.Request) {
	state, detail := s.telegramState(r.Context(), true)
	s.audit(r, "telegram_health_check", string(state))
	status := http.StatusOK
	if state != TgConnected {
		status = http.StatusServiceUnavailable
	}
	jsonOut(w, status, map[string]any{
		"ok":      state == TgConnected,
		"state":   string(state),
		"message": detail,
		"checks": map[string]bool{
			"api_reachable":     s.telegramConfigured(),
			"authentication":    state == TgConnected || state == TgDisconnected,
			"storage_channel":   state == TgConnected,
			"session_persisted": s.telegramSessionPersisted(),
		},
	})
}

func jsonOut(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// Thin wrapper to avoid importing telegram package in handler file scope;
// these delegate to the package-level helpers added by handlers_telegram_wizard.go.
