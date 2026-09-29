package entry

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCompletionWaitsForEveryOutputAndTarget(t *testing.T) {
	completed := 0
	outputs := SplitCompletion(func() { completed++ }, 2)
	targets := SplitCompletion(outputs[0], 3)
	outputs[1]()
	targets[2]()
	targets[0]()
	targets[0]()
	require.Zero(t, completed)
	targets[1]()
	require.Equal(t, 1, completed)
	outputs[0]()
	outputs[1]()
	require.Equal(t, 1, completed)
	SplitCompletion(func() { completed++ }, 0)
	require.Equal(t, 2, completed, "filtered-out output is consumed")
}
