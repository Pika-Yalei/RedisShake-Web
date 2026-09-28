package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pika-Yalei/RedisShake-Web/internal/client/proto"
)

// The default user cannot run commands; only AUTH <user> "" grants access.
// HELLO rejection also exercises the RESP2 fallback used by Redis-compatible servers.
func authFixture(t *testing.T, username, master string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	t.Cleanup(func() { listener.Close(); workers.Wait() })
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				reader := proto.NewReader(bufio.NewReader(conn))
				authed := false
				for {
					value, err := reader.ReadReply()
					if err != nil {
						return
					}
					args := value.([]interface{})
					command := strings.ToUpper(args[0].(string))
					reply := "+OK\r\n"
					switch {
					case command == "AUTH":
						authed = len(args) == 3 && args[1] == username && args[2] == ""
						if !authed {
							reply = "-WRONGPASS invalid credentials\r\n"
						}
					case command == "HELLO":
						reply = "-ERR unknown command HELLO\r\n"
					case !authed:
						reply = "-NOAUTH Authentication required\r\n"
					case command == "PING":
						reply = "+PONG\r\n"
					case command == "SENTINEL":
						host, port, _ := net.SplitHostPort(master)
						reply = fmt.Sprintf("*2\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n", len(host), host, len(port), port)
					}
					if _, err = conn.Write([]byte(reply)); err != nil {
						return
					}
				}
			}()
		}
	}()
	return listener.Addr().String()
}

func TestUsernameOnlyAuthenticatesBeforeSelectingDB(t *testing.T) {
	addr := authFixture(t, "reader", "")
	for _, kind := range []string{"standalone", "sentinel"} {
		t.Run(kind, func(t *testing.T) {
			c := Connection{Name: "auth", Kind: kind, Address: addr, AuthMode: authUsername, Username: "reader"}
			if kind == "sentinel" {
				c.SentinelAddress = authFixture(t, "observer", addr)
				c.SentinelMaster = "primary"
				c.SentinelAuthMode = authUsername
				c.SentinelUsername = "observer"
			}
			for range 2 {
				client, err := redisClient(c, 5)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				err = client.Ping(ctx).Err()
				cancel()
				client.Close()
				if err != nil {
					t.Fatalf("named empty-password auth must run before DB selection: %v", err)
				}
			}
		})
	}
}

func TestTestConnectionDoesNotReusePasswordOrPersistDraft(t *testing.T) {
	// The API's read path is exercised separately from updateConnection.
	st, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.db.Close()
	old := Connection{ID: "saved", Name: "saved", Kind: "standalone", AuthMode: authUsernamePassword, Username: "reader", Password: "old-password", Address: authFixture(t, "reader", "")}
	if err = st.saveConnection(old); err != nil {
		t.Fatal(err)
	}
	draft := old
	draft.AuthMode = authUsername
	draft.Password = ""
	body, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	(&app{store: st}).testConnection(w, httptest.NewRequest("POST", "/api/connections/test", strings.NewReader(string(body))))
	var checks []Check
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &checks) != nil || len(checks) == 0 || !checks[0].OK {
		t.Fatalf("test connection failed: %d %s", w.Code, w.Body.String())
	}
	saved, err := st.connection(old.ID)
	if err != nil || saved.Password != old.Password {
		t.Fatal("test draft changed saved credentials")
	}
}
