# 测试目录

所有自动化测试源码、测试辅助函数和后续新增的测试资源统一放在此目录。

```text
tests/
├── run.go                         # 统一执行入口
├── browser/routes.cjs             # 页面 URL 与导航的无界面浏览器回归
├── cmd/redisshakeweb/*_test.go     # Web、配置、认证与嵌入资源测试
└── internal/
    ├── aof/*_test.go
    ├── client/*_test.go
    ├── commands/*_test.go
    ├── filter/*_test.go
    ├── rdb/types/*_test.go
    ├── reader/*_test.go
    └── utils/*_test.go
```

## 运行

在仓库根目录执行，只需要项目已要求的 Go 工具链：

```bash
# 全部测试
go run ./tests

# 竞态检查
go run ./tests test -race ./...

# 指定包和用例，包路径仍使用业务代码的位置
go run ./tests test ./cmd/redisshakeweb -run TestFrontendAssetsAreEmbedded

# 包含测试源码的静态检查
go run ./tests vet ./...

# 查看实际发现的测试文件
go run ./tests list -f '{{.ImportPath}} {{.TestGoFiles}}' ./...

# 基准测试
go run ./tests test ./internal/filter -run '^$' -bench RunFunction -benchmem
```

`test`、`vet`、`list` 后的参数原样传给对应 Go 命令；提供参数时请同时指定包路径。省略命令及参数时默认执行 `test ./...`。

## Go 包内测试

这些测试需要访问业务包中的未导出函数，不能作为 `tests/cmd/...` 或 `tests/internal/...` 的独立包编译。入口通过 Go 的 `-overlay` 将测试源码临时映射到对应业务包，并从包发现过程中隐藏 `tests/` 下的副本。例如：

```text
tests/cmd/redisshakeweb/auth_test.go → cmd/redisshakeweb/auth_test.go
```

映射仅对当前 Go 命令有效，不在业务目录写入文件，也不改变生产代码、包名、嵌入资源或测试工作目录。临时映射文件在命令结束后删除，测试失败会返回非零退出状态。多个测试命令可独立运行。

请使用上述入口替代裸 `go test ./...`；直接测试 `tests/` 下的业务测试目录会因缺少同包实现而失败。生产构建继续使用 `go build ./cmd/redisshakeweb`。

新增 Go 用例和包内辅助函数使用 `*_test.go` 文件，放在 `tests/` 下与业务包对应的目录中，入口自动发现，无需维护文件清单。测试数据应放在 `tests/` 下；运行时按业务包工作目录引用相应路径，文件系统读取不会经过编译期 overlay。浏览器检查沿用 [项目测试流程](../AGENTS.md)，仅验证桌面端，截图和运行日志保存在忽略的 `.redis-shake-web/dev` 目录。

## 页面路由回归

先启动开发 Web 服务。`browser/routes.cjs` 使用无界面 Playwright 和隔离上下文，拦截全部 `/api/**` 请求；覆盖直接访问、刷新、前进后退、新标签页、登录返回原地址、创建和编辑后的导航、分页保留、已删除记录及异步请求乱序。不会修改真实连接、任务或 Redis 数据。

需要 Node.js、可被 Node 解析的 `playwright` 包及其 Chromium。已有工具链提供 Playwright 时直接使用，必要时通过 `NODE_PATH` 指定模块目录；否则可将依赖安装到忽略的运行目录：

```bash
npm install --prefix .redis-shake-web/dev/browser-tools --no-save --package-lock=false playwright
node .redis-shake-web/dev/browser-tools/node_modules/playwright/cli.js install chromium
NODE_PATH="$PWD/.redis-shake-web/dev/browser-tools/node_modules" node tests/browser/routes.cjs
```

默认验证 `http://127.0.0.1:8080`，可用 `TEST_BASE_URL` 指定其他开发实例。截图写入 `.redis-shake-web/dev/routes/`，浏览器在成功或失败时都会关闭。

## 消费位点回归

`browser/consumed_offsets.cjs` 同样使用隔离的无界面浏览器和 API fixtures，覆盖详情按节点展示任务上报的消费位点和延迟、大整数、零位点、未知位点、采集失败、过期上报、停止后的历史记录，以及三列任务列表、当前页轮询、搜索与焦点保持、错误恢复和空表尺寸；截图写入 `.redis-shake-web/dev/offset-ui/`。

```bash
node tests/browser/consumed_offsets.cjs
```

Go 用例覆盖目标回复前不推进、并行回复的连续确认、转换及广播命令的全部确认、内存窗口限流、RESP 读取缓冲边界、SQLite 定时保存与停止补存、保存失败后重试，以及重开数据库读取历史位点。另覆盖逐节点 master 查询、副本追溯 master、复制历史匹配、位点差值、采集失败及不发生消费时继续上报。

真实同步回归需要 Docker 和本地 `redis:7.2` 镜像。脚本创建两个临时 Redis 容器，使用随机端口和独立应用目录，验证全量与增量同步、过滤规则、内存缓存、每 5 秒保存、目标端暂停写入确认时的延迟上报、恢复后的消费推进、停止补存及 runner 重启后读取。不会连接现有 Redis 或修改开发实例的任务；成功或失败均清理本次创建的容器和进程，日志保留在 `.redis-shake-web/dev/offset-e2e-*/`。

```bash
go build -o bin/redis-shake-web-dev-next ./cmd/redisshakeweb
python3 tests/integration/consumed_offsets.py
```

可用 `TEST_BINARY` 指定其他已构建程序。保存的位点只用于展示，不用于断点续传；异常退出时保留最近一次成功保存的记录。
