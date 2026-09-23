package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type clearNodeResult struct {
	DB      int    `json:"db"`
	Node    string `json:"node"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func clearScopeDigest(t Task, target Connection) string {
	payload, _ := json.Marshal(struct {
		Target Connection
		DBs    []int
	}{Target: target, DBs: targetDBList(t)})
	return digest(string(payload))
}

func (a *app) targetBusy(target Connection) (bool, error) {
	tasks, err := a.store.tasks()
	if err != nil {
		return true, err
	}
	var targetIDs map[string]bool
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, task := range tasks {
		active, err := a.store.activeRun(task.ID)
		if err != nil {
			return true, err
		}
		if !active {
			continue
		}
		other, err := a.store.connection(task.TargetID)
		if err != nil {
			return true, err
		}
		if other.ID == target.ID || (other.Kind == target.Kind && other.Address == target.Address && other.SentinelMaster == target.SentinelMaster && other.SentinelAddress == target.SentinelAddress) {
			return true, nil
		}
		if targetIDs == nil {
			targetIDs, err = redisProcessIDs(ctx, target)
			if err != nil {
				return true, err
			}
		}
		otherIDs, err := redisProcessIDs(ctx, other)
		if err != nil {
			return true, err
		}
		for id := range targetIDs {
			if otherIDs[id] {
				return true, nil
			}
		}
	}
	return false, nil
}

func (a *app) clearPreview(w http.ResponseWriter, r *http.Request) {
	t, source, target, err := a.taskConnections(r.PathValue("id"))
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	if err := validateTask(t, source, target); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	busy, err := a.targetBusy(target)
	if err != nil || busy {
		writeError(w, 409, "此目标正在执行同步，请先停止相关任务")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		writeError(w, 500, "无法创建确认请求")
		return
	}
	_, err = a.store.db.Exec("INSERT INTO clear_requests(digest,task_id,target_digest,expires_at,status) VALUES(?,?,?,?,?)", digest(token), t.ID, clearScopeDigest(t, target), time.Now().Add(5*time.Minute).Unix(), "PENDING")
	if err != nil {
		writeError(w, 500, "无法创建确认请求")
		return
	}
	writeJSON(w, 200, map[string]any{"token": token, "targetName": target.Name, "address": target.Address, "kind": target.Kind, "dbs": targetDBList(t), "allKeys": true, "expiresInSeconds": 300})
}

func (a *app) clearConfirm(w http.ResponseWriter, r *http.Request) {
	var input struct{ Token, TargetName string }
	if !readJSON(w, r, &input) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var taskID, targetDigest, status, result string
	var expiry int64
	err := a.store.db.QueryRow("SELECT task_id,target_digest,expires_at,status,result FROM clear_requests WHERE digest=?", digest(input.Token)).Scan(&taskID, &targetDigest, &expiry, &status, &result)
	if err != nil || taskID != r.PathValue("id") {
		writeError(w, 404, "确认请求不存在")
		return
	}
	if status != "PENDING" {
		if status == "DONE" || status == "PARTIAL" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(result))
			return
		}
		writeError(w, 409, "清理结果尚不确定，已阻止重复执行")
		return
	}
	if expiry < time.Now().Unix() {
		writeError(w, 410, "确认请求已过期，请重新查看范围")
		return
	}
	t, source, target, err := a.taskConnections(taskID)
	if err != nil || target.Name != input.TargetName {
		writeError(w, 409, "目标名称不匹配")
		return
	}
	if err := validateTask(t, source, target); err != nil {
		writeError(w, 409, "任务配置已变更，请重新确认范围")
		return
	}
	if targetDigest != clearScopeDigest(t, target) {
		writeError(w, 409, "目标连接或 DB 范围已变更，请重新确认范围")
		return
	}
	busy, err := a.targetBusy(target)
	if err != nil || busy {
		writeError(w, 409, "此目标正在执行同步，请先停止相关任务")
		return
	}
	_, err = a.store.db.Exec("UPDATE clear_requests SET status='RUNNING' WHERE digest=? AND status='PENDING'", digest(input.Token))
	if err != nil {
		writeError(w, 500, "无法记录清理操作")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	results := []clearNodeResult{}
	var resultsMu sync.Mutex
	allOK := true
	for _, db := range targetDBList(t) {
		client, err := redisClient(target, db)
		if err != nil {
			results = append(results, clearNodeResult{DB: db, Node: target.Address, Message: err.Error()})
			allOK = false
			continue
		}
		if cluster, ok := client.(*redis.ClusterClient); ok {
			err = cluster.ForEachMaster(ctx, func(ctx context.Context, node *redis.Client) error {
				nodeErr := node.FlushDB(ctx).Err()
				resultsMu.Lock()
				results = append(results, clearNodeResult{DB: db, Node: node.Options().Addr, OK: nodeErr == nil, Message: messageOf(nodeErr, "已清空")})
				resultsMu.Unlock()
				return nodeErr
			})
		} else {
			err = client.FlushDB(ctx).Err()
			results = append(results, clearNodeResult{DB: db, Node: target.Address, OK: err == nil, Message: messageOf(err, "已清空")})
		}
		_ = client.Close()
		if err != nil {
			allOK = false
		}
	}
	if len(results) == 0 {
		allOK = false
	}
	for _, item := range results {
		if !item.OK {
			allOK = false
		}
	}
	text := "全部选定目标 DB 已清空"
	state := "DONE"
	if !allOK {
		text, state = "清理部分失败或结果不确定，请检查逐节点结果", "PARTIAL"
	}
	payload, _ := json.Marshal(map[string]any{"ok": allOK, "message": text, "results": results})
	_, err = a.store.db.Exec("UPDATE clear_requests SET status=?,result=? WHERE digest=?", state, string(payload), digest(input.Token))
	if err != nil {
		writeError(w, 500, "清理命令已发送，但结果记录失败；请人工核对")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func clearSummary(result []clearNodeResult) string {
	parts := make([]string, len(result))
	for i, item := range result {
		parts[i] = fmt.Sprintf("DB %d %s: %s", item.DB, item.Node, item.Message)
	}
	return strings.Join(parts, "\n")
}
