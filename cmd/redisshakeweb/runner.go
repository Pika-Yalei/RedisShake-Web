package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type app struct {
	store         *store
	shakePath     string
	mu            sync.Mutex
	authMu        sync.Mutex
	loginFailures int
	loginWindow   time.Time
	active        map[string]*runProcess
	server        *http.Server
	listener      net.Listener
}

type runProcess struct {
	cmd      *exec.Cmd
	liveness *os.File
	stopping bool
}

type boundedWriter struct {
	mu        sync.Mutex
	file      *os.File
	remaining int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	length := len(p)
	if w.remaining <= 0 {
		return length, nil
	}
	if int64(length) > w.remaining {
		p = p[:w.remaining]
	}
	n, err := w.file.Write(p)
	w.remaining -= int64(n)
	if err != nil {
		return n, err
	}
	return length, nil
}

func serveRunner(dir, socketDir, shake string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lockFile, err := os.OpenFile(filepath.Join(dir, "runner.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("另一个执行器已在使用此数据目录")
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	st, err := openStore(dir)
	if err != nil {
		return err
	}
	defer st.db.Close()
	if err := reconcileOldRuns(st); err != nil {
		return err
	}
	// Older installations may still contain a now-unused initialization code.
	_ = os.Remove(filepath.Join(dir, "bootstrap.code"))
	if shake == "" {
		bin, err := os.Executable()
		if err != nil {
			return err
		}
		shake = filepath.Join(filepath.Dir(bin), "redis-shake")
		if _, err := os.Stat(shake); err != nil {
			shake = filepath.Join("bin", "redis-shake")
		}
	}
	a := &app{store: st, shakePath: shake, active: make(map[string]*runProcess)}
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		return err
	}
	path := socketPath(socketDir)
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(path)
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	a.listener = listener
	a.server = &http.Server{Handler: a.routes(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("执行器已启动：%s", path)
	err = a.server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func reconcileOldRuns(st *store) error {
	rows, err := st.db.Query("SELECT pid FROM runs WHERE status IN ('STARTING','RUNNING','STOPPING') AND pid>0")
	if err != nil {
		return err
	}
	var pids []int
	for rows.Next() {
		var pid int
		if err := rows.Scan(&pid); err != nil {
			_ = rows.Close()
			return err
		}
		pids = append(pids, pid)
	}
	_ = rows.Close()
	for _, pid := range pids {
		if !processAlive(pid) {
			continue
		}
		for i := 0; i < 160 && processAlive(pid); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if processAlive(pid) {
			return fmt.Errorf("上次运行的进程 %d 仍存在；禁止重复启动，请核对后处理", pid)
		}
	}
	return st.markInterrupted()
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func (a *app) shutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "不支持此操作")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		a.mu.Lock()
		for _, p := range a.active {
			p.stopping = true
			_ = p.cmd.Process.Signal(os.Interrupt)
		}
		a.mu.Unlock()
		time.Sleep(2 * time.Second)
		_ = a.server.Shutdown(ctx)
	}()
}

func (a *app) startRun(t Task, source, target Connection) (Run, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.active) >= 5 {
		return Run{}, errors.New("已达到 5 个并发任务上限")
	}
	active, err := a.store.activeRun(t.ID)
	if err != nil || active {
		return Run{}, errors.New("此任务已有运行中的同步")
	}
	busy, err := a.targetBusy(target)
	if err != nil {
		return Run{}, errors.New("无法检查目标端是否已被其他任务占用")
	}
	if busy {
		return Run{}, errors.New("此目标连接已有运行中的同步任务")
	}
	if _, err := os.Stat(a.shakePath); err != nil {
		return Run{}, errors.New("未找到 RedisShake 内核；请先安装固定版本制品")
	}
	id, err := randomToken(18)
	if err != nil {
		return Run{}, err
	}
	runDir := filepath.Join(a.store.dir, "runs", id)
	if err := os.MkdirAll(runDir, 0700); err != nil {
		return Run{}, err
	}
	versionContext, versionCancel := context.WithTimeout(context.Background(), 12*time.Second)
	sourceVersion, sourceErr := redisVersion(versionContext, source)
	targetVersion, targetErr := redisVersion(versionContext, target)
	versionCancel()
	if sourceErr != nil || targetErr != nil {
		return Run{}, errors.New("启动前无法确认源端和目标端版本")
	}
	config, err := generateConfig(t, source, target, runDir, versionIsOlder(targetVersion, sourceVersion))
	if err != nil {
		return Run{}, err
	}
	configPath := filepath.Join(runDir, "shake.toml")
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		return Run{}, err
	}
	console, err := os.OpenFile(filepath.Join(runDir, "console.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return Run{}, err
	}
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		_ = console.Close()
		return Run{}, err
	}
	cmd := exec.Command(a.shakePath, configPath)
	cmd.Dir = runDir
	consoleWriter := &boundedWriter{file: console, remaining: 1 << 20}
	cmd.Stdout, cmd.Stderr = consoleWriter, consoleWriter
	cmd.ExtraFiles = []*os.File{readPipe}
	cmd.Env = append(os.Environ(), "REDISSHAKE_WEB_RUNNER_FD=3", "REDISSHAKE_WEB_LITERAL_CONFIG=1")
	if err := cmd.Start(); err != nil {
		_ = console.Close()
		_ = readPipe.Close()
		_ = writePipe.Close()
		return Run{}, err
	}
	_ = readPipe.Close()
	run := Run{ID: id, TaskID: t.ID, Status: "RUNNING", Phase: "UNKNOWN", PID: cmd.Process.Pid, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	_, err = a.store.db.Exec("INSERT INTO runs(id,task_id,status,phase,error,pid,started_at,ended_at) VALUES(?,?,?,?,?,?,?,?)", run.ID, run.TaskID, run.Status, run.Phase, "", run.PID, run.StartedAt, "")
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = writePipe.Close()
		_ = console.Close()
		return Run{}, err
	}
	a.active[id] = &runProcess{cmd: cmd, liveness: writePipe}
	go a.watchRun(run, cmd, writePipe, console)
	go a.trackPhase(run.ID, runDir)
	return run, nil
}

func (a *app) watchRun(run Run, cmd *exec.Cmd, liveness, console *os.File) {
	err := cmd.Wait()
	_ = liveness.Close()
	_ = console.Close()
	_ = os.Remove(filepath.Join(a.store.dir, "runs", run.ID, "shake.toml"))
	a.mu.Lock()
	stopping := false
	if p, exists := a.active[run.ID]; exists {
		stopping = p.stopping
		delete(a.active, run.ID)
	}
	a.mu.Unlock()
	status, message := "FAILED", "同步进程意外结束；请核对日志后重新全量"
	if stopping {
		status, message = "STOPPED", ""
	} else if err != nil {
		message = "RedisShake 退出：" + err.Error()
	}
	_, _ = a.store.db.Exec("UPDATE runs SET status=?,error=?,ended_at=? WHERE id=?", status, message, time.Now().UTC().Format(time.RFC3339), run.ID)
}

func (a *app) trackPhase(runID, runDir string) {
	logPath := filepath.Join(runDir, "shake.log")
	var offset int64
	for {
		time.Sleep(2 * time.Second)
		file, err := os.Open(logPath)
		if err == nil {
			if stat, statErr := file.Stat(); statErr == nil && stat.Size() < offset {
				offset = 0
			}
			_, _ = file.Seek(offset, io.SeekStart)
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := strings.ToLower(scanner.Text())
				phase := ""
				if strings.Contains(line, "syncing aof") {
					phase = "INCREMENTAL"
				} else if strings.Contains(line, "syncing rdb") || strings.Contains(line, "receiving rdb") {
					phase = "FULL_SYNC"
				}
				if phase != "" {
					_, _ = a.store.db.Exec("UPDATE runs SET phase=? WHERE id=?", phase, runID)
				}
			}
			offset, _ = file.Seek(0, io.SeekCurrent)
			_ = file.Close()
		}
		a.mu.Lock()
		_, active := a.active[runID]
		a.mu.Unlock()
		if !active {
			return
		}
	}
}

func (a *app) stopRun(runID string) error {
	a.mu.Lock()
	p, ok := a.active[runID]
	if !ok {
		a.mu.Unlock()
		return errors.New("运行已结束")
	}
	if p.stopping {
		a.mu.Unlock()
		return nil
	}
	p.stopping = true
	_, _ = a.store.db.Exec("UPDATE runs SET status='STOPPING' WHERE id=?", runID)
	err := p.cmd.Process.Signal(os.Interrupt)
	a.mu.Unlock()
	if err != nil {
		return err
	}
	go func() {
		time.Sleep(15 * time.Second)
		a.mu.Lock()
		p, ok := a.active[runID]
		a.mu.Unlock()
		if ok {
			_ = p.cmd.Process.Kill()
		}
	}()
	return nil
}
