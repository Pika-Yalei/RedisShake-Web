package client

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Pika-Yalei/RedisShake-Web/internal/client/proto"
	"github.com/stretchr/testify/require"
)

func replicationServer(t *testing.T, reply string, auth chan []interface{}) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan struct{})
	t.Cleanup(func() { listener.Close(); <-done })
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				reader := proto.NewReader(bufio.NewReader(conn))
				request, err := reader.ReadReply()
				if err != nil {
					return
				}
				args := request.([]interface{})
				if args[0] == "AUTH" {
					if auth != nil {
						auth <- args
					}
					fmt.Fprint(conn, "+OK\r\n")
					request, err = reader.ReadReply()
					if err != nil {
						return
					}
					args = request.([]interface{})
				}
				if len(args) != 2 || args[0] != "INFO" || args[1] != "replication" {
					fmt.Fprint(conn, "-ERR wrong command\r\n")
					return
				}
				if strings.HasPrefix(reply, "-") {
					fmt.Fprint(conn, reply+"\r\n")
				} else {
					fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(reply), reply)
				}
			}()
		}
	}()
	return listener.Addr().String()
}

func TestReadActualMasterThroughReplicaAndNamedEmptyPassword(t *testing.T) {
	auth := make(chan []interface{}, 2)
	master := replicationServer(t, "role:master\r\nmaster_repl_offset:9007199254740993\r\nmaster_replid:history\r\nmaster_replid2:previous\r\nsecond_repl_offset:30\r\n", auth)
	host, port, err := net.SplitHostPort(master)
	require.NoError(t, err)
	replica := replicationServer(t, "role:slave\r\nmaster_host:"+host+"\r\nmaster_port:"+port+"\r\nslave_repl_offset:10\r\nmaster_repl_offset:20\r\n", auth)
	pos, err := ReadMasterPosition(context.Background(), replica, "named-user", "", false, TlsConfig{})
	require.NoError(t, err)
	require.Equal(t, master, pos.Node)
	require.Equal(t, int64(9007199254740993), pos.Offset)
	require.Equal(t, "history", pos.ReplicationID)
	require.Equal(t, []interface{}{"AUTH", "named-user", ""}, <-auth)
	require.Equal(t, []interface{}{"AUTH", "named-user", ""}, <-auth)
}

func TestReadMasterErrorsAreReturned(t *testing.T) {
	for _, reply := range []string{"-NOPERM INFO denied", "role:master\r\nmaster_repl_offset:bad\r\n", "role:slave\r\nmaster_host:localhost\r\nmaster_port:bad\r\n"} {
		address := replicationServer(t, reply, nil)
		_, err := ReadMasterPosition(context.Background(), address, "", "", false, TlsConfig{})
		require.Error(t, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			buf := make([]byte, 1024)
			for {
				if _, err = conn.Read(buf); err != nil {
					return
				}
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = ReadMasterPosition(ctx, listener.Addr().String(), "", "", false, TlsConfig{})
	require.Error(t, err)
	require.Less(t, time.Since(start), time.Second)
	<-done
}
