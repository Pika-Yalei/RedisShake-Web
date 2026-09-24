package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, map[string]bool{"ok": true}) })
	mux.HandleFunc("POST /internal/shutdown", a.shutdown)
	mux.HandleFunc("GET /api/bootstrap/status", a.bootstrapStatus)
	mux.HandleFunc("POST /api/bootstrap/init", a.bootstrapInit)
	mux.HandleFunc("POST /api/login", a.login)
	mux.HandleFunc("POST /api/password/reset", a.resetPassword)
	mux.HandleFunc("GET /api/me", a.requireAuth(a.me))
	mux.HandleFunc("POST /api/logout", a.requireAuth(a.logout))
	mux.HandleFunc("POST /api/password", a.requireAuth(a.changePassword))
	mux.HandleFunc("GET /api/connections", a.requireAuth(a.listConnections))
	mux.HandleFunc("POST /api/connections", a.requireAuth(a.createConnection))
	mux.HandleFunc("POST /api/connections/test", a.requireAuth(a.testConnection))
	mux.HandleFunc("PUT /api/connections/{id}", a.requireAuth(a.updateConnection))
	mux.HandleFunc("DELETE /api/connections/{id}", a.requireAuth(a.deleteConnection))
	mux.HandleFunc("GET /api/tasks", a.requireAuth(a.listTasks))
	mux.HandleFunc("POST /api/tasks", a.requireAuth(a.createTask))
	mux.HandleFunc("GET /api/tasks/{id}", a.requireAuth(a.getTask))
	mux.HandleFunc("PUT /api/tasks/{id}", a.requireAuth(a.updateTask))
	mux.HandleFunc("DELETE /api/tasks/{id}", a.requireAuth(a.deleteTask))
	mux.HandleFunc("POST /api/tasks/{id}/copy", a.requireAuth(a.copyTask))
	mux.HandleFunc("POST /api/tasks/{id}/preflight", a.requireAuth(a.preflightTask))
	mux.HandleFunc("POST /api/tasks/{id}/start", a.requireAuth(a.startTask))
	mux.HandleFunc("GET /api/tasks/{id}/runs", a.requireAuth(a.listRuns))
	mux.HandleFunc("POST /api/tasks/{id}/clear-preview", a.requireAuth(a.clearPreview))
	mux.HandleFunc("POST /api/tasks/{id}/clear-confirm", a.requireAuth(a.clearConfirm))
	mux.HandleFunc("POST /api/runs/{id}/stop", a.requireAuth(a.stopTaskRun))
	mux.HandleFunc("GET /api/runs/{id}/logs", a.requireAuth(a.logs))
	return mux
}

func (a *app) listConnections(w http.ResponseWriter, _ *http.Request) {
	connections, err := a.store.connections()
	if err != nil {
		writeError(w, 500, "读取连接失败")
		return
	}
	writeJSON(w, 200, connections)
}

func (a *app) createConnection(w http.ResponseWriter, r *http.Request) {
	var c Connection
	if !readJSON(w, r, &c) {
		return
	}
	if err := validateConnection(c); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	id, err := randomToken(16)
	if err != nil {
		writeError(w, 500, "无法创建连接")
		return
	}
	c.ID = id
	if err := a.store.saveConnection(c); err != nil {
		writeError(w, 500, "无法保存连接")
		return
	}
	writeJSON(w, 201, c.safe())
}

func (a *app) updateConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	old, err := a.store.connection(id)
	if err != nil {
		writeError(w, 404, "连接不存在")
		return
	}
	var c Connection
	if !readJSON(w, r, &c) {
		return
	}
	c.ID = id
	if c.Password == "" {
		c.Password = old.Password
	}
	if c.SentinelPassword == "" {
		c.SentinelPassword = old.SentinelPassword
	}
	if err := validateConnection(c); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := a.store.saveConnection(c); err != nil {
		writeError(w, 500, "无法保存连接")
		return
	}
	writeJSON(w, 200, c.safe())
}

func (a *app) deleteConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tasks, err := a.store.tasks()
	if err != nil {
		writeError(w, 500, "无法检查任务引用")
		return
	}
	for _, task := range tasks {
		if task.SourceID == id || task.TargetID == id {
			writeError(w, 409, "连接已被任务引用")
			return
		}
	}
	if _, err := a.store.db.Exec("DELETE FROM connections WHERE id=?", id); err != nil {
		writeError(w, 500, "删除连接失败")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *app) testConnection(w http.ResponseWriter, r *http.Request) {
	var c Connection
	if !readJSON(w, r, &c) {
		return
	}
	if c.ID != "" {
		old, err := a.store.connection(c.ID)
		if err == nil {
			if c.Password == "" {
				c.Password = old.Password
			}
			if c.SentinelPassword == "" {
				c.SentinelPassword = old.SentinelPassword
			}
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	writeJSON(w, 200, checkRedis(ctx, c))
}

func (a *app) listTasks(w http.ResponseWriter, _ *http.Request) {
	tasks, err := a.store.tasks()
	if err != nil {
		writeError(w, 500, "读取任务失败")
		return
	}
	writeJSON(w, 200, tasks)
}

func (a *app) createTask(w http.ResponseWriter, r *http.Request) {
	var t Task
	if !readJSON(w, r, &t) {
		return
	}
	if strings.TrimSpace(t.Name) == "" {
		writeError(w, 400, "任务名称不能为空")
		return
	}
	id, err := randomToken(16)
	if err != nil {
		writeError(w, 500, "无法创建任务")
		return
	}
	t.ID = id
	if t.TargetPolicy == "" {
		t.TargetPolicy = "require_empty"
	}
	if err := a.store.saveTask(t); err != nil {
		writeError(w, 500, "保存任务失败")
		return
	}
	writeJSON(w, 201, t)
}

func (a *app) getTask(w http.ResponseWriter, r *http.Request) {
	t, err := a.store.task(r.PathValue("id"))
	if err != nil {
		writeError(w, 404, "任务不存在")
		return
	}
	writeJSON(w, 200, t)
}

func (a *app) updateTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.store.task(id); err != nil {
		writeError(w, 404, "任务不存在")
		return
	}
	active, _ := a.store.activeRun(id)
	if active {
		writeError(w, 409, "运行中的任务不可编辑")
		return
	}
	var t Task
	if !readJSON(w, r, &t) {
		return
	}
	t.ID = id
	if strings.TrimSpace(t.Name) == "" {
		writeError(w, 400, "任务名称不能为空")
		return
	}
	if err := a.store.saveTask(t); err != nil {
		writeError(w, 500, "保存任务失败")
		return
	}
	writeJSON(w, 200, t)
}

func (a *app) deleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	active, _ := a.store.activeRun(id)
	if active {
		writeError(w, 409, "请先停止运行中的任务")
		return
	}
	_, err := a.store.db.Exec("DELETE FROM tasks WHERE id=?", id)
	if err != nil {
		writeError(w, 500, "删除任务失败")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *app) copyTask(w http.ResponseWriter, r *http.Request) {
	t, err := a.store.task(r.PathValue("id"))
	if err != nil {
		writeError(w, 404, "任务不存在")
		return
	}
	t.ID, err = randomToken(16)
	if err != nil {
		writeError(w, 500, "复制任务失败")
		return
	}
	t.Name += " - 副本"
	if err := a.store.saveTask(t); err != nil {
		writeError(w, 500, "复制任务失败")
		return
	}
	writeJSON(w, 201, t)
}

func (a *app) taskConnections(id string) (Task, Connection, Connection, error) {
	t, err := a.store.task(id)
	if err != nil {
		return Task{}, Connection{}, Connection{}, err
	}
	source, err := a.store.connection(t.SourceID)
	if err != nil {
		return Task{}, Connection{}, Connection{}, errors.New("源连接不存在")
	}
	target, err := a.store.connection(t.TargetID)
	if err != nil {
		return Task{}, Connection{}, Connection{}, errors.New("目标连接不存在")
	}
	return t, source, target, nil
}

func (a *app) preflightTask(w http.ResponseWriter, r *http.Request) {
	t, source, target, err := a.taskConnections(r.PathValue("id"))
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	checks := preflight(ctx, t, source, target, a.taskBinary)
	writeJSON(w, 200, checks)
}

func checksPass(checks []Check) bool {
	if len(checks) == 0 {
		return false
	}
	for _, c := range checks {
		if !c.OK {
			return false
		}
	}
	return true
}

func (a *app) startTask(w http.ResponseWriter, r *http.Request) {
	t, source, target, err := a.taskConnections(r.PathValue("id"))
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	checks := preflight(ctx, t, source, target, a.taskBinary)
	if !checksPass(checks) {
		writeJSON(w, 409, map[string]any{"error": "预检未通过", "checks": checks})
		return
	}
	run, err := a.startRun(t, source, target)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 201, run)
}

func (a *app) listRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := a.store.runs(r.PathValue("id"))
	if err != nil {
		writeError(w, 500, "读取运行记录失败")
		return
	}
	writeJSON(w, 200, runs)
}

func (a *app) stopTaskRun(w http.ResponseWriter, r *http.Request) {
	if err := a.stopRun(r.PathValue("id")); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 202, map[string]bool{"stopping": true})
}

func (a *app) logs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var exists int
	if err := a.store.db.QueryRow("SELECT 1 FROM runs WHERE id=?", id).Scan(&exists); err != nil {
		writeError(w, 404, "运行记录不存在")
		return
	}
	path := filepath.Join(a.store.dir, "runs", id, "shake.log")
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		path = filepath.Join(a.store.dir, "runs", id, "console.log")
		file, err = os.Open(path)
	}
	if err != nil {
		writeError(w, 404, "暂无日志")
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		writeError(w, 500, "无法读取日志")
		return
	}
	start := stat.Size() - 64*1024
	if start < 0 {
		start = 0
	}
	_, _ = file.Seek(start, io.SeekStart)
	data, err := io.ReadAll(io.LimitReader(file, 64*1024))
	if err != nil {
		writeError(w, 500, "无法读取日志")
		return
	}
	writeJSON(w, 200, map[string]any{"text": string(data), "truncated": start > 0, "updatedAt": stat.ModTime().UTC().Format(time.RFC3339)})
}

func mustJSON(value any) string {
	b, _ := json.Marshal(value)
	return string(b)
}

func dbNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func badRequest(err error) error { return fmt.Errorf("invalid request: %w", err) }
