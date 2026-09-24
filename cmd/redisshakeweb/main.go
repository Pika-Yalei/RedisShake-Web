package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pika-Yalei/RedisShake-Web/internal/kernel"
)

//go:embed static/*
var assets embed.FS

func main() {
	mode := "serve"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		mode, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet(mode, flag.ExitOnError)
	dataDir := flags.String("data-dir", defaultDataDir(), "persistent data directory")
	socketDir := flags.String("socket-dir", "", "directory containing runner Unix socket")
	listen := flags.String("listen", "127.0.0.1:8080", "Web listen address")
	assetsDir := flags.String("assets-dir", "", "serve frontend assets directly from a directory (development)")
	_ = flags.Parse(args)
	if *socketDir == "" {
		*socketDir = *dataDir
	}
	var err error
	switch mode {
	case "serve":
		err = serve(*dataDir, *socketDir, *listen, *assetsDir)
	case "web":
		err = serveWeb(*socketDir, *listen, *assetsDir)
	case "runner":
		err = serveRunner(*dataDir, *socketDir)
	case "task":
		if flags.NArg() != 1 {
			err = errors.New("task 需要一个配置文件路径")
		} else {
			kernel.Run(flags.Arg(0))
		}
	case "status":
		err = printStatus(*socketDir)
	case "shutdown":
		err = requestShutdown(*socketDir)
	default:
		err = fmt.Errorf("unknown command %q", mode)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func defaultDataDir() string {
	if s := os.Getenv("REDISSHAKE_WEB_DATA_DIR"); s != "" {
		return s
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ".redis-shake-web"
	}
	return filepath.Join(h, ".redis-shake-web")
}

func socketPath(dir string) string { return filepath.Join(dir, "runner.sock") }

func serve(dataDir, socketDir, listen, assetsDir string) error {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return err
	}
	if !runnerAlive(socketDir) {
		bin, err := os.Executable()
		if err != nil {
			return err
		}
		arguments := []string{"runner", "--data-dir", dataDir, "--socket-dir", socketDir}
		cmd := exec.Command(bin, arguments...)
		logPath := filepath.Join(dataDir, "runner-console.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer logFile.Close()
		cmd.Stdout, cmd.Stderr = logFile, logFile
		configureDetached(cmd)
		if err := cmd.Start(); err != nil {
			return err
		}
		for i := 0; i < 50 && !runnerAlive(socketDir); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if !runnerAlive(socketDir) {
			return fmt.Errorf("执行器未启动，查看 %s", logPath)
		}
	}
	return serveWeb(socketDir, listen, assetsDir)
}

func runnerClient(dataDir string) *http.Client {
	return &http.Client{Timeout: 3 * time.Minute, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socketPath(dataDir))
		},
	}}
}

func runnerAlive(dataDir string) bool {
	client := runnerClient(dataDir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://runner/health", nil)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func serveWeb(socketDir, listen, assetsDir string) error {
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return err
	}
	if assetsDir != "" {
		if _, err := os.Stat(filepath.Join(assetsDir, "index.html")); err != nil {
			return fmt.Errorf("development assets: %w", err)
		}
		static = os.DirFS(assetsDir)
	}
	client := runnerClient(socketDir)
	mux := http.NewServeMux()
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Local socket is the only control path. The runner authenticates each request.
		upstream, err := http.NewRequestWithContext(r.Context(), r.Method, "http://runner"+r.URL.RequestURI(), r.Body)
		if err != nil {
			http.Error(w, "请求错误", http.StatusBadRequest)
			return
		}
		upstream.Header = r.Header.Clone()
		upstream.Host = r.Host
		resp, err := client.Do(upstream)
		if err != nil {
			http.Error(w, "执行器暂不可用", http.StatusServiceUnavailable)
			return
		}
		defer resp.Body.Close()
		for k, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(k, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	mux.Handle("/", http.FileServer(http.FS(static)))
	srv := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	fmt.Printf("Web 访问地址：http://%s\n", listen)
	err = srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func printStatus(dataDir string) error {
	if !runnerAlive(dataDir) {
		fmt.Println("执行器未运行")
		return nil
	}
	fmt.Println("执行器正在运行")
	return nil
}

func requestShutdown(dataDir string) error {
	req, _ := http.NewRequest(http.MethodPost, "http://runner/internal/shutdown", nil)
	resp, err := runnerClient(dataDir).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("shutdown returned %d", resp.StatusCode)
	}
	return nil
}
