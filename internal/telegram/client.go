package telegram

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"tdocs/internal/db"
)

type ClientManager struct {
	client    *telegram.Client
	api       *tg.Client
	db        *db.DB
	appID     int
	appHash   string
	secretKey string
	storage   *EncryptedSessionStorage

	mu                  sync.Mutex
	channelID           int64
	accessHash          int64
	configuredChannelID int64

	// docCache memoises live-resolved object references so media playback
	// (many Range requests per file) does not pay an MTProto round-trip per
	// request. Guarded by docCacheMu; entries expire per docCacheTTL.
	docCacheMu sync.Mutex
	docCache   map[int]docCacheEntry

	// runOnce guards the single long-lived gotd Client.Run. gotd's Run is NOT
	// re-entrant: each call resets the client context and tears down
	// sub-connections, so concurrent Run calls corrupt transfers. Instead of
	// calling Run per request, request handlers dispatch work to the one
	// running session through taskCh.
	runMu   sync.Mutex
	running bool
	taskCh  chan runTask

	// authorized is the last live Auth().Status outcome. Presence of session
	// bytes alone proves nothing: gotd persists session state even for
	// never-authorized runs.
	authorized atomic.Bool
}

type runTask struct {
	fn   func(context.Context) error
	done chan error
}

// NewClientManager configures a new MTProto client instance with Safe Mode client headers.
func NewClientManager(database *db.DB, appID int, appHash, secretKey string) *ClientManager {
	storage := NewEncryptedSessionStorage(database, secretKey)

	// Organic client fingerprinting per ToS safe mode
	device := telegram.DeviceConfig{
		DeviceModel:    "PC 64bit",
		SystemVersion:  "Linux/x86_64",
		AppVersion:     "5.0.0",
		LangCode:       "en",
		SystemLangCode: "en",
	}

	client := telegram.NewClient(appID, appHash, telegram.Options{
		SessionStorage: storage,
		Device:         device,
	})

	return &ClientManager{
		client:    client,
		api:       tg.NewClient(client),
		db:        database,
		appID:     appID,
		appHash:   appHash,
		secretKey: secretKey,
		storage:   storage,
		taskCh:    make(chan runTask),
	}
}

// Client returns the underlying gotd telegram.Client.
func (m *ClientManager) Client() *telegram.Client {
	return m.client
}

// API returns the raw tg.Client for MTProto RPC calls.
func (m *ClientManager) API() *tg.Client {
	return m.api
}

// Run starts the single long-lived gotd session and services dispatched tasks
// until ctx is canceled. It must be called exactly once per ClientManager.
func (m *ClientManager) Run(ctx context.Context, f func(ctx context.Context) error) error {
	m.runMu.Lock()
	if m.running {
		m.runMu.Unlock()
		return fmt.Errorf("telegram client is already running; use Do() for request-scoped calls")
	}
	m.running = true
	m.runMu.Unlock()
	defer func() {
		m.runMu.Lock()
		m.running = false
		m.runMu.Unlock()
	}()

	return m.client.Run(ctx, func(runCtx context.Context) error {
		// Service dispatched request-scoped calls concurrently with the
		// caller's own setup callback (e.g. EnsureStorageChannel).
		taskDone := make(chan struct{})
		go func() {
			defer close(taskDone)
			m.serveTasks(runCtx)
		}()
		defer func() { <-taskDone }()
		return f(runCtx)
	})
}

// serveTasks drains the dispatch channel until the session context ends. Each
// task runs inside runCtx, which is the sole active gotd session context.
// A panic in one task is contained so the session goroutine stays alive.
func (m *ClientManager) serveTasks(runCtx context.Context) {
	for {
		select {
		case <-runCtx.Done():
			return
		case task, ok := <-m.taskCh:
			if !ok {
				return
			}
			task.done <- safeRun(task.fn, runCtx)
		}
	}
}

// safeRun executes fn, recovering from any panic so a single bad request
// (e.g. a nil dereference in the Telegram response path) cannot bring down
// the entire session goroutine.
func safeRun(fn func(context.Context) error, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("telegram task panic: %v", r)
		}
	}()
	return fn(ctx)
}

// Do runs fn inside the active gotd session. It is the request-scoped
// counterpart to Run and is safe to call from HTTP handlers / background
// jobs. It returns an error when the session is not running (never logged in)
// or when fn fails.
func (m *ClientManager) Do(ctx context.Context, fn func(context.Context) error) error {
	m.runMu.Lock()
	running := m.running
	m.runMu.Unlock()
	if !running {
		return fmt.Errorf("telegram client is not running (run `tdocs login` first)")
	}

	done := make(chan error, 1)
	select {
	case m.taskCh <- runTask{fn: fn, done: done}:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SetStorageChannel caches the active storage channel credentials.
func (m *ClientManager) SetStorageChannel(channelID, accessHash int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channelID = channelID
	m.accessHash = accessHash
}

// SetConfiguredChannelID sets an explicit storage channel ID from configuration or env var.
func (m *ClientManager) SetConfiguredChannelID(channelID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configuredChannelID = channelID
}

// SetDB updates the database reference (e.g. after snapshot restoration).
func (m *ClientManager) SetDB(database *db.DB) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.db = database
}

// StorageChannel returns the active storage channel channel_id and access_hash.
func (m *ClientManager) StorageChannel() (int64, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.channelID == 0 {
		return 0, 0, fmt.Errorf("storage channel not initialized")
	}
	return m.channelID, m.accessHash, nil
}
