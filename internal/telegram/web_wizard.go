package telegram

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

// wizardErrorText turns a raw gotd/RPC failure into a short, actionable
// message. The wizard renders this verbatim in the UI, so raw transport
// wrappers ("rpcDoRequest: rpc error code 400: …") must never reach the admin.
func wizardErrorText(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "PHONE_CODE_INVALID"):
		return "That login code is incorrect. Check the code Telegram sent you and try again."
	case strings.Contains(msg, "PHONE_CODE_EXPIRED"):
		return "That login code has expired. Request a new one and try again."
	case strings.Contains(msg, "PHONE_CODE_EMPTY") || strings.Contains(msg, "PHONE_CODE_HASH_EMPTY"):
		return "The login code was not accepted. Request a new code and try again."
	case strings.Contains(msg, "PASSWORD_HASH_INVALID"):
		return "Incorrect two-step verification password."
	case strings.Contains(msg, "PHONE_NUMBER_INVALID"):
		return "Telegram rejected that phone number. Use the full international format, e.g. +628123456789."
	case strings.Contains(msg, "PHONE_NUMBER_BANNED"):
		return "That phone number is banned from Telegram."
	case strings.Contains(msg, "PHONE_NUMBER_FLOOD"):
		return "Too many attempts for this number. Wait a while before trying again."
	case strings.Contains(msg, "SESSION_PASSWORD_NEEDED"):
		return "Two-step verification is enabled on this account — enter your password."
	case strings.Contains(msg, "wizard timeout"):
		return msg
	case strings.Contains(msg, "wizard cancelled"):
		return msg
	case strings.Contains(msg, "FLOOD_WAIT"):
		return "Telegram is rate-limiting this account. Wait a few minutes and try again."
	default:
		return "Telegram sign-in failed. Check the server log for details."
	}
}

// webWizardState is an in-memory state machine that walks a single
// browser session through the gotd auth flow. The wizard is *not*
// blocking — each step is a short poll/dispatch cycle. A goroutine
// runs the flow on the backend so the user's browser can poll
// /api/telegram/wizard/status for progress.
type webWizardState struct {
	id       string
	phase    string
	phone    string
	hash     string
	code     string
	password string
	err      string
	flow     auth.Flow
	client   *ClientManager
	mu       sync.Mutex
	done     bool
	success  bool
	created  time.Time
}

var (
	webWizardsMu sync.Mutex
	webWizards   = map[string]*webWizardState{}
)

// WizardStart kicks off a new wizard session for the supplied phone
// number. Returns a wizard ID the browser can poll.
func WizardStart(ctx context.Context, m *ClientManager, phone string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("telegram client not initialized")
	}

	// Only skip the wizard when Telegram itself confirms the stored session is
	// authorized. The cached flag is not trusted here: a stale
	// `telegram_authorized` setting previously made this return a synthetic
	// "done" wizard, so the UI reported success without ever sending a code.
	if m.CheckAuthorized(ctx) {
		// Return a synthetic "already authenticated" wizard so the UI
		// shows the right copy.
		wiz := &webWizardState{
			id:      newWizardID(),
			phase:   "done",
			success: true,
			done:    true,
			created: time.Now(),
		}
		webWizardsMu.Lock()
		webWizards[wiz.id] = wiz
		webWizardsMu.Unlock()
		return wiz.id, nil
	}

	wiz := &webWizardState{
		id:      newWizardID(),
		phone:   phone,
		client:  m,
		phase:   "sending_code",
		created: time.Now(),
	}
	wiz.flow = auth.NewFlow(&webAuthenticator{state: wiz}, auth.SendCodeOptions{})

	webWizardsMu.Lock()
	webWizards[wiz.id] = wiz
	webWizardsMu.Unlock()

	// Detach from the caller's context: the HTTP request context is canceled
	// the moment the start response is written, which would kill the auth
	// flow before the user can submit a code. run() applies its own timeout.
	go wiz.run(context.WithoutCancel(ctx))
	return wiz.id, nil
}

func (w *webWizardState) run(ctx context.Context) {
	flowCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// The MTProto session is already running in the server's background
	// runner, and gotd allows exactly one Run per client. Driving the flow
	// therefore goes through Do(), which dispatches into the live session.
	err := w.client.Do(flowCtx, func(runCtx context.Context) error {
		// gotd exposes the underlying Telegram client as a FlowClient.
		// We drive the flow with our GUI-backed authenticator; the
		// callbacks block until the user submits the matching field.
		return w.flow.Run(runCtx, w.client.client.Auth())
	})

	w.mu.Lock()
	w.done = true
	if err != nil {
		if w.phase != "error" {
			w.phase = "error"
		}
		if w.err == "" {
			// Keep the raw text out of the UI but leave it in the server log.
			log.Printf("telegram wizard %s failed: %v", w.id, err)
			w.err = wizardErrorText(err)
		}
	} else {
		w.success = true
		w.phase = "done"
		w.client.markAuthorized(true)
	}
	w.mu.Unlock()

	// Storage channel discovery follows authentication. Best-effort.
	if err == nil {
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer bgCancel()
		if e := w.client.EnsureStorageChannel(bgCtx); e != nil {
			w.mu.Lock()
			if w.err == "" {
				w.err = "storage channel: " + e.Error()
			}
			w.mu.Unlock()
		}
	}
}

// webAuthenticator implements auth.UserAuthenticator for the browser
// wizard. Each phase pushes the wizard into the corresponding
// "waiting" state so the UI knows what prompt to render.
type webAuthenticator struct {
	state *webWizardState
}

func (w *webAuthenticator) Phone(_ context.Context) (string, error) {
	return w.state.phone, nil
}

func (w *webAuthenticator) Code(_ context.Context, sent *tg.AuthSentCode) (string, error) {
	w.state.mu.Lock()
	w.state.hash = sent.PhoneCodeHash
	w.state.code = ""
	w.state.phase = "waiting_code"
	w.state.mu.Unlock()
	// Block until user submits a code via the wizard API.
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-deadline.C:
			return "", errors.New("wizard timeout: no code submitted")
		case <-ticker.C:
			w.state.mu.Lock()
			code := w.state.code
			done := w.state.done
			if code != "" {
				w.state.code = "" // consume: a retry must not resend the same code
			}
			w.state.mu.Unlock()
			if done {
				return "", errors.New("wizard cancelled")
			}
			if code != "" {
				return code, nil
			}
		}
	}
}

func (w *webAuthenticator) Password(_ context.Context) (string, error) {
	w.state.mu.Lock()
	w.state.password = ""
	w.state.phase = "waiting_password"
	w.state.mu.Unlock()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-deadline.C:
			return "", errors.New("wizard timeout: no 2FA password submitted")
		case <-ticker.C:
			w.state.mu.Lock()
			pw := w.state.password
			done := w.state.done
			if pw != "" {
				w.state.password = ""
			}
			w.state.mu.Unlock()
			if pw != "" {
				return pw, nil
			}
			if done {
				return "", errors.New("wizard cancelled")
			}
		}
	}
}

func (w *webAuthenticator) AcceptTermsOfService(_ context.Context, _ tg.HelpTermsOfService) error {
	return nil
}

func (w *webAuthenticator) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, fmt.Errorf("sign-up not supported: register your Telegram account via the official app first")
}

// webWizardLookup returns a wizard by id or nil.
func WizardLookup(id string) *webWizardState {
	webWizardsMu.Lock()
	defer webWizardsMu.Unlock()
	return webWizards[id]
}

// webWizardSubmit pushes user-supplied data into the wizard buffer and
// returns true if it was successfully enqueued.
func WizardSubmit(id, code, password string) (bool, string) {
	w := WizardLookup(id)
	if w == nil {
		return false, "wizard not found or already completed"
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return false, "wizard already completed"
	}
	switch w.phase {
	case "waiting_code":
		if len(code) < 4 {
			return false, "code is required"
		}
		w.code = code
		return true, ""
	case "waiting_password":
		if password == "" {
			return false, "2FA password is required"
		}
		w.password = password
		return true, ""
	default:
		return false, "wizard is not waiting for that input"
	}
}

// WizardSnapshot is the public view of a wizard for the API.
type WizardSnapshot struct {
	Phase   string `json:"phase"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func WizardSnapshotJSON(id string) WizardSnapshot {
	w := WizardLookup(id)
	if w == nil {
		return WizardSnapshot{Phase: "not_found", Error: "wizard not found or expired"}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return WizardSnapshot{
		Phase:   w.phase,
		Success: w.success,
		Error:   w.err,
	}
}

// webWizardDiscard cleans up any finished wizard older than 10 minutes.
func WizardDiscard(id string) {
	webWizardsMu.Lock()
	defer webWizardsMu.Unlock()
	if w, ok := webWizards[id]; ok {
		if w.done && time.Since(w.created) > 10*time.Minute {
			delete(webWizards, id)
		}
	}
}

func newWizardID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
