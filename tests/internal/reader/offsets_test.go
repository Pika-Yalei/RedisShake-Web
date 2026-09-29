package reader

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pika-Yalei/RedisShake-Web/internal/entry"
	"github.com/stretchr/testify/require"
)

func TestConsumedCommandBoundariesExcludeReadAhead(t *testing.T) {
	dir := t.TempDir()
	commands := [][]string{{"SELECT", "2"}, {"SET", "a", "one"}, {"PING"}, {"MULTI"}, {"SET", "b", "two"}, {"EXEC"}}
	var data []byte
	var ends []int64
	const start = int64(9007199254740993)
	for _, argv := range commands {
		data = append(data, (&entry.Entry{Argv: argv}).Serialize()...)
		ends = append(ends, start+int64(len(data)))
	}
	// All commands fit in a single buffered read. Only one may count at a time.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "0.aof"), data, 0600))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &syncStandaloneReader{ctx: ctx, ch: make(chan *entry.Entry), trackOffsets: true, stat: syncStandaloneReaderStat{Address: "source:6379", Dir: dir}}
	done := make(chan struct{})
	go func() { defer close(done); r.sendAOF(start) }()
	for i := range commands {
		select {
		case e := <-r.ch:
			require.Equal(t, ends[i], e.SourceOffset)
			require.Equal(t, "source:6379", e.SourceNode)
			if i == 1 || i == 4 {
				require.False(t, e.ProgressOnly)
				require.Equal(t, 2, e.DbId)
			} else {
				require.True(t, e.ProgressOnly)
			}
		case <-time.After(time.Second):
			t.Fatal("missing command progress")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader did not stop")
	}
}
