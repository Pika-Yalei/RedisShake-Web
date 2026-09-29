package progress

import (
	"context"
	"database/sql"
	"net/url"
	"time"

	_ "modernc.org/sqlite"
)

const FlushInterval = 5 * time.Second
const Schema = `CREATE TABLE IF NOT EXISTS run_offsets (
	run_id TEXT NOT NULL, node TEXT NOT NULL, offset INTEGER NOT NULL,
	PRIMARY KEY(run_id, node))`

type Recorder struct {
	*Tracker
	db    *sql.DB
	runID string
	saved map[string]int64
}

// Open uses the application's existing database; a wrong path must not silently
// create a separate database in the task's working directory.
func Open(path, runID string) (*Recorder, error) {
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", "rw")
	q.Set("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	rows, err := db.Query("SELECT run_id FROM run_offsets LIMIT 0")
	if err != nil {
		db.Close()
		return nil, err
	}
	rows.Close()
	return &Recorder{Tracker: NewTracker(8192), db: db, runID: runID, saved: make(map[string]int64)}, nil
}

func (r *Recorder) Close() error { return r.db.Close() }

// Flush is called by one persistence goroutine, never by command reply handlers.
// Failed transactions leave saved unchanged and are retried on the next tick.
func (r *Recorder) Flush() error {
	var changed []Position
	for _, p := range r.Snapshot() {
		if previous, ok := r.saved[p.Node]; !ok || previous != p.Offset {
			changed = append(changed, p)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range changed {
		_, err = tx.Exec(`INSERT INTO run_offsets(run_id,node,offset) VALUES(?,?,?)
			ON CONFLICT(run_id,node) DO UPDATE SET offset=excluded.offset`, r.runID, p.Node, p.Offset)
		if err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	for _, p := range changed {
		r.saved[p.Node] = p.Offset
	}
	return nil
}

// Run persists periodically and once more after the writer has drained and its
// owner cancels ctx. It deliberately does not share the reader's stop context.
func (r *Recorder) Run(ctx context.Context, interval time.Duration, onError func(error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	flush := func() {
		if err := r.Flush(); err != nil {
			onError(err)
		}
	}
	for {
		select {
		case <-ticker.C:
			flush()
		case <-ctx.Done():
			flush()
			return
		}
	}
}
