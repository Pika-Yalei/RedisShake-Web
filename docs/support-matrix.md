# 迁移与运行支持矩阵

更新日期：2026-09-23  
状态：九种拓扑组合已完成小数据量全量冒烟，部分完成增量测试；具体版本和剩余验收见 [验证记录](verification.md)。下表保留完整需求范围，不代表全面发布支持。

## 已确认的迁移类型

核心模式：单向 A → B，全量迁移后长期同步增量，需要切换业务时由用户执行。失败报错停止，由用户确认重新全量；不做自动重试和断点续传。两端均可有业务写入，产品正常重放源端命令，不处理应用写入冲突。双向及防循环后续再做，见 [长期同步设计议题](long-running-sync.md)。

用户已要求支持单机、哨兵和 Redis Cluster。以下九类组合均纳入设计及验证，具体版本、数据规则和运行限制确认后才能形成发布支持承诺。

| 源端 → 目标端 | 单机 | 哨兵管理的 Redis | Redis Cluster |
| --- | --- | --- | --- |
| 单机 | 全量、增量冒烟通过 | 全量冒烟通过 | 全量冒烟通过 |
| 哨兵管理的 Redis | 全量、增量冒烟通过 | 全量冒烟通过 | 全量、增量冒烟通过 |
| Redis Cluster | 全量冒烟通过 | 全量、增量冒烟通过 | 全量、增量冒烟通过 |

已确认版本范围：Redis 6.x、7.x、8.x；第一版只支持自建 Redis，不纳入托管云 Redis 产品适配。

已确认规则范围：DB 选择与一对一映射、Key 前缀/正则包含或排除、仅增量命令过滤；不做数据类型或命令组过滤。不支持多个源 DB 合并到同一个目标 DB；迁到集群时选择一个源 DB 映射到 DB 0。目标端提供要求为空和覆盖同名 Key 两种策略，默认要求为空。

认证方案包含无认证、密码和 ACL 用户名/密码；第一版只连接明文 Redis，不做 TLS/mTLS。

待补充维度：具体小版本、过滤与映射细节、数据类型、大 Key 及测试负载。Redis 8.x 不意味着自动承诺选定内核无法解析的未来小版本或模块数据。

## 已确认的版本方向

用户要求 Redis 6/7/8 之间全部方向支持，包含降级。下表是实现与验证范围，不是无条件兼容承诺。

| 源端 → 目标端 | Redis 6.x | Redis 7.x | Redis 8.x |
| --- | --- | --- | --- |
| Redis 6.x | 6.2→6.2 全量、增量冒烟通过 | 6.2→7.2 全量冒烟通过 | 6.2→8.0 全量、增量冒烟通过 |
| Redis 7.x | 7.2→6.2 全量、增量冒烟通过 | 7.2→7.2 全量、增量冒烟通过 | 7.2→8.0 全量冒烟通过 |
| Redis 8.x | 8.0→6.2 全量、增量冒烟通过 | 8.0→7.2 全量冒烟通过 | 8.0→8.0 全量、增量冒烟通过 |

该版本矩阵与九类拓扑组合交叉形成测试范围。具体小版本及测试分层后续确定，不能通过只测单机同版本来覆盖全部组合。

RedisShake 官方降级方案可将 RDB 数据转换成 RESP 命令重建，以规避部分二进制格式差异；它不能让旧 Redis 获得不存在的命令或数据语义。[跨版本说明](https://tair-opensource.github.io/RedisShake/en/others/version.html)

固定内核版本后，应逐项核对数据类型与命令能力。例如当前公开兼容性文档列出 Vector Sets 与 Redis Stack 模块不受支持，不能因用户选择 Redis 8.x 就默认承诺这些数据可以迁移。[版本兼容性说明](https://tair-opensource.github.io/RedisShake/en/others/compatibility.html)

不兼容数据或命令报错停止，不自动跳过。预检发现则阻止启动，运行中发现则失败停止；不能预先保证未来增量不会出现不兼容命令。降级的复合结构重建、覆盖策略、TTL 和仅增量命令过滤必须组合验证。

## 接入方式与内核证据

以下基于 RedisShake 当前公开文档，最终需在选定的固定版本上复核。

| 类型 | 已核实的内核配置或行为 | Web 实现需要明确的事项 |
| --- | --- | --- |
| 单机 | `sync_reader` 从 Redis 获取全量和增量数据，`redis_writer` 写入目标。 | 连接、认证、复制权限、版本及目标端可写检查。 |
| 哨兵 | 文档提供源端和目标端的 Sentinel 配置，可从 Sentinel 获取主节点地址。 | Sentinel 与 Redis 两组连接参数；主节点发现、源端副本选择、故障切换后的任务行为。 |
| 集群 | 读写组件有集群模式；源端可根据集群节点发现其他节点。 | 发现后的节点可达性、节点级状态汇总、目标端跨槽命令限制。 |

来源：[Sync Reader](https://tair-opensource.github.io/RedisShake/en/reader/sync_reader.html)、[迁移模式选择](https://tair-opensource.github.io/RedisShake/en/guide/mode.html)、[Redis Writer](https://github.com/tair-opensource/RedisShake/blob/v4/docs/src/en/writer/redis_writer.md)。

## 需要单独设计与验证的限制

1. **哨兵源端读哪个节点**：官方文档提醒，直接从被 Sentinel 管理的主节点执行同步可能让 RedisShake 被识别为副本，并建议选择副本作为源端。要验证如何发现和选择源端副本，以及没有可用副本时如何反馈。[迁移模式选择](https://tair-opensource.github.io/RedisShake/en/guide/mode.html)
2. **接入哨兵不等于自动恢复**：启动时发现节点，与迁移中故障切换后的继续同步是不同能力。RedisShake 4.x 没有断点续传与集群拓扑变化感知；产品在任务失败后报错停止，由用户确认重新全量。[内核限制](https://github.com/tair-opensource/RedisShake#limitations)
3. **集群目标的数据规则**：只接受一个源 DB 映射到目标 DB 0；还需验证多 Key 命令是否满足目标集群的槽位要求。[Redis Writer 的目标端限制](https://github.com/tair-opensource/RedisShake/blob/v4/docs/src/en/writer/redis_writer.md)
4. **复制能力预检**：即使只支持自建 Redis，也需要确认源端复制权限与相关命令是否可用。建议无法满足全量加增量要求时明确阻断，不自动切换成其他模式。托管云 Redis 的适配留在第一版范围之外。[迁移模式选择](https://tair-opensource.github.io/RedisShake/en/guide/mode.html)
5. **Key 冲突策略有阶段差异**：内核的 RDB 恢复冲突配置不控制增量命令直接重放，部分大数据恢复路径也可能不适用。产品不能将“跳过已有 Key”表述成整个迁移期间都不会修改这些 Key。[重复 Key 处理说明](https://github.com/tair-opensource/RedisShake/blob/v4/docs/src/en/writer/redis_writer.md)

## 运行与部署矩阵

| 交付方式 | 第一版范围 | 待明确事项 |
| --- | --- | --- |
| 服务器 Docker | linux/amd64、linux/arm64；Web 与执行器两个服务 | 最低 Docker 版本；独立重启与整组重启分别验证。 |
| Linux 直接运行 | x86_64、ARM64；解压后单命令启动 | 最低发行版/运行库要求；执行器与终端分离需验证。 |
| macOS 直接运行 | Intel x86_64、Apple Silicon ARM64；解压后单命令启动 | 最低系统版本；执行器与终端分离需验证。 |
| Windows 直接运行 | 本次选择未纳入 | 不作为第一版交付验收要求。 |

访问模型：全新数据目录自动创建唯一管理员，默认账号 `admin`、密码 `RedisShake@123456`；首次登录后修改密码。不提供多账号、成员、角色及权限管理。详细初始化、修改/重置、会话和 Web 重启接管见 [运行设计](runtime-design.md)。

网络范围：执行器直接连接源、目标及发现的实际节点，不做 SSH 隧道或远程 Agent。统一使用默认性能参数，页面不提供性能设置。

## 验证记录要求

每个实测组合记录：Web 版本、RedisShake 版本与制品校验值、源/目标版本与拓扑、部署平台、配置摘要（脱敏）、数据集、操作步骤、预期与实际结果、已知限制。

发布时区分「已验证支持」「明确不支持」「尚未验证」，不能把计划表当作测试结果。
