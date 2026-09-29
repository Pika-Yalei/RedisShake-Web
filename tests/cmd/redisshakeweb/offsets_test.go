package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Pika-Yalei/RedisShake-Web/internal/client"
	"github.com/Pika-Yalei/RedisShake-Web/internal/progress"
	"github.com/stretchr/testify/require"
)

func TestRunsLoadPersistedConsumedOffsets(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir)
	require.NoError(t, err)
	for _, id := range []string{"old", "new"} {
		_, err = s.db.Exec("INSERT INTO runs(id,task_id,status,phase,started_at) VALUES(?, 'task', 'STOPPED', 'INCREMENTAL', ?)", id, map[string]string{"old": "2026-09-28", "new": "2026-09-29"}[id])
		require.NoError(t, err)
	}
	r, err := progress.Open(filepath.Join(dir, "app.db"), "new")
	require.NoError(t, err)
	for node, offset := range map[string]int64{"a:6379": 9007199254740993, "b:6379": 0} {
		ack, err := r.Track(context.Background(), node, offset)
		require.NoError(t, err)
		ack()
	}
	r.SetReplicationID("a:6379", "history")
	r.Sources = []progress.Source{
		{Node: "a:6379", ReadMaster: func(context.Context) (client.MasterPosition, error) {
			return client.MasterPosition{Node: "a:6379", Offset: 9007199254741093, ReplicationID: "history"}, nil
		}},
		{Node: "b:6379", ReadMaster: func(context.Context) (client.MasterPosition, error) {
			return client.MasterPosition{Node: "b:6379", Offset: 0}, nil
		}},
	}
	require.NoError(t, r.Flush())
	require.NoError(t, r.Close())
	require.NoError(t, s.db.Close())
	s, err = openStore(dir)
	require.NoError(t, err)
	defer s.db.Close()
	runs, err := s.runs("task")
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, "new", runs[0].ID)
	require.Equal(t, []progress.Position{{Node: "a:6379", Offset: 9007199254740993}, {Node: "b:6379", Offset: 0}}, runs[0].ConsumedOffsets)
	require.Empty(t, runs[1].ConsumedOffsets)
	require.Empty(t, runs[1].NodeProgress)
	require.Len(t, runs[0].NodeProgress, 2)
	require.Equal(t, int64(100), *runs[0].NodeProgress[0].Lag)
	require.Equal(t, int64(9007199254740993), *runs[0].NodeProgress[0].Offset)
	require.Nil(t, runs[0].NodeProgress[1].Lag)
	data, err := json.Marshal(runs)
	require.NoError(t, err)
	require.Contains(t, string(data), `"offset":"9007199254740993"`)
	require.Contains(t, string(data), `"consumedOffsets":[]`)
	runs, err = s.runs("other-task")
	require.NoError(t, err)
	require.Empty(t, runs)
	// Runs created in the same second must still put the newest attempt first.
	_, err = s.db.Exec("UPDATE runs SET started_at='2026-09-29'")
	require.NoError(t, err)
	runs, err = s.runs("task")
	require.NoError(t, err)
	require.Equal(t, "new", runs[0].ID)
}
