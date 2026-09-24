# RedisShake Web

基于 [RedisShake v4.6.2](https://github.com/tair-opensource/RedisShake/releases/tag/v4.6.2) 的自部署 Redis 迁移管理界面。通过中文 Web 页面保存连接、配置规则、预检并启动单向全量迁移；全量完成后持续同步增量。前端使用原生 JavaScript，服务端使用 Go 与 SQLite；每个迁移任务由独立进程运行内核。

当前是开发版本。隔离环境已完成单机、Sentinel、Cluster 九种拓扑组合，以及 Redis 6.2、7.2、8.0 九个版本方向的小数据量全量冒烟；部分组合验证了增量。目标覆盖、DB 映射、Key 与增量命令过滤、目标 DB 清理和 Web 独立重启也已通过测试。四平台实机运行、复杂命令和 24 小时稳定性尚未验收；不要把冒烟结果当作生产支持承诺。实测记录见 [验证状态](docs/verification.md)。

## 快速启动

开发方式在仓库目录执行 `bash scripts/dev.sh`。脚本从同一 Go 模块构建一个包含 Web 和 RedisShake 内核的程序，使用仓库内被忽略的 `.redis-shake-web/dev` 数据目录，并直接读取前端源码；修改 JS、CSS、HTML 后刷新浏览器即可看到结果。需要 Go 1.26，不必先单独下载或构建内核。默认访问 <http://127.0.0.1:8080>，首次打开页面在初始化表单中确认或修改预填的账号 `admin` 与密码 `RedisShake@123456`，点击“创建管理员账号”后才会保存。修改 Go 代码后需重新运行脚本；执行器会继续运行，如需更新执行器程序，先在页面停止任务，再执行 `bin/redis-shake-web-dev shutdown --socket-dir .redis-shake-web/dev` 并重新启动。

已有 Docker 和网络访问条件时，在仓库目录执行：

```bash
docker compose up -d --build
```

打开 <http://127.0.0.1:8080>，设置唯一管理员的账号和密码后登录。默认只监听本机回环地址；远程访问请自行配置 HTTPS 反向代理并控制访问范围。执行器所在机器必须能直接访问源、目标以及集群发现的每个 Redis 节点。

已有 Go 1.26 时，原生包可用下面的命令构建，例如当前系统：

```bash
bash scripts/build.sh
cd dist/redis-shake-web-$(go env GOOS)-$(go env GOARCH)
./redis-shake-web serve
```

启动时在终端显示 Web 地址。前端资源已内嵌到 `redis-shake-web` 可执行文件，直接访问该地址即可，无需单独部署前端服务。`serve` 会启动常驻执行器，然后运行 Web 服务；关闭 Web 进程不会停止已有同步任务。重新执行 `./redis-shake-web serve` 即可接回执行器。要有意停止执行器及任务，先在 Web 停止任务，再运行 `./redis-shake-web shutdown`。默认数据目录为 `~/.redis-shake-web`，可给命令传入 `--data-dir`；若单独设置 `--socket-dir`，后续管理命令也要使用同一参数。

支持交叉构建 `darwin/amd64`、`darwin/arm64`、`linux/amd64`、`linux/arm64`，例如 `bash scripts/build.sh linux amd64`。执行 `bash scripts/package.sh` 可在 `dist/release/` 生成四个可解压运行的 `.tar.gz` 包和 `SHA256SUMS`。RedisShake 内核代码已直接合入项目的同一个 Go 模块：[单任务入口](internal/kernel/run.go)与[内部包](internal)基于官方 v4.6.2，包含本项目的增量过滤、进程存活与安全处理修改；[MIT 许可证](REDISSHAKE-LICENSE.txt)保留在仓库根目录。发布包只有一个 `redis-shake-web` 可执行文件：Web 将请求交给常驻执行器，执行器每启动一个任务，就以内部 `task` 模式创建一个独立进程。Web 重启不影响已运行的任务进程。构建产物还包含文档、版本记录及 RedisShake 许可证；Docker 镜像也从仓库内源码构建。

## 使用流程

1. 在“连接管理”保存源端和目标端，分别测试连接。支持自建 Redis 单机、Sentinel 和 Cluster，明文连接、密码和 ACL；暂不提供 Redis TLS。
2. 在“同步任务”通过四步向导选择连接、设置一对一 DB 映射与 Key 规则、执行预检、确认启动。命令包含/排除规则仅作用于增量同步；默认要求目标 DB 为空，也可选择覆盖同名 Key。
3. 运行中查看阶段和 RedisShake 原始日志。停止或失败后可重新全量运行；没有断点续传或自动重试。目标端清理是独立危险操作，必须确认目标连接名称；会清空选定目标 DB 内的**全部 Key**，不受任务 Key 过滤限制。
4. 切换业务由使用者自行安排和核验。产品不提供数据一致性校验或应用写入冲突处理；目标端应用写入可能被后续源端变更覆盖。

单机和 Sentinel 可选择多个源 DB，但一个源 DB 只能映射到一个目标 DB，多个源 DB 不能合并；Cluster 仅有 DB 0。跨版本迁移遇到目标版本不支持的数据或命令时任务会失败，已写入目标的数据不会自动回滚。源端复制流中未过滤的 `FLUSHDB`/`FLUSHALL` 会使任务停止，避免整库清空绕过 Key 筛选。源端 Sentinel 默认从健康副本同步；预检找不到副本时阻止启动。这两项是当前安全默认值，可在需求确认后调整。

## 运维

- Docker 中只重启 Web：`docker compose restart web`。同步进程由 `runner` 容器管理，会继续运行。重启 runner、整组服务或主机将中断同步；恢复后需要人工确认并重新全量。
- SQLite、加密密钥和运行日志位于数据目录。备份时应同时保存整个目录；密钥丢失会使已保存的 Redis 凭据无法解密。任务配置文件含 Redis 凭据，运行期间权限为当前运行用户独占，任务结束后删除。
- 日志在任务详情查看。RedisShake 日志轮转为单文件上限 64 MiB、最多 3 个备份；页面返回日志末尾，历史运行记录可切换查看。

详细需求、设计和验证边界见 [docs](docs/README.md)。
