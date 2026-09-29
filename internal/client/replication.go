package client

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/Pika-Yalei/RedisShake-Web/internal/client/proto"
)

// MasterPosition is sampled from the actual master, not a replica's local head.
type MasterPosition struct {
	Node                    string
	Offset                  int64
	ReplicationID           string
	PreviousReplicationID   string
	SecondReplicationOffset int64
}

// ReadMasterPosition uses a separate, short-lived connection. A metrics failure
// returns an error and must not terminate or block the replication connection.
func ReadMasterPosition(ctx context.Context, address, username, password string, useTLS bool, tlsConfig TlsConfig) (MasterPosition, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for hop := 0; hop < 3; hop++ {
		info, err := replicationInfo(ctx, address, username, password, useTLS, tlsConfig)
		if err != nil {
			return MasterPosition{}, fmt.Errorf("读取 master 位点失败 (%s): %w", address, err)
		}
		if info["role"] == "master" {
			offset, err := strconv.ParseInt(info["master_repl_offset"], 10, 64)
			if err != nil || offset < 0 || info["master_replid"] == "" {
				return MasterPosition{}, fmt.Errorf("master 复制信息不完整 (%s)", address)
			}
			second, err := strconv.ParseInt(info["second_repl_offset"], 10, 64)
			if err != nil {
				second = -1
			}
			return MasterPosition{Node: address, Offset: offset, ReplicationID: info["master_replid"], PreviousReplicationID: info["master_replid2"], SecondReplicationOffset: second}, nil
		}
		if info["role"] != "slave" && info["role"] != "replica" {
			return MasterPosition{}, fmt.Errorf("无法识别源节点角色 (%s)", address)
		}
		port, err := strconv.Atoi(info["master_port"])
		if info["master_host"] == "" || err != nil || port < 1 || port > 65535 {
			return MasterPosition{}, fmt.Errorf("源节点未上报 master 地址 (%s)", address)
		}
		address = net.JoinHostPort(info["master_host"], strconv.Itoa(port))
	}
	return MasterPosition{}, fmt.Errorf("源节点的 master 拓扑尚未稳定")
}

func replicationInfo(ctx context.Context, address, username, password string, useTLS bool, tlsConfig TlsConfig) (map[string]string, error) {
	dialer := &net.Dialer{}
	var conn net.Conn
	var err error
	if useTLS {
		cfg, loadErr := loadTlsConfig(tlsConfig)
		if loadErr != nil {
			return nil, loadErr
		}
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: cfg}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	buffer := bufio.NewWriter(conn)
	writer, reader := proto.NewWriter(buffer), proto.NewReader(bufio.NewReader(conn))
	command := func(args ...interface{}) (interface{}, error) {
		if err := writer.WriteArgs(args); err != nil {
			return nil, err
		}
		if err := buffer.Flush(); err != nil {
			return nil, err
		}
		return reader.ReadReply()
	}
	if username != "" || password != "" {
		var reply interface{}
		if username != "" {
			reply, err = command("AUTH", username, password)
		} else {
			reply, err = command("AUTH", password)
		}
		if err != nil {
			return nil, err
		}
		if reply != "OK" {
			return nil, fmt.Errorf("Redis 认证失败")
		}
	}
	reply, err := command("INFO", "replication")
	if err != nil {
		return nil, err
	}
	text, ok := reply.(string)
	if !ok {
		return nil, fmt.Errorf("无效的 INFO replication 响应")
	}
	info := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), ":"); ok {
			info[key] = value
		}
	}
	return info, nil
}
