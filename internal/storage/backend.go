package storage

import (
	"context"
	"io"

	"tdocs/internal/db"
	"tdocs/internal/telegram"
)

// Backend abstracts the object store behind tDocs. The production
// implementation is Telegram MTProto (*telegram.ClientManager); S3/MinIO or
// local-disk backends can be added later by implementing this interface — the
// web layer must only depend on Backend, never on a concrete client.
//
// Design notes for future backends:
//   - Message IDs are opaque per-backend object addresses. For Telegram they
//     are Storage Channel message IDs; an S3 backend could use version IDs.
//   - File references/coordinates stay backend-internal; the catalog stores
//     whatever strings the backend returns at CompleteUpload time.
type Backend interface {
	// Do runs fn inside the backend's active session/connection scope.
	Do(ctx context.Context, fn func(ctx context.Context) error) error

	// IsAuthorized reports the last live authorization check.
	IsAuthorized() bool

	// Health runs a real end-to-end verification of the backend:
	// reachability of the remote API, validity of the stored credentials and
	// accessibility of the backing bucket/channel. Implementations must
	// contact the real service — cached settings alone must never be enough
	// to report a healthy backend.
	Health(ctx context.Context) error

	// EnsureStorageChannel verifies (and if necessary provisions) the
	// backing vault/bucket.
	EnsureStorageChannel(ctx context.Context) error

	// SyncChannelToDB rebuilds the local catalog from stored objects.
	SyncChannelToDB(ctx context.Context) (*telegram.SyncResult, error)

	// SyncAndRecord runs SyncChannelToDB and persists the outcome to the
	// sync history table.
	SyncAndRecord(ctx context.Context, trigger string) (*telegram.SyncResult, error)

	// ResolveChannelDocument returns fresh coordinates for a stored object.
	ResolveChannelDocument(ctx context.Context, msgID int) (*telegram.ChannelDocument, error)

	// UploadPart stores one erasure/part segment of an in-flight upload.
	UploadPart(ctx context.Context, fileID int64, partIndex, totalParts int, data []byte, limiter *telegram.SafeLimiter) error

	// CompleteUpload commits uploaded parts as one stored object and
	// returns its message ID plus backend coordinates.
	CompleteUpload(ctx context.Context, fileID int64, totalParts int, fileName, mimeType string) (int, int64, int64, error)

	// DownloadFullByMessage / DownloadRangeByMessage stream object bytes,
	// resolving fresh coordinates first.
	DownloadFullByMessage(ctx context.Context, msgID int, fallbackID, fallbackHash int64, w io.Writer) error
	DownloadRangeByMessage(ctx context.Context, msgID int, fallbackID, fallbackHash int64, start, end int64, w io.Writer, limiter *telegram.SafeLimiter) error

	// Snapshot lifecycle for database backups stored on the backend.
	ListSnapshots(ctx context.Context) ([]telegram.SnapshotInfo, error)
	UploadSnapshot(ctx context.Context, gzPath string, limiter *telegram.SafeLimiter) (int, error)
	RestoreSnapshotByID(ctx context.Context, msgID int, destDBPath string) error
	DownloadSnapshotByID(ctx context.Context, msgID int, w io.Writer) error
	DeleteSnapshotByID(ctx context.Context, msgID int) error

	// SetDB swaps the metadata handle (e.g. after point-in-time restore).
	SetDB(database *db.DB)

	// DeleteChannelMessage removes one object message (purge support).
	DeleteChannelMessage(ctx context.Context, msgID int) error
}
