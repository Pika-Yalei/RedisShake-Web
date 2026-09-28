package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"time"

	"github.com/Pika-Yalei/RedisShake-Web/internal/client/proto"
	"github.com/redis/go-redis/v9"
)

// go-redis omits AUTH when the password is empty. Authenticate named nopass
// users before its HELLO/SELECT handshake, on every new pooled connection.
func redisAuthDialer(username, password string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if username == "" || password != "" {
			return conn, nil
		}
		deadline := time.Now().Add(4 * time.Second)
		if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
			deadline = limit
		}
		if err = conn.SetDeadline(deadline); err == nil {
			writer := bufio.NewWriter(conn)
			err = proto.NewWriter(writer).WriteArgs([]interface{}{"AUTH", username, ""})
			if err == nil {
				err = writer.Flush()
			}
			if err == nil {
				var reply string
				reply, err = proto.NewReader(bufio.NewReader(conn)).ReadString()
				if err == nil && reply != "OK" {
					err = fmt.Errorf("unexpected AUTH response")
				}
			}
		}
		if err == nil {
			err = conn.SetDeadline(time.Time{})
		}
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("Redis 用户认证失败：%w", err)
		}
		return conn, nil
	}
}

// FailoverOptions shares a single Dialer between Redis and Sentinel. For empty
// passwords, resolve the master on each dial with a separate Sentinel client so
// the two authentication scopes can never be confused, including on reconnect.
func sentinelEmptyPasswordClient(c Connection, db int) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr: c.SentinelAddress, Username: c.Username, Password: c.Password, DB: db,
		DialTimeout: 4 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
		Dialer: func(ctx context.Context, network, _ string) (net.Conn, error) {
			sentinel := redis.NewSentinelClient(&redis.Options{
				Addr: c.SentinelAddress, Username: c.SentinelUsername, Password: c.SentinelPassword,
				DialTimeout: 4 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
				Dialer: redisAuthDialer(c.SentinelUsername, c.SentinelPassword),
			})
			defer sentinel.Close()
			address, err := sentinel.GetMasterAddrByName(ctx, c.SentinelMaster).Result()
			if err != nil {
				return nil, err
			}
			if len(address) != 2 {
				return nil, fmt.Errorf("哨兵未返回有效的主节点地址")
			}
			return redisAuthDialer(c.Username, c.Password)(ctx, network, net.JoinHostPort(address[0], address[1]))
		},
	})
}
