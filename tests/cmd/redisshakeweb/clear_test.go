package main

import "testing"

func TestClearScopeDigestChangesWithDBMapping(t *testing.T) {
	target := Connection{ID: "target", Name: "target", Kind: "standalone", Address: "127.0.0.1:6379"}
	task := Task{DBMap: map[string]int{"0": 1}}
	first := clearScopeDigest(task, target)
	task.DBMap = map[string]int{"0": 2}
	if first == clearScopeDigest(task, target) {
		t.Fatal("a changed target DB must invalidate the clear confirmation")
	}
	target.Password = "changed"
	if first == clearScopeDigest(Task{DBMap: map[string]int{"0": 1}}, target) {
		t.Fatal("a changed target connection must invalidate the clear confirmation")
	}
}
