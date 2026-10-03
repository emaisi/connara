# 工作流 v2 与受限 JavaScript 运行维护

本说明对应 `js-v1/goja-104bc28c3abd/protocol-1`。实现及本地验收记录见 [工作流开发验收记录](workflow-validation-report.md)。功能开关默认关闭；本文中的生产配置和切换步骤尚未在现有服务执行。

## 构建与配置

运行 `scripts/build.sh` 会先构建前端嵌入资源，再生成主服务、管理命令、`apihub-workflow-reschedule` 和静态链接的 `workflow-code-worker`。主服务与 worker 应作为同一发布产物分发。单独更新前端源码不会更新正在运行的 Go 服务。

配置项：

| 变量 | 默认值及作用 |
| --- | --- |
| `APIHUB_WORKFLOW_V2_ENABLED` | `false`；控制新 v2 定义编辑、部署和新触发 |
| `APIHUB_WORKFLOW_CODE_ENABLED` | `false`；控制新的代码预览、编译、部署及含代码流程的新触发。v2 开启时仍可保存代码草稿 |
| `APIHUB_WORKFLOW_CODE_WORKER_PATH` | 管理员设置的绝对可执行路径，例如 `/opt/connara/bin/workflow-code-worker` |
| `APIHUB_WORKFLOW_CODE_LAUNCHER_PATH` | 固定启动器绝对路径，例如 `/opt/connara/scripts/workflow-code-launcher.sh` |
| `APIHUB_WORKFLOW_CODE_MAX_CONCURRENCY` | `2`，有效范围 1–16，单个服务进程所有入口共享 |

关闭开关控制新接纳，不取消已经接受的运行。兼容 worker、启动器及读取器须保留到任务排空；关闭期间已接受任务仍执行原快照，不读取今天的源码、变量初值或业务日期。环境失效、身份失效或构建不兼容时明确失败。

## Linux 隔离前提

以非特权 Linux 服务账号运行。该账号需有可连接的 systemd user manager、cgroup v2 的 memory/pids 控制器及用户命名空间权限。固定启动器依赖 `/usr/bin/systemd-run`、`/usr/bin/systemctl`、`/usr/bin/bwrap`、`/usr/bin/awk`、`/usr/bin/id` 和 `/bin/bash`。由管理员按宿主发行版安装 bubblewrap，并将 worker 和启动器设置为服务账号可读、可执行的固定文件，建议文件由管理员维护。

systemd 系统服务也必须能连接此账号的 user bus（`/run/user/<uid>/bus`）；按宿主管理约定启用该账号 user manager 的持续运行，例如由管理员执行 `loginctl enable-linger <service-user>`。不要为通过自检而全局关闭宿主安全策略；命名空间或 cgroup 不可用时保留功能关闭。

启动器为每次请求创建独立 transient service：

- `MemoryMax=128M`、`MemorySwapMax=0`、`TasksMax=32`。
- `RuntimeMaxSec=2s`、`TimeoutStopSec=100ms`、`KillMode=control-group`、`NoNewPrivileges=yes`。
- bubblewrap 隔离全部命名空间，UID/GID 为 65534，无网络接口（仅隔离的 lo），不挂载宿主目录、凭据、缓存或临时文件。
- 仅只读挂载静态 worker、`/proc` 和内存限制文件。环境仅为 `TZ=UTC` 与启动 shell 的 `PWD=/`。

worker 在解析源码前检查身份、环境、NoNewPrivs、零 capability、128 MiB 内存上限、零 swap 上限、隔离网络和文件系统。主服务启用代码时启动探测；每次含代码运行在首个业务 API 前再次做环境及构建预检。预检不运行用户源码。纯 scheduler 可只配置开关而不安装本地 worker；其 HTTP 代码能力如实显示不可用。

服务进程还实施两秒 wall clock、限长协议、进程组强制终止、等待回收及 transient service 清理。不可确认 OOM 原因时报告 `code_worker_failed`，不猜测为内存超限。没有主进程内执行或放宽限制的回退。

## 各角色与容量

| 角色 | 要求 |
| --- | --- |
| all | 编译、预览、同步及后台共用一个 CodeRunner，安装匹配 worker/启动器 |
| api | 本地编译、预览、同步执行；本地探测不能证明后台集群可用 |
| worker | 所有领取 `workflow_run` 的实例安装匹配构建，领取后先预检 |
| scheduler | 只检查定义并入队；代码运行交给实际 worker，本地 HTTP 请求不自动获得执行能力 |

总并发 C≥2 时，预览与部署编译合计最多使用 C−1 个槽位，正式等待者按 FIFO 优先，等待上限 500 ms。C=1 时含代码正式运行从运行开始到结束登记占用优先权，已有交互必须在首个业务 API 前于 500 ms 内清空；登记期间拒绝新交互。容量繁忙会明确失败，不自动重试或续跑。

预览另外限制每个管理端进程工作区同时一个请求、最短间隔 200 ms。当前平台实例固定工作区，限制不是跨实例全局锁。部署前按所有实例的进程并发数核算宿主内存，不能把单进程的 128 MiB×C 当成集群总上限。

## 首次开放与版本升级

1. 暂停新触发，排空不兼容任务。保留既有 v1 读取与执行能力。
2. 两个开关保持关闭，执行数据库迁移 007，发布所有 API/worker/scheduler 和嵌入前端；退出旧实例，避免旧客户端写回时丢失 v2 字段。
3. 在各执行实例配置固定路径，按相同服务账号验证启动探测及受限代码计算。确认构建标识一致；scheduler 的本地不可用不代表后台失败。
4. 按下一节重排过期规则计划，再开放 v2/代码及调度。分别验收同步、异步、手动入队和定时执行。
5. 后续改变 goja/profile/协议时，先排空对应构建任务或保留匹配的执行构建；不静默用新引擎解释旧快照，不删除新字段回退。

源码审计仅记录节点、profile、源码字节数/哈希及声明字段，不写完整源码、输入值、样本或运行中变量。

## 开关关闭和恢复后的定时规则

关闭时，scheduler 遇到被关闭能力阻挡的到期项，只把 `nextRunAt` 推到本次检查时间之后；不生成 operation/job、不改变部署状态或业务版本，也不让这些项堵住 due 查询 LIMIT 内的旧流程。调度暂停期间则不会产生推进。

恢复时不能依赖“重新打开开关”补发历史计划。维护操作：

1. 停止全部 scheduler（含 all 角色的调度）和新触发入口，保持相关开关关闭。
2. 在维护开始时选择一个固定 UTC RFC3339 时间 `resumeAt`，保留到重排成功。不得每批或失败重试时重新取当前时间。
3. 在对应工作区执行以下命令。只恢复代码时用 `--scope code`；恢复全部 v2 时用 `--scope v2`。多工作区逐一执行。

```bash
set -a
source /path/to/service.env
set +a
/opt/connara/bin/apihub-workflow-reschedule \
  --scope code --resume-at '2026-10-03T11:00:00Z'
```

上面的时间仅是示例，实际维护用本次选择的固定时间。命令按 ID keyset 每批 100 个、事务行锁核验当前状态。只更新对应范围仍 deployed、仍为 cron/interval 且 `nextRunAt <= resumeAt` 的项到 resumeAt 之后；已经在未来、暂停、草稿、手动流程及已接受快照不变。

4. 若命令部分失败，保持 scheduler 和入口停止，修复后用同一 cutoff 重试；已处理的未来项不会重复推进。核对所有工作区完成后再开放能力和 scheduler。

该维护不补发关闭期间的旧时间点，不创建 operation/job，不取消在途任务，也不增加“等待恢复”的工作流状态。

## 本地验证

基础检查：

```bash
./scripts/check.sh
./scripts/build.sh
```

真实 PostgreSQL/Redis 与 worker 检查需要当前账号满足上述隔离条件、数据库测试工具和 redis-server：

```bash
APIHUB_TEST_CODE_WORKER="$PWD/bin/workflow-code-worker" \
APIHUB_TEST_CODE_LAUNCHER="$PWD/scripts/workflow-code-launcher.sh" \
./scripts/test-integration.sh
```

可用 `APIHUB_TEST_PG_BIN` 指定 initdb/pg_ctl 所在目录。集成脚本创建随机端口的独立 PostgreSQL/Redis，完成后仅清理自己的实例。未指定 worker/launcher 的普通 Go 测试会跳过真实沙箱用例，不能据此声称隔离验收通过。浏览器必须连接由最新嵌入资产构建的独立服务或已授权更新的准确服务。
