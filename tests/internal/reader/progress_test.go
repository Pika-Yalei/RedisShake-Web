package reader

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestClusterProgressPreservesEverySourceNode(t *testing.T) {
	r := &syncClusterReader{readers: []Reader{
		&syncStandaloneReader{opts: &SyncReaderOptions{Address: "node-a:6379"}},
		&syncStandaloneReader{opts: &SyncReaderOptions{Address: "node-b:6379"}},
	}}
	sources := r.ProgressSources()
	require.Len(t, sources, 2)
	require.Equal(t, "node-a:6379", sources[0].Node)
	require.Equal(t, "node-b:6379", sources[1].Node)
	require.NotNil(t, sources[0].ReadMaster)
	require.NotNil(t, sources[1].ReadMaster)
}
