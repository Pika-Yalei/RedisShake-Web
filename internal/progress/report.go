package progress

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/Pika-Yalei/RedisShake-Web/internal/client"
)

const ReportSchema = `CREATE TABLE IF NOT EXISTS run_progress (
	run_id TEXT PRIMARY KEY, payload TEXT NOT NULL)`

// NodeProgress is calculated and reported by the task, never by the Web server.
// All offsets are decimal strings over JSON, including nullable unknown values.
type NodeProgress struct {
	Node         string `json:"node"`
	Offset       *int64 `json:"offset,string"`
	MasterNode   string `json:"masterNode,omitempty"`
	MasterOffset *int64 `json:"masterOffset,string"`
	Lag          *int64 `json:"lag,string"`
	SampledAt    string `json:"sampledAt"`
	Error        string `json:"error,omitempty"`
}

type Source struct {
	Node       string
	ReadMaster func(context.Context) (client.MasterPosition, error)
}

func (t *Tracker) Report(ctx context.Context, sources []Source) []NodeProgress {
	reports := make([]NodeProgress, len(sources))
	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Add(1)
		go func(i int, src Source) {
			defer wg.Done()
			p := NodeProgress{Node: src.Node}
			// Snapshot consumption before querying master: ACKs can advance while
			// INFO is in flight, so reading them afterwards can produce negative lag.
			t.mu.Lock()
			var replID string
			if s := t.sources[src.Node]; s != nil {
				replID = s.replID
				if s.known {
					offset := s.offset
					p.Offset = &offset
				}
			}
			t.mu.Unlock()
			master, err := src.ReadMaster(ctx)
			p.SampledAt = time.Now().UTC().Format(time.RFC3339Nano)
			if err != nil {
				p.Error = err.Error()
			} else {
				p.MasterNode, p.MasterOffset = master.Node, &master.Offset
				if p.Offset != nil {
					compatible := replID != "" && (replID == master.ReplicationID ||
						(replID == master.PreviousReplicationID && *p.Offset <= master.SecondReplicationOffset))
					if !compatible {
						p.Error = "复制历史已变化，位点暂不可比较"
					} else if *p.Offset < 0 || master.Offset < *p.Offset {
						p.Error = "master 位点小于消费位点，等待重新采集"
					} else {
						lag := master.Offset - *p.Offset
						p.Lag = &lag
					}
				}
			}
			reports[i] = p
		}(i, src)
	}
	wg.Wait()
	sort.Slice(reports, func(i, j int) bool { return reports[i].Node < reports[j].Node })
	return reports
}
