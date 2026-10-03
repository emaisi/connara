# 工作流编排（阶段 1+2）实现方案

> 状态：**已实施（2026-10）**。本文档定义实施契约；实现已按「实施顺序」完成：`internal/workflow` 纯引擎、004 迁移与存储层、后台 worker 与 interval/cron 调度、管理端与运行时平面 HTTP API、前端工作流页面与文档均已落地。§12 的「工期紧张时可裁剪项」全部实现（all/any 组合、`{{?}}` 可选引用、加密快照/加密结果列均未裁剪）。
> 未实现项见 §13（并行执行、断点续跑、脚本引擎、Webhook 触发、可视化画布、运行中取消）。
> 验收状态：核心功能已实施；2026-10-02 补齐编辑回填、持续轮询、组合条件编辑、样本预览、工作区入队限额、严格版本校验和 unknown 收尾/清理。数据库集成、浏览器及生产验收尚未完成；本机缺少 redis-server，集成脚本无法启动。
> 范围：多接口组合编排 —— 步骤链 + 任意 DAG（dependsOn）+ 受限条件分支（runIf）+ 输入模板映射。
> 已确认决策：运行时触发同时支持**同步**（默认，直接返回组合结果）与 `?async=1` 异步；第一版**包含** cron/间隔定时调度。
> 修订 v3：补齐真实 action 输入/输出契约、异步身份与结果、数据库约束、可重复执行语义、cron 和集成验收，详见文末修订记录。
> 修订 v4：对齐 HTTP 服务器超时（同步整体预算 120s→40s）、恢复每步脱敏预览、并发限流落地机制及若干小项，详见文末修订记录。

---

## 1. 背景与现状结论

对现有代码的分析结论（2026-10 排查）：

| 能力 | 现状 | 依据 |
|---|---|---|
| 多接口组合编排 | ❌ 完全不支持 | 全库无 DAG/pipeline/workflow 模型；`executeActionCore` 严格一次执行 = 一个 action = 一次 HTTP 调用 |
| 步骤间数据传递 | ❌ 无 | 无上下文/模板机制 |
| 条件分支 | ❌ 无 | 唯一的路径提取函数 `lookup()`（`internal/background/sync.go:43`）仅用于同步分页 |
| 可复用的基建 | ✅ 具备 | jobs 队列（重试/租约/backoff）、OperationRun/OperationEvent 运行可观测、sync-task 定时调度机制、policy 权限、幂等记录、审计 |

本方案不引入任何外部编排引擎，全部构建在现有设施之上。

## 2. 设计原则

1. **安全边界不变**：步骤只能调用已注册且可执行的 action（host 固定于集成配置、凭据服务端注入、路径校验全部沿用），编排层不新增任何出站自由度。部署时必须将每步绑定到明确的集成和连接；编辑页可以预选当前可用项，但运行时不依赖“默认连接”猜测。
2. **条件是受限断言，不是脚本**：`runIf` 的叶子条件仅有 8 个操作符（eq/ne/gt/gte/lt/lte/exists/not_exists）；可用受限的 `all`/`any` 组合，保存时静态校验。不引入解释器。
3. **一条运行 = 一条 OperationRun**：kind 为 `workflow`；每次尝试的每步各记状态事件，包含跳过和失败，事件标明 attempt。
4. **一条执行引擎，两条执行路径**：HTTP 同步与后台 job 复用同一个 `workflow.Runner`（依赖倒置接口，便于单测）。
5. **串行执行**：按拓扑序逐个执行步骤，v1 不做分支并行（留待后续）。
6. **不默认重放写操作**：v1 工作流 job 固定 `maxAttempts=1`。后续只有所有可能重放的步骤都具备上游幂等保证，才能显式启用整图重试；超时等无法判断上游是否成功的结果记 `unknown`，不自动重试。
7. **一次运行使用固定定义**：触发时固定图、触发输入、步骤绑定及 action 版本；后续编辑不会改变已排队或正在运行的内容。凭据有效性和运行时授权仍在每步调用前实时校验。

## 3. 图模型（Graph JSON）

```jsonc
{
  "steps": [
    {
      "id": "get_customer",              // 别名，[A-Za-z0-9_-]{1,64}，全图唯一，禁用 trigger/status
      "title": "查询客户",                // 可选展示名（UI 与运行事件中显示）
      "action": "crm.get_customer",       // 已注册的 actionKey
      "integrationId": "crm-prod",         // 集成 ID 或 key；部署前必须明确绑定
      "connectionKey": "crm-service",      // 连接 ID 或 key；部署前必须明确绑定
      "input": { "id": "{{trigger.customerId}}" },   // 模板 JSON 对象
      "dependsOn": [],                    // 依赖的步骤别名列表，构成 DAG
      "runIf": null,                      // 受限条件，见下
      "onError": "fail"                   // fail(默认) / continue
    },
    {
      "id": "create_order",
      "title": "创建订单",
      "action": "erp.create_order",
      "integrationId": "erp-prod",
      "connectionKey": "erp-service",
      "input": { "name": "{{get_customer.data.name}}" },
      "dependsOn": ["get_customer"],
      "runIf": null,
      "onError": "fail"
    }
  ],
  "output": {                             // 部署前必填：工作流最终输出映射
    "orderId": "{{create_order.data.orderId}}",
    "customerName": "{{get_customer.data.name}}"
  }
}
```

这条示例假定触发输入为 `{"customerId":"C-1"}`，CRM 响应为 `{"data":{"name":"Alice"}}`，ERP 接到的输入即 `{"name":"Alice"}`；若 ERP 响应为 `{"data":{"orderId":"O-1"}}`，最终返回 `{"orderId":"O-1","customerName":"Alice"}`。

**输入与响应契约**：

- 触发输入 `trigger`、默认触发输入 `workflows.input`、每步 `input` 都必须是 JSON 对象。现有 `executor.Action` 的输入类型是 `map[string]any`，数组不能直接作为步骤顶层输入；对象内部可以包含数组。省略触发 `input` 时使用 `workflows.input`，显式传入 `{}` 则覆盖默认值。体积上限：`graph` ≤256 KiB、`workflows.input` 与每步 `input` ≤64 KiB —— 加密快照按运行逐条存储，控制定义体积即控制每次运行开销。
- 成功步骤的别名直接指向**上游 HTTP 响应体**解析出的 JSON 值，不是 Connara 的 `{success,data,meta}` 信封，也不会自动增加 `data`。上例仅在 CRM 实际返回 `{"data":{"name":"..."}}` 时成立；若返回 `{"name":"..."}`，路径应写 `{{get_customer.name}}`。现有 action 的 `outputSchema` 不校验真实响应，字段存在与否最终仍需在运行时确认。
- 响应不是 JSON 时可作为整个字符串结果读取，但不允许取其子路径；空响应为 `null`。若后续步骤或输出映射需要解析字段，应给出 `workflow_non_json_output`/缺失路径错误，指明步骤和路径。

**模板语法**：`{{路径}}`，路径 = 点分段 + 数组下标，如 `{{s1.data.items[0].id}}`；特殊字段名用 JSON 字符串括号写法，如 `{{s1.data["customer.id"]}}`。上下文根键为 `trigger`、已成功步骤的别名，以及只读状态表 `status.<步骤别名>`（`success`/`failed`/`skipped`/`unknown`）。
- 整串恰好是一个占位符时保留 JSON 类型（特别保留 `json.Number`）；混排文本只允许插入字符串、数字、布尔值，不能把对象或数组隐式格式化为字符串。
- 路径不存在时报错并指明路径；`null` 与路径不存在不同。只在工作流级 `output` 中允许可选整值引用 `{{?step.path}}`，缺失时返回 `null`，用于条件分支输出；步骤输入仍必须显式处理缺失字段。

**引用闭包规则**：任一步骤的 input 模板与 runIf 路径引用的别名（含 `status.<别名>`），必须在该步骤 `dependsOn` 的传递闭包内；`trigger` 可随时引用。保存时拒绝未声明的引用，确保被引用步骤已经运行或已明确跳过。工作流级 output 在所有步骤结束后求值，可引用任意别名、状态与 trigger。拓扑排序中同层步骤按图中声明顺序执行，保证事件顺序稳定。

**runIf 语义**：

| op | 含义 | value |
|---|---|---|
| eq / ne | 深相等 / 不等（json.Number 归一化后比较） | 任意 JSON |
| gt / gte / lt / lte | 数值比较（仅数字，否则运行时报错） | 数字 |
| exists / not_exists | 路径存在 / 不存在 | 无 |

`exists` 对存在但值为 `null` 的路径返回 true；`not_exists` 对缺失路径返回 true。其他操作符遇到缺失路径时报错。`runIf` 可为单个断言，也可为 `{"all":[...]}` 或 `{"any":[...]}`；分组不能为空，最多 8 个叶子条件、嵌套最多 2 层，按顺序短路求值。若前置步骤可能被跳过，可先判断状态，再读取响应，例如 `{"all":[{"path":"status.get_customer","op":"eq","value":"success"},{"path":"get_customer.data.vip","op":"eq","value":true}]}`。

**onError**：`fail`（默认，整图失败）/ `continue`（记失败 outcome 和警告事件后继续）。失败或跳过的步骤**没有输出别名**；状态仅通过 `status.<别名>` 与 `StepOutcome` 查看，不把错误对象伪装成上游响应。下游若引用该步骤输出，需要用 runIf 跳过或让运行明确报缺失路径。含被容忍失败的运行以 `success` 结束，但结果与事件必须带 `hasWarnings=true`、失败步骤列表。

**输出映射（output，工作流级）**：如 `"output":{"orderId":"{{create_order.data.orderId}}"}`。草稿可以暂缺，部署前必须配置非空 JSON 对象，避免默认暴露所有中间响应。需要完整响应时可明确映射 `"customer":"{{get_customer}}"` 等字段。条件分支可能跳过的字段使用 `{{?step.path}}`。同步/异步调用方只收到映射后的最终结果；运行事件不存原始步骤响应。

## 4. 数据库（迁移 v3 → v4）

新文件 `internal/store/migrations/004_workflows.sql`：

```sql
BEGIN;

CREATE TABLE workflows (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL,
    workflow_key varchar(120) NOT NULL,
    name varchar(180) NOT NULL,
    description varchar(1000) NOT NULL DEFAULT '',
    status varchar(24) NOT NULL,                    -- draft / deployed / paused / disabled
    graph jsonb NOT NULL DEFAULT '{"steps":[]}'::jsonb,
    schedule_type varchar(24) NOT NULL,             -- manual / interval / cron
    cron_expression varchar(120),
    schedule_timezone varchar(80) NOT NULL DEFAULT 'UTC',
    next_run_at timestamptz,
    retry_policy jsonb NOT NULL DEFAULT '{"maxAttempts":1}'::jsonb,
    input jsonb NOT NULL DEFAULT '{}'::jsonb,       -- 默认触发输入：定时运行与空 body 触发的取值来源
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE UNIQUE INDEX workflows_workspace_key_uq
    ON workflows (workspace_id, workflow_key) WHERE deleted_at IS NULL;
CREATE INDEX workflows_due_idx
    ON workflows (next_run_at, id) WHERE status = 'deployed' AND deleted_at IS NULL;

ALTER TABLE operation_runs ADD COLUMN workflow_id uuid;
ALTER TABLE operation_runs ADD COLUMN workflow_version bigint;
ALTER TABLE operation_runs ADD COLUMN scheduled_for timestamptz;
ALTER TABLE operation_runs ADD COLUMN workflow_payload_ciphertext bytea;
ALTER TABLE operation_runs ADD COLUMN workflow_result_ciphertext bytea;
CREATE INDEX operation_runs_workflow_idx
    ON operation_runs (workspace_id, workflow_id, started_at DESC)
    WHERE workflow_id IS NOT NULL;
CREATE UNIQUE INDEX operation_runs_workflow_schedule_uq
    ON operation_runs (workspace_id, workflow_id, scheduled_for)
    WHERE source = 'schedule' AND workflow_id IS NOT NULL;

-- v3 的 NOT VALID 约束仍约束新行；必须在插入新 kind 前扩展。
ALTER TABLE jobs DROP CONSTRAINT jobs_kind_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_kind_check
    CHECK (kind IN ('sync_run', 'webhook_delivery', 'workflow_run')) NOT VALID;
ALTER TABLE operation_runs DROP CONSTRAINT operation_runs_kind_check;
ALTER TABLE operation_runs ADD CONSTRAINT operation_runs_kind_check
    CHECK (kind IN ('action', 'sync', 'webhook', 'auth', 'workflow')) NOT VALID;

ALTER TABLE workflows ADD CONSTRAINT workflows_workspace_fk
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) NOT VALID;
ALTER TABLE operation_runs ADD CONSTRAINT operation_runs_workflow_fk
    FOREIGN KEY (workflow_id) REFERENCES workflows(id) NOT VALID;
ALTER TABLE workflows ADD CONSTRAINT workflows_status_check
    CHECK (status IN ('draft', 'deployed', 'paused', 'disabled')) NOT VALID;
ALTER TABLE workflows ADD CONSTRAINT workflows_schedule_check
    CHECK (schedule_type IN ('manual', 'interval', 'cron')) NOT VALID;
ALTER TABLE workflows ADD CONSTRAINT workflows_input_object_check
    CHECK (jsonb_typeof(input) = 'object') NOT VALID;

COMMENT ON TABLE workflows IS '由现有 API 操作构成的工作流定义';
COMMENT ON COLUMN workflows.id IS '工作流 UUID';
COMMENT ON COLUMN workflows.workspace_id IS '所属工作区 UUID';
COMMENT ON COLUMN workflows.workflow_key IS '稳定工作流标识';
COMMENT ON COLUMN workflows.name IS '工作流显示名称';
COMMENT ON COLUMN workflows.description IS '工作流用途说明';
COMMENT ON COLUMN workflows.status IS 'draft 草稿、deployed 可运行、paused 暂停、disabled 禁用';
COMMENT ON COLUMN workflows.graph IS '步骤、依赖、条件和最终输出映射';
COMMENT ON COLUMN workflows.schedule_type IS 'manual 手动、interval 固定间隔、cron 定时表达式';
COMMENT ON COLUMN workflows.cron_expression IS '固定间隔标记或五字段 cron 表达式';
COMMENT ON COLUMN workflows.schedule_timezone IS 'cron 使用的 IANA 时区';
COMMENT ON COLUMN workflows.next_run_at IS '下一次计划触发的 UTC 时间';
COMMENT ON COLUMN workflows.retry_policy IS '重试策略；默认最多执行一次';
COMMENT ON COLUMN workflows.input IS '定时运行及省略触发输入时的默认 JSON 对象；不得保存凭据';
COMMENT ON COLUMN workflows.version IS '每次定义或状态更新递增的版本号';
COMMENT ON COLUMN workflows.created_at IS '创建时间';
COMMENT ON COLUMN workflows.updated_at IS '最后更新时间';
COMMENT ON COLUMN workflows.deleted_at IS '软删除时间';
COMMENT ON COLUMN operation_runs.workflow_id IS '关联工作流 UUID';
COMMENT ON COLUMN operation_runs.workflow_version IS '触发时固定的工作流版本';
COMMENT ON COLUMN operation_runs.scheduled_for IS '计划触发的 UTC 时间；非定时运行为空';
COMMENT ON COLUMN operation_runs.workflow_payload_ciphertext IS '加密的运行定义快照及真实触发输入';
COMMENT ON COLUMN operation_runs.workflow_result_ciphertext IS '加密的最终输出，仅授权运行时查询解密';

INSERT INTO schema_migrations(version) VALUES(4);
COMMIT;
```

配套改动：
- `internal/store/store.go`：`//go:embed migrations/004_workflows.sql`、追加到 `Migrate` 切片第 4 项、`CheckSchema` 期望版本 3 → 4。
- `internal/model/model.go`：新增 `Workflow`（`Graph`/`Input` 为 `json.RawMessage`）；`OperationRun` 增加可公开的 `WorkflowID`/`WorkflowVersion`/`ScheduledFor`，密文列不序列化到 API。
- `internal/store/runtime.go`：`CreateOperation`、`ListOperations`、`Operation`、`scanOperation`、`CompleteOperation`/`FinishRuntime` 都要处理工作流字段及结果；`internal/store/pagination.go` 的 `ListOptions` 增加 `WorkflowID`。不要只加列表过滤，否则运行详情读不到关联信息。
- 入队时用现有 `secret.Codec` 加密 `{graph, workflowVersion, actionId/version, integrationId, connectionId, trigger}` 快照，AAD 含 workspace ID 与 operation ID；job payload 只存运行 ID，不存原始输入、凭据或原始输出。`operation_runs.input/output` 只存脱敏、限长预览；完整最终输出加密存 `workflow_result_ciphertext`，随 OperationRun 的保留期清理。使用幂等键的运行，其保留期至少覆盖幂等键 24 小时有效期。运行时查询须验证同一 token ID 和 token 仍有效。

## 5. 核心新包 `internal/workflow`

### 5.1 文件与职责

| 文件 | 职责 |
|---|---|
| `definition.go` | `Definition{Steps []Step; Output map[string]any}` / `Step` / `Condition`；运行快照包含图、触发输入、绑定的 action ID/版本及集成/连接 ID；`MaxSteps = 20`；`ParseDefinition([]byte)` 使用 `jsonutil.Unmarshal` |
| `validate.go` | 别名合法/唯一/≠`trigger`/`status`；步骤 input 与工作流 output 顶层为对象；dependsOn 引用存在、无自依赖；Kahn 拓扑排序验环并稳定排序；input/runIf 引用闭包及 output 引用检查；runIf 叶子操作符、all/any 深度与数量、onError 白名单；严格扫描 `{{路径}}`/`{{?路径}}` 及带引号字段的语法 |
| `template.go` | `Resolve(ctx map[string]any, value any) (any, error)`：递归遍历 map/slice；整串单占位符保类型替换，混排插值 |
| `condition.go` | `Evaluate(ctx map[string]any, cond Condition) (bool, error)`；all/any 短路、缺失路径与 null 语义 |
| `engine.go` | `Runner` 执行器（见 5.2） |
| `*_test.go` | 全部纯逻辑单测（本包是测试重点） |

### 5.2 执行引擎（依赖倒置，两条路径共用）

```go
// 示意接口：运行器不直接读取可变的 workflows 行。
type Principal struct {
    Kind    string // runtime_token / admin / schedule
    TokenID string // 仅 runtime_token 必填
}
type Dependencies interface {
    Action(ctx context.Context, id string) (model.ActionDefinition, error)
    Integration(ctx context.Context, id string) (model.Integration, error)
    Connection(ctx context.Context, id string) (model.Connection, error)
    ResolveAuth(ctx context.Context, connection model.Connection) (executor.RequestAuth, error)
    AuthorizeStep(ctx context.Context, principal Principal, action model.ActionDefinition, connection model.Connection) error
}
type Runner struct {
    Deps     Dependencies
    Executor *executor.Executor
    Catalog  *catalog.Catalog
}
type StepOutcome struct {
    StepID, ActionKey string
    Status            string // success / failed / skipped / unknown
    ProviderStatus    int
    Output            any    // 内存中供模板解析；事件仅持久化脱敏+截断预览（见 §10）
    Error             string // 对外/日志使用安全摘要
}
type RunResult struct {
    Outputs     map[string]any // 仅成功步骤的响应体
    Final       any            // 必需的 output 映射结果
    Outcomes    []StepOutcome
    HasWarnings bool
}
type Options struct {
    Principal Principal
    Attempt   int
    OnStep    func(StepOutcome) // 每次尝试的每步状态；持久化失败不得触发自动重放
}
func (r *Runner) Run(ctx context.Context, snapshot Snapshot, options Options) (RunResult, error)
```

**单步流程**（严格镜像 `executeActionCore` 的解析顺序）：

1. `runIf` 评估 → 不满足 → 记 `skipped` outcome 和 `status.<id>`，不写输出别名，继续下一步。
2. 用快照中的 action ID 加载定义，确认 `active` + `executable` 且版本等于快照版本；版本变化时停止并要求重新触发，不能悄悄改用新定义。
3. `template.Resolve` 解析对象型 `step.Input`（上下文 = `{trigger, status, 各成功别名}`）。
4. `Catalog.ValidateInput(definitionAction(action), resolvedInput)`。
5. 按快照固定的集成 ID、连接 ID 加载，实时确认集成 ready、连接 active、所属系统/集成一致。编辑阶段可以按 action 默认集成预选；**部署时必须明确绑定**，不能运行时取“最早创建的可用连接”。
6. `AuthorizeStep`：`runtime_token` 每步按 token ID 重新读取 active/未过期/未撤销的令牌，再做 `policy.AllowsAction` 与 `policy.AllowsConnection`；`admin`/`schedule` 是明示的控制面执行身份，记录触发者，不以空 token 隐式放行。
7. `Deps.ResolveAuth(connection)` → 凭据。
8. 单步 ctx 超时 40s（与 action 路径一致）→ `Executor.Action(ctx, provider, actionModel, input, nil, RequestAuth{…})`；累计上游响应体最多 8 MiB，超过即停止，避免 20 × 4 MiB 全部驻留内存。
9. 已收到的非 2xx：`onError=continue` 时记 `failed` 和警告、继续；默认整图失败。传输错误或超时无法判断上游是否已执行，应记 `unknown` 并停止，即使该步骤配置 continue 也不吞掉。
10. 成功 → 用 `jsonutil.Unmarshal` 解析上游响应体；写入成功别名和 `status.<id>`，触发 `OnStep`。任何步骤失败也必须记录该步骤的事件；事件含 attempt、stepId、actionKey、状态、HTTP 状态、安全摘要，以及**脱敏+截断的响应预览**（共享脱敏函数，≤4 KiB，仅管理端运行详情可见，运行时调用方任何接口不暴露）——与现有 action/sync 运行存脱敏输出的做法对齐，否则异步失败时管理员无从排查上游实际返回了什么。

**导入方向**：纯运行器 `workflow → {executor, catalog, model, jsonutil}`；`httpapi`/`background` → `workflow`，接线层适配 store、authn 与 policy。`httpapi.definitionAction`/`background.actionModel` 的等价转换应收敛为可复用函数，避免新路径行为漂移。

### 5.3 jsonutil 增强

`internal/jsonutil` 新增 `PathLookup(value any, path string) (any, bool)`：是 `background/sync.go` 中 `lookup()` 的泛化，增加 `[n]` 数组下标和 `["带点号的字段"]`。`sync.go` 改为调用它，原有 `$`/`.` 前缀及点路径行为必须回归测试，不能因新解析器改变同步分页取值。

## 6. 存储层 `internal/store/workflows.go`

复用现有 jobs/OperationRun 事务模式，但不能直接复制 sync-task 的“同一资源只允许一个活跃 job”和“运行时重读最新配置”规则：

| 方法 | 说明 |
|---|---|
| `ListWorkflows(ctx)` | 全量列表（同 `ListSyncTasks` 无分页） |
| `Workflow(ctx, idOrKey)` | 按 id 或 key 取，过滤 `deleted_at IS NULL` |
| `SaveWorkflow(ctx, item, expectedVersion)` | 新建生成新 UUID、status=draft、timezone=UTC；同一 key 的软删记录不被隐式复活。更新按 ID + workspace + version 做乐观锁；修改已部署定义会转回 draft 并清空 next_run_at，须重新部署。请求不能直接写 status |
| `DeleteWorkflow(ctx, id)` | 软删并停止新触发；同 key 可新建新 UUID，历史运行仍指向旧 ID |
| `SetWorkflowStatus(ctx, id, status, nextRunAt, expectedVersion)` | 只处理 deploy/pause 的合法迁移；version+1；部署时把选择的集成/连接 key 规范化为明确 ID 并持久化到已部署图中 |
| `ValidateWorkflowReady(ctx, wf)` | 逐步检查 action active+executable、action/集成/连接所属关系、集成 ready、连接 active、输入图可解析及调度配置；触发时再校验一次 |
| `EnqueueWorkflow(ctx, wf, trigger, principal, source, requestID)` | 事务内锁定工作流版本、生成不可变加密快照，插入 `OperationRun{Kind:"workflow", Status:"queued", WorkflowID, WorkflowVersion, RuntimeTokenID, Input:脱敏预览}` 和 `jobs{kind:"workflow_run",resource_id:operationID,operation_id:operationID,payload:{"operationId":…}}`；不同输入的手动/运行时请求允许分别排队，幂等键负责同一请求防重；入队事务内计数本工作区 `workflow_run` 的 queued+running，超过上限（默认 50，可配）返回 `ErrBusy` → HTTP 429 `workflow_busy`（SQL 计数、fail-closed，不依赖 Redis） |
| `EnqueueDueWorkflows(ctx, limit)` | `FOR UPDATE SKIP LOCKED` 选到期 deployed 行；用 `scheduled_for` 记录原计划时间并只对**同一工作流的定时运行**去重。若上一条定时运行未结束，跳过本次并推进 next_run_at、记录结构化 skipped 日志/指标；其他手动/运行时运行不受全局去重影响 |

`resource_id=operationID` 使现有按 resource_id 的 worker 租约隔离每次独立触发；工作流 ID 从 OperationRun 读取。v1 `retry_policy.maxAttempts` 只允许 1，数据库列为后续有上游幂等保障的步骤保留。完成异步运行需新增带 `fenceJob` 校验的事务方法，一次提交 job 状态、OperationRun 状态、脱敏预览和加密最终输出；不能先无条件 `CompleteOperation` 再 `CompleteJob`。已调用上游但无法确认结果的故障设 `unknown`，扩展 `CleanupExpired` 对 workflow job 的兜底状态，绝不自动重放。同步运行用同一结果持久化契约，并在提交成功后发送 HTTP 响应。

## 7. 后台服务

`internal/background/service.go`：

- `RunWorkers(ctx, syncWorkers, workflowWorkers, webhookWorkers int)` —— 签名扩展，新增 `start("workflow_run", workflowWorkers)`。
- `work()` 的 switch 增加 `case "workflow_run": runErr = s.runWorkflow(jobCtx, job)`。
- `runWorkflow`：由 job.operation_id 读取并解密固定快照和 trigger，读取 Principal（runtime token 需重新确认有效）→ `MarkOperationRunning`（须检查影响行数）→ `Runner.Run` → 对每步写带 attempt 的事件 → 用带租约栅栏的事务提交结果。为异步路径新增 `RuntimeTokenByID`，与现有按 hash 读取相同地限制 workspace、active、未过期、未撤销。pause/delete 只阻止新触发；已接受的运行使用快照继续，token 撤销及连接停用仍会阻止后续步骤。事件存元数据、安全错误摘要和**脱敏+截断（≤4 KiB）的每步响应预览**（共享脱敏函数，仅管理端可见）；不存未脱敏的原始响应。
- `RunScheduler` 增加 `s.store.EnqueueDueWorkflows(ctx, 20)`。固定间隔复用已支持的四种值；**标准五字段 cron 需要新增解析器**，不能调用现有 `nextTaskRun`（只认固定间隔）或复制 `EnqueueDueSyncTasks` 的 CASE SQL。保存时校验表达式和 IANA 时区；每次从上次计划时间计算下次时间，落库为 UTC。宕机错过的多次触发不补跑，只计算下一次；夏令时不存在的本地时刻跳过，重复的本地时刻只触发一次；用测试覆盖这两种情况；二进制 `import _ "time/tzdata"` 兜底，避免裸机部署缺系统 tzdata 时 `time.LoadLocation` 失败。
- `internal/config/config.go`：`WorkflowWorkers`（env `APIHUB_WORKFLOW_WORKERS`，默认 2）+ config_test 用例；`cmd/apihub/main.go` 传参；`.env.example` 补一行。

## 8. HTTP API 契约

### 8.1 管理端（`internal/httpapi/workflows.go`，handler 形态照抄 sync-task）

```
GET    /api/workflows                 列表（不套 audited）
GET    /api/workflows/{id}            详情
GET    /api/workflow-runs/{id}/result  解密最终输出；仅 owner/admin/developer，可审计读取
POST   /api/workflows                 新建（audited）
PATCH  /api/workflows/{id}            更新（audited）
DELETE /api/workflows/{id}            软删（audited）
POST   /api/workflows/{id}/deploy     部署：计算 next_run_at（interval/cron），状态 → deployed
POST   /api/workflows/{id}/pause      暂停（audited）
POST   /api/workflows/{id}/run        立即运行：body 可选 {"input":{…}}，须 deployed，
                                      EnqueueWorkflow(source="manual") → 202 + OperationRun
```

- saveWorkflow：结构、路径、引用闭包、条件组、默认触发 input 为对象及体积上限（`graph` ≤256 KiB、`input` ≤64 KiB）；只要求引用的 action 存在。新建只进入 draft，更新按 `version` 乐观锁；编辑已部署版本自动转 draft。部署时另行检查所有 action active+executable、集成/连接、非空输出映射和调度，规范化并固定图中的集成/连接 ID；不能直接通过 save 请求把 status 设为 deployed。`audit("workflow.saved", …)` 的 before/after 也要经共享脱敏函数处理，不能把默认输入原样写入审计。
- 管理端结果读取只返回工作流最终 output，不返回步骤原始响应；需在 handler 内再次校验 owner/admin/developer（现有 `allowsAdmin` 对所有 GET 默认放行），并记录读取审计。管理端 202 后可轮询该接口展示最终结果。
- manual/interval/cron 分别校验；interval 限现有四个固定值，cron 用新增五字段解析器和 IANA 时区，`nextTaskRun` 不能计算 cron。`retryPolicy.maxAttempts` v1 仅接受 1。
- `api.go` 注册路由（写操作套 `audited`）；`middleware.go` 的 `allowsAdmin` developer 可写前缀表追加 `"/api/workflows"`。

### 8.2 运行时平面（requireRuntime）

**发现接口（v2，与 `/v1/actions` 对齐，否则调用方只能靠带外渠道得知 key）**：
```
GET /v1/workflows           列出已部署、且 token 允许全部步骤 action 与绑定连接的工作流（key/名称/描述）
GET /v1/workflows/{key}     返回可调用信息及 input 为 JSON 对象的约定；不暴露连接绑定、凭据或原始默认输入
GET /v1/workflow-runs/{id}  仅发起该运行且仍有效的 token 可查询状态、错误摘要和已完成的最终输出
```

列表计算 policy 交集时须批量加载涉及的连接与 action，避免逐工作流 N+1 查询。

**触发 `POST /v1/workflows/{key}`**

前置校验：按 key 加载、须 `deployed`；body 可省略，省略 `input` 时使用 `workflows.input`，显式 `{"input":{}}` 覆盖默认值；必须校验为对象。token 须允许图中**全部** actionKey 和部署时绑定的连接，否则 403 `policy_denied`；每步调用前再次按当前 token 状态校验。

**同步（默认）** —— 复用现有幂等记录，但补齐工作流的原子持久化：

- `Idempotency-Key` 可选：scope = `workflow:{id}`，指纹 = sha256(`{workflowId, version, effectiveTrigger, mode}`)，24h。不同指纹返回 409；同指纹运行中返回 409 `idempotency_in_progress` 并在 meta 给出 operationId；已完成的同步请求从加密最终结果重建原响应，已接受的异步请求重放同一个 202/operationId。重新部署后 `version` 变化会改变指纹，同一 Idempotency-Key 将返回 409（预期行为，调用方应换 key）。`idempotency_records.response` 仅存不含原始结果的引用信封，不能照搬现有 `FinishRuntime` 把明文响应写入 JSONB。幂等占位、OperationRun 与 job 的事务顺序要保证崩溃后可查询，不得出现已调用上游却被当成未开始而再次执行。
- 建 `OperationRun{Kind:"workflow", Source:"runtime", Status:"running", WorkflowID, WorkflowVersion, RuntimeTokenID, Input:脱敏预览}`，加密保存固定快照和有效 trigger。
- `Runner.Run`（整体 ctx 上限 **40s**，单步 40s 共享该预算；token 逐步 policy 校验）。同步预算对齐现有 HTTP 服务器 `ReadTimeout 40s / WriteTimeout 45s`（`cmd/apihub/main.go:80`）——超时后响应无法写出，调用方只会拿到连接错误而非带 operationId 的错误信封；现有 action 路径的 40s 正是为此卡在 45s 之内。步骤多或上游慢的工作流应使用异步；未来放宽同步预算必须同时上调服务器超时与反代配置（运维决策，非代码单方面可改）。
- 成功：`FinishRuntime` + 响应

```json
{ "success": true,
  "data": { "orderId": "123", "customerName": "Alice" },
  "meta": { "operationId": "UUID", "stepCount": 2, "hasWarnings": false } }
```
`data` 始终是部署时配置的 output 映射对象。已容忍失败须在 meta 标明 `hasWarnings` 和失败步骤列表。

- **体积上限**：每步响应继承现有 4 MiB，上游响应累计 ≤ 8 MiB，最终 `data` ≤ 4 MiB；超限记 `workflow_output_too_large`。改用异步不能绕过累计/最终上限。
- 失败：错误信封的 code 区分 `workflow_step_failed`、`workflow_result_unknown`、`workflow_output_too_large`、`policy_denied` 等；message 只含步骤 ID 和安全摘要，meta 带 operationId。运行详情可看步骤状态与事件，不承诺提供未映射的完整中间响应。

**异步（`?async=1`）**：`EnqueueWorkflow` → `202` 信封 `{"success":true,"data":{"operationId":"…","status":"queued"}}`。调用方轮询 `GET /v1/workflow-runs/{id}`；查询成功但工作流失败时 HTTP 查询本身仍返回 200，`data.status` 是 failed/unknown，`data.error` 为安全摘要。成功时 `data.output` 从加密结果解密，仅暴露最终映射结果，不返回每步原始响应。管理端 `GET /api/operations/{id}` 继续展示脱敏预览（含每步脱敏响应预览）与时间线；不能当作运行时调用方的取结果接口。

## 9. 前端（`web/`）

| 文件 | 改动 |
|---|---|
| `src/pages/workflows.tsx` | **新页面** `WorkflowsPage`，骨架照抄 sync-tasks.tsx（列表 + Modal + MutationForm） |
| `src/api.ts` | `workflows()` / `saveWorkflow(body,id,version)` / `deployWorkflow` / `pauseWorkflow` / `runWorkflow(id,input)` / `workflowRunResult(id)` / `deleteWorkflow` |
| `src/query.ts` | related 增加 `"workflows": ["operations","metrics"]` |
| `src/app.tsx` | 懒加载路由 `workflows` |
| `src/shell.tsx` | 「更多功能」分组加「工作流」导航（lucide `Workflow` 图标） |
| `src/pages/demo-operations.tsx` | 支持 `?workflow={id}` 筛选透传到 `/api/operations` |
| `src/demo.tsx` | kind 联合类型与标签映射加 `Workflow` |
| `src/locales/en.json` | 新词条 |
| `src/pages/workflows.test.tsx` | 渲染 + 保存 happy path（mock api） |

**页面结构**：
- 列表（桌面表格 + 移动卡片）：标识/名称、调度标签（复用 `scheduleLabel`）、步骤数、状态 Badge；操作按钮：编辑 / 部署 / 暂停 / 删除（confirm）/ 立即运行 / 运行历史（`/operations?workflow={id}`）。
- 新建/编辑 Modal：工作流标识、名称、说明、调度（manual/interval/五字段 cron + IANA 时区；不能照抄仅支持 interval 的 sync 表单）、**默认触发输入 JSON 对象**、输出映射编辑器。v1 重试固定一次，不提供会暗示安全重放的 maxAttempts 控件。
- **步骤编辑器**（核心 UI）：步骤卡片含别名、说明、action 下拉、**明确的集成和连接选择**、输入 JSON 对象模板、dependsOn、runIf 断言或受限 all/any 组、onError；支持增/删/上移下移。同层步骤以图中顺序执行。部署前展示解析后的绑定；若 action 或连接失效，指出具体步骤。
- **引用提示与预览**：展示 `trigger` 字段、dependsOn 闭包内步骤别名、`status.<别名>`；可粘贴真实响应的脱敏样本预览模板取值及最终 output，不在预览时自动调用可能有副作用的上游 API。前端只负责提示，后端执行完整校验。
- 管理端运行 Modal：触发输入 JSON 对象 → `api.runWorkflow` → 返回 202 operationId，轮询运行状态并在有权限时展示最终 output；同步组合结果只由运行时 POST 接口直接返回。

## 10. 安全与运维要点

- **无新增出站自由度**：步骤只调已注册 action；host/凭据/路径安全全部继承现有执行器。
- **保存与部署校验**：草稿保存时校验图结构、引用闭包、类型和条件；部署时另外验证每步 action、集成、连接及调度配置。此后 action 或连接可能被停用，运行前和每步仍需实时检查。默认输入与模板中不得保存明文凭据；凭据仍从连接注入。
- **运行期权限**：runtime 同步和异步都携带 token ID，逐步重新读取当前令牌并校验 action 与连接（deny-wins）。撤销或过期后剩余步骤停止；管理端与定时触发明确记录控制面执行身份。发现接口不能泄露调用 token 无权使用的工作流。
- **结果与脱敏**：步骤响应只在本次运行内存中做映射与输出收敛。事件存元数据加**脱敏+截断（≤4 KiB）的每步响应预览**——与现有 action/sync 运行存脱敏输出的做法对齐，保证异步失败可排查；不存未脱敏原文或完整错误体，且运行时调用方任何接口都不暴露每步预览。OperationRun.Input/Output、审计 before/after 使用共享的脱敏和大小限制函数。当前 `auditJSON` 位于 httpapi 包，需移到无循环依赖的共享包；按字段名脱敏只能作为预览保护，不能保证识别任意敏感值。异步最终结果加密保存，仅原发起 token 可读；同步最终结果直接返回调用者。
- **重试与版本**：v1 每个运行最多执行一次；已发出请求后的超时、失联、结果持久化失败要标记 unknown，不能由 job 机制自动重跑。工作流编辑不影响已接受运行的加密快照；action 版本变化时停止旧运行，避免混用定义。未来放开整图重试前，需要逐步骤上游幂等键、版本钉扎和失败后恢复契约。
- **限制与并发**：MaxSteps=20、单步 40s、同步整体 40s（对齐服务器 `ReadTimeout 40s/WriteTimeout 45s`，见 §8.2）、异步整体 15 分钟；每步响应 ≤4 MiB、累计响应 ≤8 MiB、最终输出 ≤4 MiB；`graph` ≤256 KiB、默认 `input` ≤64 KiB。单个工作流允许不同请求并行执行，调度来源只去重自己的活跃运行。并发限制落地机制：`EnqueueWorkflow` 入队事务内做工作区级 `workflow_run` 活跃计数（默认上限 50，可配），超限返回可重试的 429 `workflow_busy`（SQL 计数、fail-closed、不依赖 Redis），而不是静默丢弃；可选再叠加 `requireRuntime` 同款 Redis `Cache.Allow` 做突发限流。
- **部署顺序**：004 依赖 003 的现有约束；先确认目标库已应用 003，再应用 004，最后发布 `CheckSchema` 期望版本 4 的新二进制。旧库与新二进制不兼容；生产发布另行准备回滚与数据备份。
- **token 策略**：工作流新增 action 或变更绑定后必须重新部署；旧 token 若不再覆盖所有 action/连接，触发时直接 403。已排队运行按执行时的令牌状态再次校验。
- **调度与暂停**：手动/运行时触发和定时触发各有明确身份。暂停或删除阻止新触发；已接受的运行使用快照继续，界面应说明这一点。需要中止正在运行的任务时须另做取消能力。

## 11. 测试计划

| 层 | 内容 |
|---|---|
| `internal/workflow` | 环/悬空引用/引用闭包、条件 all/any 深度和短路、别名保留字、模板数字与数组、缺失/null/可选输出、混排文本类型；httptest 上游验证 API1 的 JSON 字段保类型进入 API2 对象输入、条件跳过、continue 警告、非 2xx、超时 unknown、输出和累计体积上限、相同 DAG 的确定顺序、事件预览脱敏 |
| `internal/jsonutil` | PathLookup 点路径/下标/缺失（并回归 sync.go 行为） |
| `internal/config` | `APIHUB_WORKFLOW_WORKERS` 解析与默认值 |
| `internal/store` + `internal/httpapi` | 仓库已有 `scripts/test-integration.sh`（临时 PostgreSQL/Redis），必须用它测 003→004 迁移、v3 CHECK 扩展、新旧操作共存、CRUD/version、软删同 key 重建、运行字段扫描与筛选、手动多次入队、只对定时运行去重、租约丢失后不能覆盖结果、幂等重放与冲突、跨 token 查询拒绝、viewer 无权读取管理端明文最终结果、token 撤销后 worker 拒绝执行、加密结果与清理、工作区并发上限触发 429 |
| 调度 | interval 四档与 cron 五字段、IANA 时区、夏令时缺失/重复时刻、停机错过多个周期、同一计划时间的并发 scheduler 去重；定时默认触发输入可完整跑通 API1→API2 |
| `web` | 列表/编辑/部署/运行历史；步骤增删、显式绑定、引用提示、样本预览、cron 校验错误、版本冲突与 202 运行提示；桌面和移动宽度检查 |
| 端到端冒烟 | 建两个指向 mock 上游的自定义 action，分别验证管理端 202、`/v1` 同步映射、`?async=1` 后用同 token 取最终结果、其他 token 无权读取、条件跳过与 continue、实际 cron 触发及暂停 |
| 全量门禁 | `go build ./... && go vet ./... && go test ./...`；`scripts/test-integration.sh`；`npm --prefix web run build`（产物进 `internal/webui/static`）及 `npm --prefix web run check`；最后 `scripts/check.sh` |

## 12. 实施顺序（每步可独立编译验证）

1. `jsonutil.PathLookup`、图定义/校验/模板/条件/快照与纯 Runner，先以 httptest 证明两步传值及失败语义。
2. 004 迁移（含 v3 kind 约束扩展和加密列）、model、store CRUD/运行快照/租约栅栏/查询；用临时 PostgreSQL 集成测试确认。
3. 共享审计脱敏与加密结果接线；后台 worker、固定间隔和真正的 cron 调度、config/main 接线；集成测试权限、版本和失败。
4. 管理端 CRUD/deploy/pause/run；运行时发现、同步/异步触发、同 token 查询结果及幂等契约；覆盖 HTTP 集成测试。
5. 前端步骤编辑、绑定、样本预览、运行历史与响应式验证。
6. 构建嵌入资源、跑全量门禁；更新中英 README 和接口文档，区分已实现与待上线状态。

工作量需按上述新增的加密结果、异步授权、cron 和集成测试重新估算；原 ~1200/~700/~60 行估算已失效。

**工期紧张时的可裁剪项**（不影响正确性，仅降低体验/安全强度；裁剪任何一项需同步更新本文档对应章节）：
- `runIf` 的 `all`/`any` 组合 —— 单断言 + `status.<别名>` 判断已能表达同样的分支逻辑；
- `{{?}}` 可选输出引用 —— 可用 `status` 条件 + 显式缺失处理替代；
- 加密快照/加密结果列 —— 可先与 action 现状一致只存脱敏预览，二期再引入加密列（迁移按“加列”设计，后补不破坏兼容）。

## 13. 明确不做（留待后续）

- 步骤**并行**执行（拓扑分层调度）。
- 步骤级断点续跑和自动整图重试（v1 每次只执行一次；运行定义已钉扎）。
- 脚本引擎（expr/CEL/JMESPath）—— 阶段 3，待真实需求。
- Webhook 入站事件触发工作流（`inboundWebhook` 绑定 workflow key）。
- 可视化画布（React Flow）；v1 用「步骤卡片 + 依赖复选」表达 DAG。
- 运行中取消（jobs 目前无取消机制，与 sync-task 一致）。

---

## 14. 修订记录

- **v2（缺口评审补充）**：加入默认触发输入、引用闭包、输出映射、运行时发现、步骤标题及引用提示；后续 v3 修正了 v2 的异步权限、重试、cron 和测试基建假设。
- **v3（可实施性复核）**：对齐现有 action 的对象输入和原始上游响应；补 status/可选输出及受限条件组合；部署时明确绑定；固定加密运行快照；运行时异步 token 逐步校验并提供同 token 结果查询；扩展 v3 数据库 kind 约束；默认不重试写操作；真正实现五字段 cron；使用仓库已有 PostgreSQL/Redis 集成测试。
- **v4（超时与可观测复核）**：① 同步整体预算 120s→40s，对齐服务器 `ReadTimeout 40s/WriteTimeout 45s`（`cmd/apihub/main.go:80`），放宽需同时调服务器与反代（运维决策）；② 恢复每步脱敏+截断（≤4 KiB）响应预览入事件（仅管理端可见），对齐 action/sync 现状、保证异步失败可排查；③ 并发限流落地为入队事务内工作区级活跃计数（默认 50，429 `workflow_busy`），可选 Redis 突发限流；④ 补 `graph`/`input` 体积上限、重新部署后幂等键 409 说明、`time/tzdata` 兜底、发现接口批量加载防 N+1；⑤ §12 增列工期紧张时的可裁剪项。

- **2026-10-02 实施补齐**：管理端更新必须携带正数 version；工作区容量检查通过事务 advisory lock 串行化，并覆盖定时入队（容量不足保留待触发计划，下一轮重试）；幂等记录与运行创建原子关联。发现接口不再截断 action；前端支持 all/any JSON 编辑与脱敏样本模板预览。结果落库失败和已开始执行的任务租约失效记 unknown，unknown 数据纳入保留期清理。
