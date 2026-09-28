package client

import (
	"bufio"
	"context"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Pika-Yalei/RedisShake-Web/internal/client/proto"
)

func TestRedisClientAuthenticationCommands(t *testing.T) {
	for _, tc := range []struct {
		name, user, password string
		auth                 []interface{}
	}{
		{"none", "", "", nil},
		{"password", "", "secret", []interface{}{"auth", "secret"}},
		{"username", "reader", "", []interface{}{"auth", "reader", ""}},
		{"username and password", "reader", "secret", []interface{}{"auth", "reader", "secret"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			commands := make(chan [][]interface{}, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				reader := proto.NewReader(bufio.NewReader(conn))
				seen := [][]interface{}{}
				for {
					value, err := reader.ReadReply()
					if err != nil {
						return
					}
					command := value.([]interface{})
					seen = append(seen, command)
					if strings.EqualFold(command[0].(string), "ping") {
						_, _ = conn.Write([]byte("+PONG\r\n"))
						commands <- seen
						return
					}
					_, _ = conn.Write([]byte("+OK\r\n"))
				}
			}()
			c := NewRedisClient(context.Background(), listener.Addr().String(), tc.user, tc.password, false, TlsConfig{}, false)
			c.Close()
			select {
			case seen := <-commands:
				if tc.auth == nil {
					if len(seen) != 1 {
						t.Fatal("no-auth client sent AUTH")
					}
				} else if len(seen) != 2 || !reflect.DeepEqual(seen[0], tc.auth) {
					t.Fatalf("wrong AUTH command: %v", seen)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("server did not receive handshake")
			}
		})
	}
}
