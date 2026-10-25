package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"tdocs/internal/db"
	"tdocs/internal/telegram"
)

func TestSessionWatcherDoesNotCancelLocalSessionWrites(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	storage := telegram.NewEncryptedSessionStorage(database, "test-secret")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := storage.StoreSession(ctx, []byte("before-auth")); err != nil {
		t.Fatal(err)
	}
	written := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		written <- storage.StoreSession(ctx, []byte("during-auth"))
	}()
	watchCtx, stopWatch := context.WithTimeout(ctx, 4*time.Second)
	defer stopWatch()
	watchSessionChange(watchCtx, storage, cancel)
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("session watcher canceled authentication after this process saved its own session")
	}
}

func TestSessionWatcherCancelsForExternalSession(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := telegram.NewEncryptedSessionStorage(database, "test-secret")
	if err := server.StoreSession(ctx, []byte("server-session")); err != nil {
		t.Fatal(err)
	}
	cli := telegram.NewEncryptedSessionStorage(database, "test-secret")
	if err := cli.StoreSession(ctx, []byte("cli-session")); err != nil {
		t.Fatal(err)
	}
	watchCtx, stopWatch := context.WithTimeout(ctx, 4*time.Second)
	defer stopWatch()
	watchSessionChange(watchCtx, server, cancel)
	if ctx.Err() == nil {
		t.Fatal("session watcher did not reconnect for the external CLI session")
	}
}
