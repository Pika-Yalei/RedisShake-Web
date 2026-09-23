package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func tomlString(value string) string {
	b, _ := json.Marshal(value)
	return string(b)
}

func tomlStrings(values []string) string {
	items := make([]string, len(values))
	for i, value := range values {
		items[i] = tomlString(value)
	}
	return "[" + strings.Join(items, ", ") + "]"
}

func generateConfig(task Task, source, target Connection, runDir string, downgrade bool) (string, error) {
	if err := validateTask(task, source, target); err != nil {
		return "", err
	}
	var b strings.Builder
	writeEndpoint := func(section string, c Connection, source bool) {
		fmt.Fprintf(&b, "[%s]\n", section)
		fmt.Fprintf(&b, "cluster = %t\n", c.Kind == "cluster")
		fmt.Fprintf(&b, "address = %s\n", tomlString(c.Address))
		fmt.Fprintf(&b, "username = %s\npassword = %s\ntls = false\n", tomlString(c.Username), tomlString(c.Password))
		if source {
			fmt.Fprintln(&b, "sync_rdb = true\nsync_aof = true")
			fmt.Fprintf(&b, "prefer_replica = %t\n", c.Kind == "sentinel")
		} else {
			fmt.Fprintln(&b, "off_reply = false")
		}
		if c.Kind == "sentinel" {
			fmt.Fprintf(&b, "[%s.sentinel]\n", section)
			fmt.Fprintf(&b, "master_name = %s\naddress = %s\nusername = %s\npassword = %s\ntls = false\n", tomlString(c.SentinelMaster), tomlString(c.SentinelAddress), tomlString(c.SentinelUsername), tomlString(c.SentinelPassword))
		}
	}
	writeEndpoint("sync_reader", source, true)
	writeEndpoint("redis_writer", target, false)
	fmt.Fprintln(&b, "[filter]")
	fmt.Fprintf(&b, "allow_key_prefix = %s\n", tomlStrings(task.Rules.AllowPrefixes))
	fmt.Fprintf(&b, "block_key_prefix = %s\n", tomlStrings(task.Rules.BlockPrefixes))
	fmt.Fprintf(&b, "allow_key_regex = %s\n", tomlStrings(task.Rules.AllowRegex))
	fmt.Fprintf(&b, "block_key_regex = %s\n", tomlStrings(task.Rules.BlockRegex))
	sourceDBs := make([]int, 0, len(task.DBMap))
	for sourceDB := range task.DBMap {
		n, _ := strconv.Atoi(sourceDB)
		sourceDBs = append(sourceDBs, n)
	}
	sort.Ints(sourceDBs)
	dbItems := make([]string, len(sourceDBs))
	for i, n := range sourceDBs {
		dbItems[i] = strconv.Itoa(n)
	}
	fmt.Fprintf(&b, "allow_db = [%s]\n", strings.Join(dbItems, ", "))
	upper := func(values []string) []string {
		out := make([]string, len(values))
		for i, v := range values {
			out[i] = strings.ToUpper(strings.TrimSpace(v))
		}
		return out
	}
	fmt.Fprintf(&b, "allow_command = %s\n", tomlStrings(upper(task.Rules.AllowCommands)))
	fmt.Fprintf(&b, "block_command = %s\n", tomlStrings(upper(task.Rules.BlockCommands)))
	fmt.Fprintln(&b, "function = '''")
	for i, sourceDB := range sourceDBs {
		prefix := "if"
		if i != 0 {
			prefix = "elseif"
		}
		fmt.Fprintf(&b, "%s DB == %d then shake.call(%d, ARGV)\n", prefix, sourceDB, task.DBMap[strconv.Itoa(sourceDB)])
	}
	fmt.Fprintln(&b, "end\n'''")
	fmt.Fprintln(&b, "[advanced]")
	fmt.Fprintf(&b, "dir = %s\n", tomlString(runDir))
	fmt.Fprintln(&b, "log_file = \"shake.log\"\nlog_level = \"info\"\nlog_interval = 5")
	fmt.Fprintln(&b, "log_rotation = true\nlog_max_size = 64\nlog_max_age = 7\nlog_max_backups = 3\nlog_compress = true")
	behavior := "panic"
	if task.TargetPolicy == "overwrite" {
		behavior = "rewrite"
	}
	fmt.Fprintf(&b, "rdb_restore_command_behavior = %s\n", tomlString(behavior))
	fmt.Fprintln(&b, "empty_db_before_sync = false")
	if downgrade {
		fmt.Fprintln(&b, "target_redis_proto_max_bulk_len = 0")
	}
	return b.String(), nil
}
