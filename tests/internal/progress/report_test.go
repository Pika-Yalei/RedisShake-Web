package progress

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Pika-Yalei/RedisShake-Web/internal/client"
	"github.com/stretchr/testify/require"
)

func TestNodeReportsKeepIndependentHistoriesAndUnknowns(t *testing.T) {
	r := NewTracker(20)
	acknowledge := func(node string, offset int64) {
		r.SetReplicationID(node, "history-"+node)
		ack, err := r.Track(context.Background(), node, offset)
		require.NoError(t, err)
		ack()
	}
	acknowledge("a", 9007199254740993)
	acknowledge("b", 0)
	acknowledge("failed", 100)
	acknowledge("reset", 100)
	acknowledge("ahead", 200)
	acknowledge("promoted", 100)
	acknowledge("forked", 151)
	source := func(node, id string, offset int64) Source {
		return Source{Node: node, ReadMaster: func(context.Context) (client.MasterPosition, error) {
			return client.MasterPosition{Node: node + "-master", Offset: offset, ReplicationID: id, PreviousReplicationID: "history-" + node, SecondReplicationOffset: 150}, nil
		}}
	}
	sources := []Source{source("b", "history-b", 0), source("a", "history-a", 9007199254741093), source("unknown", "history-unknown", 500), source("reset", "new-history", 300), source("ahead", "history-ahead", 100), source("promoted", "new", 300), source("forked", "new", 300)}
	// A reset has no shared previous history.
	sources[3].ReadMaster = func(context.Context) (client.MasterPosition, error) {
		return client.MasterPosition{Offset: 300, ReplicationID: "different"}, nil
	}
	sources = append(sources, Source{Node: "failed", ReadMaster: func(context.Context) (client.MasterPosition, error) {
		return client.MasterPosition{}, errors.New("INFO denied")
	}})
	reports := r.Report(context.Background(), sources)
	byNode := map[string]NodeProgress{}
	for _, p := range reports {
		byNode[p.Node] = p
		require.NotEmpty(t, p.SampledAt)
	}
	require.Equal(t, int64(100), *byNode["a"].Lag)
	require.Equal(t, int64(9007199254740993), *byNode["a"].Offset)
	require.Zero(t, *byNode["b"].Lag)
	require.Zero(t, *byNode["b"].Offset)
	require.Nil(t, byNode["unknown"].Offset)
	require.Nil(t, byNode["unknown"].Lag)
	require.Equal(t, int64(200), *byNode["promoted"].Lag)
	for _, node := range []string{"failed", "reset", "ahead", "forked"} {
		require.Nil(t, byNode[node].Lag)
		require.NotEmpty(t, byNode[node].Error)
	}
	data, err := json.Marshal(reports)
	require.NoError(t, err)
	require.Contains(t, string(data), `"offset":"9007199254740993"`)
	require.Contains(t, string(data), `"lag":"0"`)
	require.Contains(t, string(data), `"lag":null`)
}

func TestSampleConsumptionBeforeMasterQuery(t *testing.T) {
	r := NewTracker(10)
	r.SetReplicationID("node", "history")
	ack, err := r.Track(context.Background(), "node", 100)
	require.NoError(t, err)
	ack()
	reports := r.Report(context.Background(), []Source{{Node: "node", ReadMaster: func(context.Context) (client.MasterPosition, error) {
		ack, err := r.Track(context.Background(), "node", 200)
		require.NoError(t, err)
		ack()
		return client.MasterPosition{Offset: 150, ReplicationID: "history"}, nil
	}}})
	require.Equal(t, int64(100), *reports[0].Offset)
	require.Equal(t, int64(50), *reports[0].Lag)
	require.Equal(t, int64(200), r.Snapshot()[0].Offset)
}

func TestReportRefreshesWithoutConsumptionAndClearsFailedSample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(Schema + ";" + ReportSchema)
	require.NoError(t, err)
	r, err := Open(path, "run")
	require.NoError(t, err)
	defer r.Close()
	r.SetReplicationID("node", "history")
	ack, err := r.Track(context.Background(), "node", 100)
	require.NoError(t, err)
	ack()
	master := int64(100)
	fail := false
	r.Sources = []Source{{Node: "node", ReadMaster: func(context.Context) (client.MasterPosition, error) {
		if fail {
			return client.MasterPosition{}, errors.New("unavailable")
		}
		return client.MasterPosition{Offset: master, ReplicationID: "history"}, nil
	}}}
	read := func() NodeProgress {
		require.NoError(t, r.Flush())
		var payload string
		require.NoError(t, db.QueryRow("SELECT payload FROM run_progress WHERE run_id='run'").Scan(&payload))
		var p []NodeProgress
		require.NoError(t, json.Unmarshal([]byte(payload), &p))
		return p[0]
	}
	require.Zero(t, *read().Lag)
	master = 250
	require.Equal(t, int64(150), *read().Lag)
	fail = true
	failed := read()
	require.Nil(t, failed.Lag)
	require.Nil(t, failed.MasterOffset)
	require.Equal(t, int64(100), *failed.Offset)
	require.Equal(t, "unavailable", failed.Error)
	fail = false
	require.Equal(t, int64(150), *read().Lag)
	var offset int64
	require.NoError(t, db.QueryRow("SELECT offset FROM run_offsets WHERE run_id='run'").Scan(&offset))
	require.Equal(t, int64(100), offset)
}
