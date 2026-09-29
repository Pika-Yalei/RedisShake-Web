package entry

import (
	"sync"
	"sync/atomic"
)

// SplitCompletion waits for every transformed command or broadcast target.
// Each returned callback is idempotent, so duplicate replies cannot advance it.
func SplitCompletion(done func(), count int) []func() {
	if done == nil {
		return nil
	}
	callbacks := make([]func(), count)
	if count == 0 {
		done()
		return callbacks
	}
	var remaining atomic.Int64
	remaining.Store(int64(count))
	for i := range callbacks {
		var once sync.Once
		callbacks[i] = func() {
			once.Do(func() {
				if remaining.Add(-1) == 0 {
					done()
				}
			})
		}
	}
	return callbacks
}
