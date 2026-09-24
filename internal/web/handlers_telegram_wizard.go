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
// /api/telegram/wizard/cancel  POST {id}             → {ok}
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
		// JSON (not plain-text http.Error): the dashboard parses the body
		// and a non-JSON 503 used to surface as generic "Failed to start wizard".
		configured := s.telegramConfigured()
		hint := "Run `tdocs setup` on the server (API_ID + API_HASH from my.telegram.org), then run `tdocs login` and restart tdocs."
		code := "telegram_not_configured"
		if configured {
			hint = "API credentials are stored, but the server started before `tdocs login` saved a session. Stop tdocs, run `sudo tdocs login` with the SAME TDOCS_SECRET_KEY / data dir as the service, then restart tdocs."
			code = "telegram_session_missing"
		}
		jsonOut(w, http.StatusServiceUnavailable, map[string]any{
			"error": "telegram client not initialized on server",
			"code":  code,
			"hint":  hint,
		})
		return
	}
	id, err := s.startWebWizard(r.Context(), phone)
	if err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
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
		jsonOut(w, http.StatusBadRequest, map[string]any{"error": "missing id"})
		return
	}
	snap := s.snapshotWebWizard(id)
	// Snapshot first: reclaiming a terminal wizard must never affect the
	// answer. WizardDiscard only drops wizards that finished over ten
	// minutes ago, so a live one is never removed here.
	if snap.Phase == "done" || snap.Phase == "error" || snap.Phase == "not_found" {
		s.discardWebWizard(id)
	}
	jsonOut(w, http.StatusOK, snap)
}

func (s *Server) handleTelegramWizardSubmit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if body.ID == "" {
		jsonOut(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "missing id"})
		return
	}
	ok, errStr := s.submitWebWizard(body.ID, body.Code, body.Password)
	if !ok {
		// JSON on purpose: the dashboard reads this body and a plain-text
		// http.Error surfaced as "bad response" instead of the real reason.
		jsonOut(w, http.StatusBadRequest, map[string]any{"ok": false, "error": errStr})
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTelegramWizardCancel abandons an in-flight wizard so the admin can
// request a fresh login code without waiting for the current one to expire.
func (s *Server) handleTelegramWizardCancel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if body.ID == "" {
		jsonOut(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "missing id"})
		return
	}
	s.cancelWebWizard(body.ID)
	jsonOut(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTelegramWizardDiscard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	s.cancelWebWizard(body.ID)
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
