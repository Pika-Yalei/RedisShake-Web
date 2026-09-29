package writer

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/Pika-Yalei/RedisShake-Web/internal/client/proto"
	"github.com/Pika-Yalei/RedisShake-Web/internal/config"
	"github.com/Pika-Yalei/RedisShake-Web/internal/entry"
	"github.com/mcuadros/go-defaults"
	"github.com/stretchr/testify/require"
)

func TestWriterAcknowledgesAfterTargetReply(t *testing.T) {
	original := config.Opt
	defer func() { config.Opt = original }()
	defaults.SetDefaults(&config.Opt)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	received := make(chan struct{})
	release := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader := proto.NewReader(bufio.NewReader(conn))
		if _, err = reader.ReadReply(); err != nil {
			return
		}
		_, _ = conn.Write([]byte("+PONG\r\n")) // Initial client handshake.
		if _, err = reader.ReadReply(); err != nil {
			return
		}
		close(received)
		<-release
		_, _ = conn.Write([]byte("+OK\r\n"))
	}()
	w := NewRedisStandaloneWriter(context.Background(), &RedisWriterOptions{Address: listener.Addr().String()}).(*redisStandaloneWriter)
	defer w.client.Close()
	w.StartWrite(context.Background())
	acked := make(chan struct{}, 1)
	e := &entry.Entry{Argv: []string{"SET", "test", "value"}, OnWritten: func() { acked <- struct{}{} }}
	e.Parse()
	w.Write(e)
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("target did not receive command")
	}
	require.Empty(t, acked, "buffering and socket flush must not count as target ACK")
	close(release)
	select {
	case <-acked:
	case <-time.After(time.Second):
		t.Fatal("target ACK did not advance progress")
	}
	w.Close()
	<-serverDone
}

type captureWriter struct {
	Writer
	entries []*entry.Entry
}

func (w *captureWriter) Write(e *entry.Entry) { w.entries = append(w.entries, e) }

func TestClusterBroadcastWaitsForAllTargets(t *testing.T) {
	a, b := &captureWriter{}, &captureWriter{}
	w := &RedisClusterWriter{writers: []Writer{a, b}}
	count := 0
	e := &entry.Entry{Argv: []string{"PING"}, OnWritten: func() { count++ }}
	w.Write(e)
	require.NotSame(t, a.entries[0], b.entries[0])
	a.entries[0].OnWritten()
	a.entries[0].OnWritten()
	require.Zero(t, count)
	b.entries[0].OnWritten()
	require.Equal(t, 1, count)
}
