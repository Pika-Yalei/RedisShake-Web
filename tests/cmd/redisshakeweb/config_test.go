package main

import (
	"strings"
	"testing"
)

func TestGenerateConfigKeepsFullSyncAndIncrementalRulesSeparate(t *testing.T) {
	task := Task{
		Name: "migration", SourceID: "source", TargetID: "target",
		DBMap: map[string]int{"0": 2, "1": 3}, TargetPolicy: "overwrite",
		Rules: Rules{AllowPrefixes: []string{"demo:"}, BlockCommands: []string{"set"}},
	}
	source := Connection{ID: "source", Name: "source", Kind: "standalone", Address: "127.0.0.1:6379", Password: "a$secret"}
	target := Connection{ID: "target", Name: "target", Kind: "standalone", Address: "127.0.0.1:6380"}
	config, err := generateConfig(task, source, target, "/tmp/run", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"sync_rdb = true", "sync_aof = true", "allow_db = [0, 1]",
		`allow_key_prefix = ["demo:"]`, `block_command = ["SET"]`,
		"if DB == 0 then shake.call(2, ARGV)",
		"elseif DB == 1 then shake.call(3, ARGV)",
		`rdb_restore_command_behavior = "rewrite"`,
		"target_redis_proto_max_bulk_len = 0", `password = "a$secret"`,
	} {
		if !strings.Contains(config, expected) {
			t.Errorf("generated config missing %q", expected)
		}
	}
	if strings.Contains(config, "allow_command_group") || strings.Contains(config, "block_command_group") {
		t.Fatal("command group filtering must not be enabled")
	}
}

func TestValidateTaskRejectsClusterDBMapping(t *testing.T) {
	source := Connection{ID: "source", Name: "source", Kind: "standalone", Address: "127.0.0.1:6379"}
	target := Connection{ID: "target", Name: "target", Kind: "cluster", Address: "127.0.0.1:6380"}
	task := Task{Name: "migration", SourceID: "source", TargetID: "target", DBMap: map[string]int{"0": 1}, TargetPolicy: "require_empty"}
	if err := validateTask(task, source, target); err == nil {
		t.Fatal("cluster target must use DB 0")
	}
}
