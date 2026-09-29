package progress

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAcknowledgedPrefixAndBoundedWindow(t *testing.T) {
	r := NewTracker(4)
	ctx := context.Background()
	a, err := r.Track(ctx, "a", 0)
	require.NoError(t, err)
	b, err := r.Track(ctx, "a", 200)
	require.NoError(t, err)
	c, err := r.Track(ctx, "a", 300)
	require.NoError(t, err)
	d, err := r.Track(ctx, "b", 9007199254740993)
	require.NoError(t, err)
	require.Empty(t, r.Snapshot())
	c()
	b()
	b() // Later replies, including duplicates, cannot pass a missing ACK.
	require.Empty(t, r.Snapshot())
	blocked, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err = r.Track(blocked, "a", 400)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	d()
	require.Equal(t, []Position{{Node: "b", Offset: 9007199254740993}}, r.Snapshot())
	a()
	require.Equal(t, []Position{{Node: "a", Offset: 300}, {Node: "b", Offset: 9007199254740993}}, r.Snapshot())
	zero, err := r.Track(ctx, "c", 0)
	require.NoError(t, err)
	zero()
	data, err := json.Marshal(r.Snapshot())
	require.NoError(t, err)
	require.Contains(t, string(data), `"offset":"9007199254740993"`)
	require.Contains(t, string(data), `"offset":"0"`)
}

func TestConcurrentReplies(t *testing.T) {
	r := NewTracker(1000)
	callbacks := make([]func(), 1000)
	for i := range callbacks {
		var err error
		callbacks[i], err = r.Track(context.Background(), "source", int64(i))
		require.NoError(t, err)
	}
	var wg sync.WaitGroup
	for _, ack := range callbacks {
		wg.Add(1)
		go func() { defer wg.Done(); ack(); ack(); r.Snapshot() }()
	}
	wg.Wait()
	require.Equal(t, []Position{{Node: "source", Offset: 999}}, r.Snapshot())
	require.Empty(t, r.slots)
}

func TestSQLiteBufferPeriodicFinalFlushAndRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(Schema)
	require.NoError(t, err)
	r, err := Open(path, "run")
	require.NoError(t, err)
	ack, err := r.Track(context.Background(), "source", 0)
	require.NoError(t, err)
	require.NoError(t, r.Flush())
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM run_offsets").Scan(&count))
	require.Zero(t, count)
	ack()
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM run_offsets").Scan(&count))
	require.Zero(t, count, "ACK must only change memory")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	errs := make(chan error, 10)
	go func() { defer close(done); r.Run(ctx, 20*time.Millisecond, func(err error) { errs <- err }) }()
	require.Eventually(t, func() bool { return db.QueryRow("SELECT COUNT(*) FROM run_offsets").Scan(&count) == nil && count == 1 }, time.Second, 5*time.Millisecond)
	ack, err = r.Track(context.Background(), "source", 9007199254740993)
	require.NoError(t, err)
	ack()
	cancel()
	<-done // Final flush even before the next timer fires.
	require.Empty(t, errs)
	require.NoError(t, r.Close())
	var offset int64
	require.NoError(t, db.QueryRow("SELECT offset FROM run_offsets WHERE run_id='run'").Scan(&offset))
	require.Equal(t, int64(9007199254740993), offset)
	r, err = Open(path, "next-run")
	require.NoError(t, err)
	defer r.Close()
	ack, err = r.Track(context.Background(), "source", 42)
	require.NoError(t, err)
	ack()
	_, err = db.Exec("ALTER TABLE run_offsets RENAME TO temporarily_unavailable")
	require.NoError(t, err)
	require.Error(t, r.Flush())
	_, err = db.Exec("ALTER TABLE temporarily_unavailable RENAME TO run_offsets")
	require.NoError(t, err)
	require.NoError(t, r.Flush())
	require.NoError(t, db.QueryRow("SELECT offset FROM run_offsets WHERE run_id='next-run'").Scan(&offset))
	require.Equal(t, int64(42), offset)
	_, err = Open(filepath.Join(t.TempDir(), "missing.db"), "run")
	require.Error(t, err)
}
