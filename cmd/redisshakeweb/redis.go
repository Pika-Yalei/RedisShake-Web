package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

func validateConnection(c Connection) error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("连接名称不能为空")
	}
	if c.Kind != "standalone" && c.Kind != "sentinel" && c.Kind != "cluster" {
		return errors.New("连接类型不受支持")
	}
	address := c.Address
	if c.Kind == "sentinel" {
		address = c.SentinelAddress
		if c.SentinelMaster == "" {
			return errors.New("请填写哨兵主节点名称")
		}
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		return errors.New("地址应为 host:port")
	}
	return nil
}

func redisClient(c Connection, db int) (redis.UniversalClient, error) {
	if err := validateConnection(c); err != nil {
		return nil, err
	}
	opts := &redis.UniversalOptions{
		Addrs: []string{c.Address}, Username: c.Username, Password: c.Password,
		DB: db, DialTimeout: 4 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
	}
	switch c.Kind {
	case "cluster":
		return redis.NewClusterClient(&redis.ClusterOptions{Addrs: opts.Addrs, Username: opts.Username, Password: opts.Password, DialTimeout: opts.DialTimeout, ReadTimeout: opts.ReadTimeout, WriteTimeout: opts.WriteTimeout}), nil
	case "sentinel":
		return redis.NewFailoverClient(&redis.FailoverOptions{MasterName: c.SentinelMaster, SentinelAddrs: []string{c.SentinelAddress}, SentinelUsername: c.SentinelUsername, SentinelPassword: c.SentinelPassword, Username: c.Username, Password: c.Password, DB: db, DialTimeout: opts.DialTimeout, ReadTimeout: opts.ReadTimeout, WriteTimeout: opts.WriteTimeout}), nil
	default:
		return redis.NewClient(&redis.Options{Addr: c.Address, Username: c.Username, Password: c.Password, DB: db, DialTimeout: opts.DialTimeout, ReadTimeout: opts.ReadTimeout, WriteTimeout: opts.WriteTimeout}), nil
	}
}

type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func checkRedis(ctx context.Context, c Connection) []Check {
	checks := []Check{}
	client, err := redisClient(c, 0)
	if err != nil {
		return append(checks, Check{Name: "连接配置", Message: err.Error()})
	}
	defer client.Close()
	if err := client.Ping(ctx).Err(); err != nil {
		return append(checks, Check{Name: "连接与认证", Message: err.Error()})
	}
	checks = append(checks, Check{Name: "连接与认证", OK: true, Message: "连接成功"})
	info, err := client.Info(ctx, "server").Result()
	if err != nil {
		return append(checks, Check{Name: "Redis 版本", Message: err.Error()})
	}
	version := ""
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "redis_version:") {
			version = strings.TrimSpace(strings.TrimPrefix(line, "redis_version:"))
			break
		}
	}
	parts := strings.Split(version, ".")
	major, _ := strconv.Atoi(parts[0])
	if major < 6 || major > 8 {
		checks = append(checks, Check{Name: "Redis 版本", Message: "仅支持 Redis 6.x、7.x、8.x；检测到 " + version})
	} else {
		checks = append(checks, Check{Name: "Redis 版本", OK: true, Message: version})
	}
	if c.Kind == "cluster" {
		if cluster, ok := client.(*redis.ClusterClient); ok {
			err := cluster.ForEachMaster(ctx, func(ctx context.Context, node *redis.Client) error { return node.Ping(ctx).Err() })
			checks = append(checks, Check{Name: "集群主节点", OK: err == nil, Message: messageOf(err, "全部可达")})
		}
	}
	return checks
}

func messageOf(err error, good string) string {
	if err != nil {
		return err.Error()
	}
	return good
}

func redisVersion(ctx context.Context, c Connection) ([3]int, error) {
	client, err := redisClient(c, 0)
	if err != nil {
		return [3]int{}, err
	}
	defer client.Close()
	info, err := client.Info(ctx, "server").Result()
	if err != nil {
		return [3]int{}, err
	}
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "redis_version:") {
			parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "redis_version:")), ".")
			var result [3]int
			for i := 0; i < len(parts) && i < 3; i++ {
				result[i], _ = strconv.Atoi(parts[i])
			}
			return result, nil
		}
	}
	return [3]int{}, errors.New("无法读取 Redis 版本")
}

func redisProcessIDs(ctx context.Context, c Connection) (map[string]bool, error) {
	client, err := redisClient(c, 0)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	ids := map[string]bool{}
	var idMu sync.Mutex
	readID := func(ctx context.Context, node *redis.Client) error {
		info, err := node.Info(ctx, "server").Result()
		if err != nil {
			return err
		}
		for _, line := range strings.Split(info, "\n") {
			if strings.HasPrefix(line, "run_id:") {
				idMu.Lock()
				ids[strings.TrimSpace(strings.TrimPrefix(line, "run_id:"))] = true
				idMu.Unlock()
				return nil
			}
		}
		return errors.New("Redis 未返回实例身份")
	}
	if cluster, ok := client.(*redis.ClusterClient); ok {
		err = cluster.ForEachMaster(ctx, readID)
	} else if node, ok := client.(*redis.Client); ok {
		err = readID(ctx, node)
	} else {
		info, infoErr := client.Info(ctx, "server").Result()
		err = infoErr
		if err == nil {
			for _, line := range strings.Split(info, "\n") {
				if strings.HasPrefix(line, "run_id:") {
					ids[strings.TrimSpace(strings.TrimPrefix(line, "run_id:"))] = true
				}
			}
			if len(ids) == 0 {
				err = errors.New("Redis 未返回实例身份")
			}
		}
	}
	return ids, err
}

func versionIsOlder(target, source [3]int) bool {
	for i := range target {
		if target[i] != source[i] {
			return target[i] < source[i]
		}
	}
	return false
}

func validateTask(t Task, source, target Connection) error {
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("任务名称不能为空")
	}
	if t.SourceID == "" || t.TargetID == "" {
		return errors.New("请选择源连接和目标连接")
	}
	if t.SourceID == t.TargetID || source.Kind == target.Kind && source.Address != "" && source.Address == target.Address {
		return errors.New("源端与目标端不能是同一连接")
	}
	if t.TargetPolicy != "require_empty" && t.TargetPolicy != "overwrite" {
		return errors.New("目标数据策略无效")
	}
	if len(t.DBMap) == 0 {
		return errors.New("至少选择一个源 DB")
	}
	targetDBs := map[int]bool{}
	for rawSource, targetDB := range t.DBMap {
		sourceDB, err := strconv.Atoi(rawSource)
		if err != nil || sourceDB < 0 || targetDB < 0 {
			return errors.New("DB 编号必须是非负整数")
		}
		if source.Kind == "cluster" && sourceDB != 0 {
			return errors.New("集群源端仅支持 DB 0")
		}
		if target.Kind == "cluster" && (targetDB != 0 || len(t.DBMap) != 1) {
			return errors.New("集群目标只能由一个源 DB 映射到 DB 0")
		}
		if targetDBs[targetDB] {
			return errors.New("多个源 DB 不能映射到同一目标 DB")
		}
		targetDBs[targetDB] = true
	}
	for _, expr := range append(append([]string{}, t.Rules.AllowRegex...), t.Rules.BlockRegex...) {
		if _, err := regexp.Compile(expr); err != nil {
			return fmt.Errorf("Key 正则无效：%w", err)
		}
	}
	for _, command := range append(append([]string{}, t.Rules.AllowCommands...), t.Rules.BlockCommands...) {
		if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`).MatchString(command) {
			return errors.New("增量命令名称无效")
		}
	}
	return nil
}

func preflight(ctx context.Context, t Task, source, target Connection, taskBinary string) []Check {
	checks := []Check{}
	if err := validateTask(t, source, target); err != nil {
		return []Check{{Name: "任务配置", Message: err.Error()}}
	}
	checks = append(checks, Check{Name: "任务配置", OK: true, Message: "配置有效"})
	for _, item := range []struct {
		name string
		c    Connection
	}{{"源端", source}, {"目标端", target}} {
		for _, check := range checkRedis(ctx, item.c) {
			check.Name = item.name + " · " + check.Name
			checks = append(checks, check)
		}
	}
	sourceIDs, sourceErr := redisProcessIDs(ctx, source)
	targetIDs, targetErr := redisProcessIDs(ctx, target)
	if sourceErr != nil || targetErr != nil {
		checks = append(checks, Check{Name: "源与目标身份", Message: "无法确认两端是否为同一 Redis 实例"})
	} else {
		same := false
		for id := range sourceIDs {
			if targetIDs[id] {
				same = true
			}
		}
		checks = append(checks, Check{Name: "源与目标身份", OK: !same, Message: map[bool]string{true: "源端与目标端包含同一 Redis 实例", false: "两端实例不同"}[same]})
	}
	if source.Kind == "sentinel" {
		client, err := redisClient(source, 0)
		if err == nil {
			info, e := client.Info(ctx, "replication").Result()
			_ = client.Close()
			if e != nil || !strings.Contains(info, "connected_slaves:") || strings.Contains(info, "connected_slaves:0") {
				checks = append(checks, Check{Name: "源端副本", Message: "未发现可用副本；哨兵模式默认从副本同步"})
			} else {
				checks = append(checks, Check{Name: "源端副本", OK: true, Message: "存在副本；内核启动时重新选择"})
			}
		}
	}
	for _, db := range targetDBList(t) {
		client, err := redisClient(target, db)
		if err == nil {
			err = client.Ping(ctx).Err()
			_ = client.Close()
		}
		checks = append(checks, Check{Name: fmt.Sprintf("目标 DB %d", db), OK: err == nil, Message: messageOf(err, "DB 可访问")})
	}
	if t.TargetPolicy == "require_empty" {
		seen := map[int]bool{}
		for _, db := range t.DBMap {
			if seen[db] {
				continue
			}
			seen[db] = true
			client, err := redisClient(target, db)
			if err != nil {
				checks = append(checks, Check{Name: "目标 DB 空检查", Message: err.Error()})
				continue
			}
			var count int64
			if cluster, ok := client.(*redis.ClusterClient); ok {
				var countMu sync.Mutex
				err = cluster.ForEachMaster(ctx, func(ctx context.Context, node *redis.Client) error {
					n, e := node.DBSize(ctx).Result()
					countMu.Lock()
					count += n
					countMu.Unlock()
					return e
				})
			} else {
				count, err = client.DBSize(ctx).Result()
			}
			_ = client.Close()
			ok := err == nil && count == 0
			msg := fmt.Sprintf("DB %d：%d 个 Key", db, count)
			if err != nil {
				msg = err.Error()
			}
			checks = append(checks, Check{Name: "目标 DB 空检查", OK: ok, Message: msg})
		}
	}
	if _, err := os.Stat(taskBinary); err != nil {
		checks = append(checks, Check{Name: "任务进程", Message: "任务进程程序不可用"})
	} else {
		checks = append(checks, Check{Name: "任务进程", OK: true, Message: "单任务进程已就绪"})
	}
	return checks
}

func targetDBList(t Task) []int {
	seen := map[int]bool{}
	for _, db := range t.DBMap {
		seen[db] = true
	}
	out := make([]int, 0, len(seen))
	for db := range seen {
		out = append(out, db)
	}
	sort.Ints(out)
	return out
}
