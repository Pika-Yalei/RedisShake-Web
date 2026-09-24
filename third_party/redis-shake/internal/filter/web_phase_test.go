package filter

import (
	"os"
	"os/exec"
	"testing"

	"RedisShake/internal/config"
	"RedisShake/internal/entry"
)

func TestWebIncrementalCommandFilterDoesNotAffectFullSync(t *testing.T) {
	previous := config.Opt.Filter
	defer func() { config.Opt.Filter = previous }()
	config.Opt.Filter = config.FilterOptions{BlockCommand: []string{"SET"}, AllowDB: []int{0}}
	for _, test := range []struct {
		name        string
		incremental bool
		command     []string
		want        bool
	}{
		{"full SET", false, []string{"SET", "demo:key", "value"}, true},
		{"incremental SET", true, []string{"SET", "demo:key", "value"}, false},
		{"incremental HSET", true, []string{"HSET", "demo:key", "field", "value"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := &entry.Entry{DbId: 0, Argv: test.command, IsIncremental: test.incremental}
			e.Parse()
			if got := Filter(e); got != test.want {
				t.Fatalf("Filter() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestWebUnfilteredDatabaseFlushStops(t *testing.T) {
	if os.Getenv("REDISSHAKE_WEB_TEST_FLUSH_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestWebUnfilteredDatabaseFlushStops$")
		cmd.Env = append(os.Environ(), "REDISSHAKE_WEB_TEST_FLUSH_CHILD=1")
		output, err := cmd.CombinedOutput()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 {
			t.Fatalf("expected a failed exit, exit=%v output=%s", err, output)
		}
		return
	}
	previous := config.Opt.Filter
	defer func() { config.Opt.Filter = previous }()
	config.Opt.Filter = config.FilterOptions{}
	e := &entry.Entry{DbId: 0, Argv: []string{"FLUSHDB"}, IsIncremental: true}
	e.Parse()
	Filter(e)
	t.Fatal("unfiltered FLUSHDB should stop migration")
}

func TestWebFlushAllStopsWhenCurrentDBIsNotSelected(t *testing.T) {
	if os.Getenv("REDISSHAKE_WEB_TEST_FLUSHALL_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestWebFlushAllStopsWhenCurrentDBIsNotSelected$")
		cmd.Env = append(os.Environ(), "REDISSHAKE_WEB_TEST_FLUSHALL_CHILD=1")
		output, err := cmd.CombinedOutput()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 {
			t.Fatalf("expected a failed exit, exit=%v output=%s", err, output)
		}
		return
	}
	previous := config.Opt.Filter
	defer func() { config.Opt.Filter = previous }()
	config.Opt.Filter = config.FilterOptions{AllowDB: []int{1}}
	e := &entry.Entry{DbId: 0, Argv: []string{"FLUSHALL"}, IsIncremental: true}
	e.Parse()
	Filter(e)
	t.Fatal("unfiltered FLUSHALL must stop even when issued from an unselected DB")
}
