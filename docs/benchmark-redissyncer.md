# RedisSyncer 参考分析

核查日期：2026-09-23  
对标来源：用户指定的 [TraceNature/redissyncer-server](https://github.com/TraceNature/redissyncer-server)。

## 已核实内容

| 内容 | 一手资料 |
| --- | --- |
| RedisSyncer 提供同步服务端、客户端、Web 控制面板和数据校验工具等配套项目。 | [服务端 README](https://github.com/TraceNature/redissyncer-server) |
| 基本操作流程包含创建、启动、查看状态、停止和删除同步任务。 | [Quick Start 的使用基本步骤](https://github.com/TraceNature/redissyncer-server/blob/main/docs/quickstart.md) |
| 文档提供直接运行服务端和 Docker 部署方式。 | [Quick Start 的构建与启动](https://github.com/TraceNature/redissyncer-server/blob/main/docs/quickstart.md) |
| 任务创建返回任务标识；文档说明集群任务可能拆成多个节点任务。 | [Quick Start 的任务创建示例](https://github.com/TraceNature/redissyncer-server/blob/main/docs/quickstart.md) |
| 官方关联的 Dashboard 发布仓库说明了静态页面部署、API 反向代理和登录入口。 | [Dashboard README](https://github.com/TraceNature/dashboard_release) |

本次核查基于公开文档，没有运行参考项目或实测其页面。不能把文档中的接口流程等同于已核实的逐步 UI 交互。用户尚未指定必须复用的视觉风格或页面细节。

## 建议借鉴的产品行为（待确认）

1. 以迁移任务为主要操作对象，任务列表作为日常入口。
2. 配置与启动分开：用户先保存和检查配置，再明确启动。
3. 任务详情集中展示源端、目标端、同步阶段、运行记录和日志。
4. 停止执行与删除任务分别表达，避免用户把删除记录理解为撤销已迁移数据。
5. 如支持集群，以一个用户任务汇总各节点状态；节点细节在详情页展开。是否采用该设计需结合 RedisShake 实际进程与指标验证。

## 本产品的差异与能力边界

- 已确认：RedisShake Web 面向个人或小团队，提供 Docker 和本机直接运行两种方式；迁移内核仍为 RedisShake。
- 建议：将连接配置、预检查、启动和状态/日志查看组织成图形化流程，普通操作不要求填写原始内核配置。
- 已确认独立连接管理和连接复用；第一版不内置数据核验，不引入对标项目的配套数据校验功能。
- 已确认草稿与任务基本操作；具体日志展示与页面布局待细化。
- 待确认：参考产品中希望避免的操作，以及是否需要接近其外观。
- RedisSyncer README 列出的断点续传、文件导入等能力，不自动纳入本产品范围。[RedisSyncer 功能列表](https://github.com/TraceNature/redissyncer-server#功能列表)
- RedisShake 4.x 官方说明不支持断点续传，重启会重新全量同步。因此本产品的重试与恢复语义必须基于选定内核版本实测，不能直接沿用参考产品承诺。[RedisShake 限制说明](https://github.com/tair-opensource/RedisShake#limitations)

## 后续交互设计的输出要求

在支持范围明确后，将建议转成具体页面规格，至少说明：入口、字段、默认值、校验、按钮状态、加载与错误、成功反馈、刷新行为、任务状态转换和验收条件。
