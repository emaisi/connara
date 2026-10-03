# JavaScript 代码节点开发流程

> 2026-10-03 交互优化：代码节点编辑体验已同步：分支出口可直接添加代码；命名输入/返回声明折叠，日常映射与源码保留在主编辑路径，未应用缓冲在关闭属性后保留。节点搜索、专注模式及一次撤销同步适用。运行环境不可用与节点开关关闭仍分别提示，不修改正式优先、单并发或调度恢复规则。 实测边界与截图见 [验收记录](workflow-validation-report.md)。

> 2026-10-03 开发记录：受限 worker、共享容量/正式优先/C=1 行为、事务外编译、开关关闭及恢复维护已接入；代码 UI 与运行历史同步交付，证据见 [工作流开发验收记录](workflow-validation-report.md)，部署与调度恢复见 [运行维护说明](workflow-code-runtime.md)。仅将实际完成的本地检查记为通过；真实供应商及现有服务发布不在本次记录中冒充完成。

日期：2026-10-03。状态：首版已实现；解释器、隔离、接口、UI 及本机服务更新结果见开发验收记录，生产环境未发布。

本文落实“只支持 JavaScript，处理简单确定性计算”的决策。[总开发文档](workflow-visual-editor-development-plan.md)负责完整交付，[节点规格](workflow-node-design.md)负责六类节点及共用执行契约，[原编排方案](workflow-orchestration-plan.md)保留为当前 v1 基线。代码节点的专属契约与开发顺序以本文为准，共用来源、流程变量、分支和权限继续遵循前两份文档。

实施审阅补充（2026-10-03）：明确失败分类、事务外编译、进程级共享 CodeRunner、分角色部署和升级、源码审计投影、本地节点身份检查、nullable 与两种样本验证范围；先打通 API＋代码完整执行链，再接变量/其他节点和完整画布。以下是首版实现契约，逐项证据见开发验收记录。

衔接规则补充：明确正式执行优先的容量分配、单并发行为和开关恢复时的计划重排；业务时间从只读运行快照显式映射给代码。条件汇合及结束规则继续遵循节点专项，不增加执行节点。

## 1. 定位与首版范围

节点种类固定为 **开始、API、数据处理、代码、条件判断、结束**。API、数据处理、代码、条件判断合计最多 20 个执行步骤；开始/结束为唯一虚拟节点，不增加公式、关联、赋值、合并等独立节点。

可部署的 v2 流程至少包含一个 API 节点，保持“用于 API 调用组合”的定位；此条件同时用于管理端部署、runtime 发现/触发和定时入队。草稿及单个代码节点的样本执行可以没有 API，不因此新增纯计算工作流的公共调用权限。至少包含 API 不代表每条运行路径都必须实际调用 API；命中不调用 API、直接连接结束的分支仍合法，但最终返回仍等待所有有效步骤完成，不中断其他路径。

| 需求 | 使用方式 |
| --- | --- |
| API1 某字段直接作为 API2 参数 | API2 点选上游字段，无需代码 |
| 选字段、筛选、排序、取前 10 条、计数 | 数据处理节点，可视化配置 |
| API1 数量 × API2 单价 − API3 优惠 | 一个代码节点，分别点选三个输入 |
| 不同返回结构转成统一对象、数组按 ID 关联、分组汇总 | 代码节点在已提供的数据内计算 |
| 根据计算结果选择不同调用路径 | 代码 → 条件（IF / 多 ELSE IF / ELSE）→ API |
| 计算后更新 orderId 或其他公共值 | 代码返回命名字段 → “更新流程变量” → 后续读取 vars |

首版固定 JavaScript，不增加语言下拉框。不支持 Python、TypeScript 编译、npm/pip 包、用户安装依赖、LLM、浏览器 DOM、Node.js API、网络请求、文件/环境变量/命令执行、定时器或异步任务。API 的账号、授权、重试边界仍由 API 节点负责。

允许在代码中遍历输入数组、计算多个字段；这不等于支持循环调用 API、自动翻页或工作流回边。所有计算受大小、时间和内存预算限制，不承诺任意规模或任意库的运算。

借鉴 Dify 的“显式选择输入 → 编写函数 → 声明输出 → 下游选字段”交互；不引入它的完整服务体系。[Dify 官方代码节点说明](https://docs.dify.ai/en/cloud/use-dify/nodes/code)用于交互参考，本项目的语言、运行限制及契约以本文为准。

<a id="contract"></a>
## 2. 节点定义、输入与返回

### 2.1 持久化结构

沿用 `graph.schemaVersion: 2`，增加 `type: "code"`，不另存画布执行逻辑。v2 尚未发布，因此本次不再增加 v3；任何读取器遇到未知步骤类型都必须拒绝，不能降级为 API 或丢弃字段。

```ts
type CodeStep = StepBase & {
  type: "code";
  language: "javascript";
  runtimeProfile: "js-v1";
  input: Record<string, ValueSource>;
  inputSchema: Schema;
  code: string;
  outputSchema: Schema;
  assign?: { variable: string; value: ValueSource }[];
};
```

StepBase、ValueSource 复用[节点共用契约](workflow-node-design.md#shared-contract)。runtimeProfile 表示本项目固定的语法、内置能力及资源规则；由编辑器填入，不给用户选择解释器版本。

| 字段 | 约定 |
| --- | --- |
| id/title/dependsOn/scope | 与其他执行步骤一致，支持分支内及公共汇合位置 |
| input | 输入名到来源的映射；来源可为开始、只读运行信息、流程变量、多个合法上游、固定值或按分支取值 |
| inputSchema | 必填、根为 object；输入名和 properties 一一对应，首版全部输入必填 |
| code | 用户源代码，UTF-8，最多 32 KiB；保存原文，不展开其中的任何模板 |
| outputSchema | 必填、根为 object，至少一个声明字段；这些字段全部必填，返回根对象不允许多出字段 |
| assign | 成功后显式更新变量；当前节点输出可在此引用，执行中不能直接修改流程上下文 |

Schema 复用 type/properties/required/items/description 子集和六种业务类型 string/number/integer/boolean/object/array；未知关键字拒绝。代码输入/输出根字段额外执行“名称集合一致、全部必填”规则；内部对象沿用共用规则，允许未声明字段。字段增加“允许空值”，按[共用空值契约](workflow-node-design.md#nullable-contract)保存为基础类型与 null 的 type 数组，例如 `"type": ["string", "null"]`；不增加任意联合类型编辑器，也不把 nullable 当成新的 JSON Schema 关键字。

根 input/output 必须为非 null 对象；根字段仍全部必填，“允许空值”只允许字段明确存在且值为 null。缺失来源/缺返回字段继续报错；空字符串、0、false、空数组分别按真实类型处理，不用真值判断，也不自动变成 null 或默认值。流程变量沿用已有 type＋nullable 声明，UI/校验与字段 Schema 转换共用小型适配函数。

输入/输出根字段名为 1–64 位 ASCII 字母、数字或下划线，首位字母；不接受 `__proto__`、`prototype`、`constructor` 等保留名称。嵌套 API 业务键不因此被删除，转换时按自有数据属性安全构造，不能把业务键当作对象原型操作。

### 2.2 函数协议与 JSON 边界

只接受包含顶层普通函数声明 `function main(input) { ... }` 的脚本，可另外定义同步辅助函数。执行器编译原文，执行初始化后用函数调用接口调用 main，传入已解析 JSON；不把输入拼进 JavaScript 源码，不拼接用户输入到 shell 命令。

```javascript
function main(input) {
  return {
    totalCents: input.quantity * input.unitPriceCents - input.discountCents
  };
}
```

此模板前提是输入已经按声明验证为适当数字；生产业务仍需按第 7 节示例检查非负、上限等规则。代码字符串中的 `{{api1.id}}` 只是字符串；UI 提示通过输入映射读取上游，不能对源码做模板替换。输入映射是依赖分析的唯一入口，不扫描源码猜测工作流依赖。

返回值为普通 JSON 对象，直接成为该节点的输出。例如节点 calc_total 返回 `{ "totalCents": 1900 }`，下游引用 `{{calc_total.totalCents}}`；不自动包一层 result。数据处理节点的 `{result: ...}` 约定继续保留。

返回检查必须在 worker 的预算内完成：

1. 根必须是普通对象；不能返回数组、标量、null、Promise 或没有返回值。
2. 递归只接受 JSON 数据；拒绝 undefined、函数、Symbol、BigInt、NaN/Infinity、循环引用、稀疏数组及非普通实例。不得由 JSON.stringify 静默丢字段或把非有限数字变成 null。
3. 拒绝访问器属性、Proxy 和用户 toJSON 等带执行行为的返回结构；不将它们导出为主进程可执行对象。严格遍历、异常捕获及最终序列化全部留在子进程，防止检查本身阻塞主进程。
4. 校验深度、体积和 outputSchema，成功后返回纯 JSON 字节；主进程再次检查协议、体积、Schema。不能依赖子进程“说成功”就提交变量。

JSON 数字进入 JS 前检查：表示整数的数值必须在 ±9007199254740991 内；输出中的整数也执行这一限制。ID、超大整数和高精度金额应以字符串传入，不能先转 float64 后再判断是否失真。普通小数遵循 JavaScript Number 精度；金额模板优先使用最小货币单位的安全整数，业务舍入需显式定义，首版没有十进制定点库。

input 是独立 JSON 副本，代码修改其中对象不会改写 trigger、上游输出或 vars。不向 JS 暴露完整工作流上下文、Go map/struct 指针、账号凭据、Token、HTTP 客户端或服务对象。

### 2.3 流程变量与分支

例如 input.currentOrderId 选择 `{{vars.orderId}}`，input.remoteOrder 选择 API2 输出；代码返回 `{ "orderId": "O002" }` 后，配置 `assign: [{"variable":"orderId","value":"{{normalize_order.orderId}}"}]`。后续 API 读取 vars.orderId；没有 assign 时只新增 normalize_order.orderId，不自动覆盖同名变量。

复用变量规则：input/assign 的 vars 读取本节点进入时的版本；所有赋值先计算、校验，再和成功输出一起发布。计算、输出校验或任一赋值失败，全部变量保持原值，下游停止。代码不支持 onError=continue 和自动重试；之前已成功的 API 不重放，也不自动回滚其外部效果。

代码输入中的变量引用加入读集合，assign 目标加入写集合；沿用同次运行可共存读写必须有依赖顺序的规则。未激活分支先 skipped，不解析输入、不创建代码进程。跨互斥分支取值使用既有 `$value.kind=branch`，不能在源码里读取不存在的全局 api1/api2 代替分支映射。

<a id="runtime"></a>
## 3. Go 后端执行方案

### 3.1 技术选择与验证前提

采用 **goja + 受限独立 worker 进程**。goja 为纯 Go JavaScript 引擎，官方说明以 ES5.1 为完整基础并持续支持较新的语法；本项目应锁定具体依赖版本，验证常用语法后再公布支持范围，不能标成完整 Node.js 环境。[goja 官方仓库](https://github.com/dop251/goja)

同一个 Runtime 不跨任务复用；首版每次样本执行或正式执行启动一个受限进程、创建一个 Runtime、完成后退出。暂不建设进程池、跨节点全局变量、解释器缓存服务或通用插件注册中心。CodeRunner 仅是现有 workflow Runner 调用的本地计算组件。

**CodeRunner 在每个服务进程启动时只创建一次。** 在 `cmd/apihub/main.go` 初始化监督器、容量闸门和能力探测，再通过 httpapi.Dependencies 与 background.Service 注入。`background.NewWorkflowRunner` 可以继续每次创建工作流 Runner，但必须持有同一个 CodeRunner 引用，不能在该工厂或每个请求里新建限流器。编译、样本和正式执行共用它；运行 input/vars/JS Runtime 仍逐次独立。不同操作系统进程各有自己的限额，部署总容量按进程数计算，不能宣称是全工作区分布式限额。

goja Interrupt 仅能打断 JavaScript，不能中断原生 Go 函数及内置操作，所以主进程必须有独立墙钟超时、进程终止和系统内存限制；不能把 context 超时、goroutine 或 SetMaxCallStackSize 当作完整沙箱。[goja Interrupt 文档](https://pkg.go.dev/github.com/dop251/goja#Runtime.Interrupt)

### 3.2 进程边界及首版部署要求

```text
现有 workflow Runner
  → 分支/输入/变量验证
  → CodeRunner：限额、并发、固定启动器、超时监督
  → 受限进程 workflow-code-worker：goja 编译、main、返回检查
  → 主进程复核 JSON、Schema、assign
  → 发布步骤输出/变量 → 下一个节点
```

新增 `cmd/workflow-code-worker`，与服务一起构建和分发；只处理 stdin/stdout 协议，不开放用户可访问的执行 HTTP 服务。主进程使用固定绝对路径和固定参数直接启动，不用 `sh -c`，不接收请求指定的二进制路径。父进程监督进程组，超时/请求取消后终止、Wait 回收并清理本次资源。

**独立进程并不自动具有隔离能力。** 阶段零必须在目标 Linux 部署中落实受限启动器：worker 使用隔离的非特权身份，无认证材料、数据库配置、宿主可写挂载或继承的网络/服务文件描述符；禁止网络，限制文件系统可见范围，开启 no_new_privs。通过部署提供的 cgroup v2 委派或等效容器限制，保证每次执行有独立内存上限及可清理的进程组。选定的启动机制、权限、镜像/系统配置和验证命令必须作为阶段零产物固定下来，不自研通用容器平台，不给主服务开放任意 Docker 控制能力。

限制必须在读取/编译用户代码前生效。环境变量使用最小白名单，仅保留固定 UTC 等必要设置，不能继承主进程密钥。worker 不注册网络、文件、进程、模块加载宿主函数，不加载 goja_nodejs。禁用 eval/Function/Promise 等非首版接口并拒绝 async/await、模块脚本；静态检查只用于明确产品边界，不承担沙箱隔离保证。

首版正式运行只在验证过的 Linux 隔离配置启用。若启动器、worker、资源限制或版本不匹配，返回 runtime_unavailable；允许查看/保存草稿，但不能部署或运行含代码的流程，不回退到主服务进程执行。开发环境没有对应隔离能力时同样不能通过正式预览接口绕过。仅 API 的旧流程继续可用。

### 3.3 初始限额

下表为拟定默认值，阶段零用目标机器验证启动开销和峰值后定稿；后端统一配置并向 UI 提供有效值，用户不能逐节点放宽。

| 项目 | 首版拟定值 | 处理方式 |
| --- | --- | --- |
| 单节点源代码 | 32 KiB | 保存/部署拒绝超限；仍计入 graph 总计 256 KiB |
| input 映射配置 | 64 KiB | 沿用步骤配置限制；与解析后的实际数据区分 |
| 解析后输入 / 返回 JSON | 各 1 MiB | 超限明确失败，不截断业务值；先在父进程限长再传输 |
| JSON 最大嵌套 | 16 层 | 输入及返回均检查，根为第 1 层 |
| 一次执行墙钟 | 2 秒 | 从启动进程前开始，包含启动、编译、初始化、main 和序列化；取与工作流剩余预算的较小值 |
| 容量等待 | 最多 500 毫秒 | 取请求/工作流剩余预算的较小值；交互请求不排队，正式执行按第 3.3.1 节等待，超限 code_capacity_busy |
| worker 内存 | 每次 128 MiB | 系统硬上限，含 Go/JS 整个进程；不能仅限制输出或 Go 软内存目标 |
| JS 调用栈 | 256 层 | 防止深递归；不是内存限额的替代 |
| 每个服务实例的执行并发 | 2 | 共用总闸门并为正式执行保留容量；交互子上限及并发为 1 的行为见第 3.3.1 节；按进程数核算总容量 |
| 日志 | 最多 100 条且合计 4 KiB | 有界收集，超出停止收集并标记截断，不阻塞执行 |

代码输出和当前变量仍计入现有 8 MiB 运行数据预算；最终 output 保持 4 MiB 上限。1 MiB 是代码节点本身的更小限额，不放宽 API 或工作流的其他限制。临时输入副本与序列化开销纳入 worker 内存测试；父进程也必须采用有界读写，不能接收无限 stdout/stderr。

确定性配置固定 `TZ=UTC`，不开放真实系统时间和随机源作为业务输入。用 goja 的时间/随机源钩子设定固定 epoch 和固定种子，并在预置全局中让 Date.now、无参数 Date、Math.random 的常规调用报明确不支持错误；不能仅改全局名称后仍泄漏真实时钟。业务时间、偏移天数、需要的随机业务值由显式 input 提供；仅支持明确时区的时间字符串和 UTC 方法，不依赖服务器地区。相关钩子见 [goja Runtime 文档](https://pkg.go.dev/github.com/dop251/goja#Runtime.SetTimeSource)。

业务时间可选择[只读运行信息](workflow-node-design.md#run-metadata)：triggeredAt、scheduledFor、timezone、businessDate。服务端固定并派生这些值，代码只接收点选的命名输入；日期按计划时间还是触发时间计算的规则由该契约统一决定。JS 不直接读取快照，也不因增加时间来源而开放系统时钟或第三方日期库。

<a id="capacity-policy"></a>
### 3.3.1 正式执行与交互请求的容量分配

共享 CodeRunner 内只区分两类可信用途：已接受运行中的代码执行/内部编译属于正式执行，部署 compile 和两种样本执行属于交互请求。用途由服务端入口决定，不接受请求或脚本自报优先级。总并发上限 C 仍为每进程 1–16，不增加分布式调度器。

- **C ≥ 2：** 所有工作区的交互请求合计最多占 C−1 个名额，至少一个名额保留给正式执行；正式执行可使用全部 C 个名额。有正式请求等待时不再接纳新交互任务，正式等待者按到达顺序获得释放的名额。不终止已经执行的交互任务，也不在节点之间保留空闲 worker 名额。
- **C = 1：** 正式含代码流程从运行开始到结束登记到同一监督器的活跃运行计数，登记和交互接纳共用同步保护。只要存在这类正式运行或正式等待者，就拒绝新的交互请求；不因正式流程正在调用 API 而让预览插入占位。若正式运行到达时已有交互任务，先登记并等待其释放，最多 500 毫秒且发生在首个业务 API 之前；超时撤销登记并失败，本次业务 API 调用次数为零。多个正式流程仍可登记，其代码步骤按总闸门串行，不把整个流程串行化。
- 交互请求无法立即获得允许的名额时返回可定位的 code_capacity_busy，不排队、不自动重发；UI 保留配置/样本并显示“执行资源繁忙，可稍后再试”。部署逐个编译节点，每次释放名额后重新接纳，不为整张图长期占位；仍受 10 秒总部署预检预算约束。
- 正式代码步骤的等待最多 500 毫秒，计入剩余运行预算。正式负载自身也可能耗尽容量；优先规则不保证任意负载都成功。中途容量失败按 infrastructure 停止，不重放已完成 API。超时、取消、预检失败和运行退出均释放相应名额/登记；只含旧 API 的流程不登记代码活跃运行。

capabilities 的 code.local.capacityPolicy 返回有效的 totalConcurrency、interactiveConcurrency（C≥2 时为 C−1，C=1 时最大为 1 且受活跃运行约束）及 formalPriority=true，作为策略说明；不把一次空闲探测当作资源预留。两类计数和等待规则必须放在每进程共享实例中，不能只在预览 handler 增加独立限流器。

### 3.4 内部协议与诊断

协议版本单独为 1：请求包含 mode（compile/execute）、requestId、runtimeProfile、code、执行时的 input 和 inputSchema/outputSchema；资源配置由可信启动端传递，不能由用户源码覆盖。使用有长度上限的单个 JSON 消息，响应为 success/output/logs/duration 或明确 error。stdout 只写协议，console.log 进入有界日志集合，不用特殊 stdout 标记切割用户打印内容。

compile 模式只解析/编译和检查 main 声明，不执行任何用户顶层语句。execute 模式覆盖顶层初始化、函数调用、日志取值和返回检查的全部预算。worker 异常退出、无响应、协议多余内容或字段不匹配都明确失败；检测到 cgroup OOM 才报告 memory_limit，无法确认原因时报告 worker_failed，不能一律猜成超时。

复用 WorkflowDiagnostic，新增 `phase: "code"`，及可选 `line`、`column`（面向用户源码、从 1 开始），fieldPath 指向 /code、/input/{name}、/outputSchema 或 /assign/{index}。不暴露宿主文件路径、Go 栈、源码中的秘密或完整输入；未经安全处理的用户 throw 文本不进入公共调用错误。

| 错误类别 | 行为 |
| --- | --- |
| code_compile_error / code_entrypoint_invalid | 定位源码；部署/样本执行阻止继续 |
| code_input_invalid / code_output_invalid | 定位输入名或返回字段；实际值不自动转型 |
| code_execution_failed | 脚本异常，记录受限摘要及可用位置 |
| code_timeout / code_memory_limit / code_stack_limit | 终止本次进程，步骤失败，下游停止 |
| code_runtime_unavailable / code_capacity_busy / code_worker_failed | 明确基础设施错误，不退回其他执行方式 |
| 变量赋值失败 | 沿用 phase=variables，输出和变量均不发布 |

代码节点没有外部副作用，其可确认的局部失败不套用 API 的 unknown；如果整个运行的租约/最终持久化无法确认，仍遵循既有 unknown 收尾，不能由代码节点覆盖整体状态。

步骤失败分类及继续条件统一见[节点失败契约](workflow-node-design.md#failure-contract)。不能只新增几条 code_* 文本然后继续使用现有引擎对所有 failed 一律判断 onError 的分支；需要保留 failureClass、phase、errorCode、diagnostics 及适用的 apiCallStatus。代码的执行/输出/变量失败均停止，错误分类贯穿预览、事件、同步错误及异步运行结果。

<a id="transaction-boundary"></a>
### 3.5 部署编译与数据库事务边界

当前 `/workflows/{id}/deploy` 经 `a.audited` 包裹整个 handler，事务会覆盖 handler 内全部等待；startWorkflowRun 也在行锁事务中构建快照。新增 worker 调用不能直接塞进这些位置。

部署按以下顺序实现：

1. 校验权限/expectedVersion，读取当前定义并做结构检查和绑定解析；保留服务端读到的工作流版本、执行图摘要及 profile/runner 构建标识。
2. **事务外**通过共享 CodeRunner 的 compile 模式检查所有代码节点，仅解析/编译，不执行顶层语句。图总部署预检最多 10 秒，并受请求剩余预算约束；不足时明确预检超时，不能先部署部分节点。
3. 开启短事务，重新核对工作流版本、图摘要、绑定有效性及执行配置标识。定义发生变化返回 409；编译结果只对应服务端捕获的版本，不接受客户端提供的 compiled=true 或摘要作为通过证明。
4. 在同一短事务里更新部署状态/版本/调度并写审计；审计失败一并回滚。事务中不重新编译，不等待代码容量，不启动子进程。提交成功后才返回成功响应。

实现时仅调整工作流部署这条路由的事务位置：移除包住整个部署 handler 的 audited 包装，在最终提交阶段显式复用 Store.InTransaction 与审计写入；不取消审计原子性，也不改其他业务路由的审计约定。保存草稿不运行编译器，可保留原有短事务；preview-step 不套事务包装，不写业务审计正文。

触发及定时入队事务只做结构/实际 trigger/变量初值/绑定/快照检查和持久化；不调用代码进程。实际执行器拿到固定快照后，在第一个有副作用的 API 前完成运行环境预检。手动部署不以缺省 trigger 执行变量初始化，允许实际调用补齐输入；定时部署须检查默认输入。不要把当前传 nil/default input 的就绪检查直接扩展成所有操作共用的输入初始化入口。

<a id="deployment"></a>
### 3.6 角色、构建、能力与升级顺序

当前项目通过 scripts/build.sh 构建命令、scripts/start.sh 加载配置，README 提供非特权 Linux 服务示例；尚无现成的代码隔离启动器。阶段零必须针对这条实际部署路径交付固定 launcher、权限/资源配置、启动自检和验证命令，不能只留下“使用 cgroup 或容器”的概念。具体系统配置通过验证后锁定到部署文档，未经验证不预填可用。

| APIHUB_ROLE | 需要具备的代码能力 | 首版检查 |
| --- | --- | --- |
| all | 本地编译/预览/同步及后台执行 | 一个共享 CodeRunner；启动探测，接受触发及实际执行时复核 |
| api | 编译、预览、runtime 同步执行 | 必须安装 worker/launcher；本地可用不代表远端后台可用 |
| worker | 异步和手动入队后的执行 | 每个领取 workflow_run 的实例都安装匹配构建；实际执行前检查 |
| scheduler | 检查定义并定时入队 | 不执行脚本；核对 v2/代码启用配置及目标 profile，交后台实例执行 |

当前各角色均会监听 HTTP。角色名本身不替代路由权限或入口部署：若把编译/预览/同步请求路由到某进程，它也必须具备本地代码能力，否则明确拒绝；scheduler 不因收到 HTTP 请求就自动取得运行能力。

拟新增配置由 internal/config 校验，并同步 .env.example、configure/start/build 文档：

| 配置 | 约定 |
| --- | --- |
| APIHUB_WORKFLOW_V2_ENABLED | 首次升级默认 false；控制新增/修改 v2 执行定义、部署及新触发的开放，不用于抹去已有定义/快照 |
| APIHUB_WORKFLOW_CODE_ENABLED | 默认 false；打开前完成隔离及全部执行实例检查；关闭时停止新的代码编译/样本请求及含代码流程的新部署/触发，草稿编辑仍按 v2 开关处理 |
| APIHUB_WORKFLOW_CODE_WORKER_PATH / APIHUB_WORKFLOW_CODE_LAUNCHER_PATH | 管理员固定的绝对可执行路径；请求和源码不能覆盖 |
| APIHUB_WORKFLOW_CODE_MAX_CONCURRENCY | 每进程默认 2，首版配置范围 1–16；同一进程所有入口共用，实际部署还须核算宿主总内存 |

新代码编译/样本/部署/触发要求 v2 和代码开关均启用，代码预览不能绕过关闭的功能开关。读取历史不依赖这些开关。开关不构成运行中取消；已接受任务执行时的内部编译/运行可用原快照和兼容构建继续，必须保留所需 worker 及监督器直到任务结束。实际隔离环境失效、身份失效或构建不匹配仍会停止；不能为清理任务降低限制。资源限额来自固定 profile 和可信配置，向 UI 返回实际生效值，不另存到每个节点。

能力接口只如实报告当前处理请求进程的本地探测；异步返回“执行实例在领取后检查”，不根据一台 API 服务器的可用状态声称后台集群健康。暂不建设通用服务注册中心。各角色一致性由部署检查覆盖；worker 在运行开始前验证快照的 profile/构建、固定路径及隔离自检状态，失败时零次调用本次业务 API。该预检不执行用户源码、不启动各未命中代码节点；代码实际开始时仍检查容量/环境，预检不能保证整个运行期间资源永远可用。

首个 v2 发布顺序：暂停新的工作流触发并排空不兼容在途任务 → 保持新能力关闭，升级数据库所需布局列与所有 API/worker/scheduler 二进制及前端资源 → 逐实例确认能保真读取 v1/v2、安装并验证 worker/launcher → 确认旧实例不再接收请求或领取队列 → 开启 v2/代码并验证同步、异步、定时。构建脚本新增 workflow-code-worker，主服务与它作为同一发布产物分发；前端构建和嵌入服务重建分别核验。

旧二进制可能忽略新 JSON 字段，不能让它与已开放 v2 写入的新服务混跑。关闭新触发后仍保留 v2 数据和兼容读取器；已有 v2 图/快照时不得直接回退到只识别 v1 的版本。后续 profile 升级先排空对应任务或保留兼容执行构建，不能靠清空 code/assign 完成回退。

<a id="schedule-gates"></a>
### 3.6.1 开关关闭、恢复与定时调度

首版开关是部署配置，不增加管理端即时开关 API。变更时同步所有接收新触发的 API/worker/scheduler 配置，切换窗口先停止新请求接纳及调度入队；保留兼容执行器处理已接受任务。配置混用时不能声称已经全局关闭。v2 关闭影响全部 v2 新触发；仅代码关闭影响含 code 的 v2 流程，v1 和不受该开关影响的流程照常执行。

| 情况 | 部署状态 / nextRunAt | 新运行与恢复行为 |
| --- | --- | --- |
| 能力关闭 | 不修改工作流的 deployed 状态、定义或业务版本；nextRunAt 保留为计划游标 | 手动/runtime 新触发拒绝，runtime 发现不列为可调用；已接受 queued/running 任务继续按原快照执行 |
| 关闭期间计划到期 | 按现有 Cron/interval 规则推进到严格晚于本轮 now 的下一合法时间 | 不创建 operation/job，不记为一次执行失败；记录限长跳过原因，继续处理同批其他流程 |
| 恢复启用 | 入队恢复前统一重排受影响的已部署定时流程，保留合法未来计划；过期计划推进到严格晚于 resumeAt | 关闭/停机期间的计划不补跑，从保留或重算的下一次未来计划开始；未部署、paused、disabled 流程不被自动部署 |

EnqueueDueWorkflows 必须把“能力关闭”作为单条调度跳过处理，不能复用 ErrBusy 的 break 或通用错误的整批回滚。禁用到期项推进后继续扫描；批次采用可前进的游标或等效候选过滤，确保大量禁用项不会持续占据最早的 LIMIT 批次而饿死其他流程。推进失败按已有存储错误处理；非法调度沿用停止该计划及诊断规则，不能静默制造下一时间。

恢复步骤必须覆盖 scheduler 在关闭期间完全停机的情况：暂停所有调度实例的新入队 → 以一个固定 UTC resumeAt 执行受控维护命令，按工作区/受影响版本或 code 类型分页重排 → 全部成功后按新开关启动入队。维护命令只调用窄用途 Store 调度函数，不执行脚本/API；逐行加锁后复核当前状态/定义，避免覆盖并发编辑，更新 nextRunAt 并记录数量/失败摘要，不改业务版本或恢复已暂停流程。重排可按同一 resumeAt 重入，失败时保持入队关闭；切换期间已有任务的 scheduled_for 和快照保持不变。这个步骤纳入构建/发布操作文档和验收，不靠重启后碰巧扫描到任务实现恢复。

管理端按已保存图类型与 capabilities 的开关值派生“已部署 · 平台暂未开放执行”；禁用新运行/部署并显示原因，计划时间标为“规则计划，当前不会触发”。这不是新的 workflow/operation 状态，也不是后台健康判断。恢复后刷新能力与工作流 nextRunAt；不要求用户重新编辑或重新部署仍有效的流程。草稿编辑、只读历史及在途运行展示继续遵循第 3.6 节。

<a id="reuse"></a>
### 3.7 复用现有类型与校验组件

复用 internal/jsonutil 的 UseNumber、现有 jsonschema 依赖、executor.ValidateSchema 的安全规则及 FieldError/SchemaFailure 的诊断转换。抽取必要的小型通用校验入口，给工作流的受限 Schema 增加自己的关键词/类型形状检查；不能把本地节点伪装成 Action 后意外执行 API PrepareInput/default 逻辑，也不能扩大 API 现有 Schema 限制。

JSON 数字从 API 响应、trigger、样本解码到送入 JS 前保持 json.Number；先判断安全整数/有限数和精度策略，再转换。worker 返回字节也用同一解析器复核。新 Schema 的缓存使用本次编译/执行内缓存或有界缓存，不能把每次草稿变化永久追加进进程级无界缓存。只共享规则和可信不可变结果，不共享用户 Runtime、输入或变量。

<a id="ui"></a>
## 4. 编辑器 UI 与字段传递体验

添加菜单为 API、数据处理、代码、条件判断。代码说明为“组合多个节点的数据，计算并返回新字段”。卡片显示标题、JavaScript、输入/输出数量、变量更新数和配置/运行状态，不铺满源码。

```text
计算订单金额                  JavaScript
3 个输入 · 1 个输出 · 更新 1 个变量

右侧：[配置] [样本执行]

输入变量       类型        允许空值   来源
quantity       integer    否         [订单 API → quantity]
unitPriceCents integer    否         [商品 API → unitPriceCents]
discountCents  integer    否         [优惠 API → discountCents]
[+ 添加输入]

代码                       [展开编辑]
function main(input) {
  return { totalCents:
    input.quantity * input.unitPriceCents - input.discountCents };
}

返回字段       类型        允许空值   说明
totalCents     integer     否         应付金额（分）
[+ 添加返回字段]

更新流程变量（可选）
payableCents ← [当前节点返回 → totalCents]

[执行样本]    验证代码计算；上游字段映射需另行验证
```

输入行与其他节点共用来源树、搜索、Schema/样本来源标识和定位源节点功能。一次可以从多个上游点选字段；直接选整个对象或数组时也保留类型。添加 input 行同步创建 Schema 字段，类型未知时要求确认；删除/改名列出源码使用提示，不用正则替换任意源码。源码编辑是普通文本编辑，不自动把变量名变动传播成不可靠的代码重写。

代码区提供三个固定模板入口：多字段计算、列表按 ID 关联、结构转换。模板包含同步 main、建议输入/返回声明和简短注释；应用前展示会替换的源码/字段，用户确认后一并修改并支持撤销，不自动覆盖已有配置、不增加代码生成服务。输入行提供“插入 input.quantity”并配合编辑器补全；点选来源创建映射，插入标识只编辑源码，两种操作不混淆。

代码编辑器按需加载，采用 CodeMirror 6 的 JavaScript 语言、行号、缩进、查找、括号匹配和诊断组件；不预装 IDE、终端或包管理器。依赖已锁定在 web/package.json，包体积及手机/键盘检查见开发验收记录。仅启用 JavaScript 模式，关闭 JSX/TypeScript；语言与补全扩展参考[官方 JavaScript 包说明](https://github.com/codemirror/lang-javascript)。源码编辑器给出 input 字段补全，类型来自 inputSchema；后端 goja 编译结果才是运行兼容性的最终依据，浏览器语法高亮成功不等于可执行。

侧栏提供“展开编辑”，保留输入/输出设置；桌面展开后可并排看代码与样本，手机用全宽页签。编辑器获得焦点时，撤销、删除、Tab 等按文本编辑处理，不触发画布删除/撤销；提供退出 Tab 缩进捕获的键盘路径。不能把大 JSON/报错撑出页面。

下游 API 的来源树展示“计算订单金额 → totalCents · integer”，即使未执行也可从声明看到；嵌套未知字段标为未知。样本执行可提示实际返回结构差异，用户确认后才更新声明，不自动覆盖已配置的 Schema 或真实参数。

修改源码、输入来源/Schema、返回声明、assign 或相关祖先配置后，清除旧样本结果的有效标记；在途响应带配置指纹，过期结果丢弃。新节点默认 main 返回命名 result 字段与相应声明，用户明确修改；空模板只表示“待配置”，不标绿为运行成功。

<a id="preview"></a>
## 5. 样本执行、事件与历史

### 5.1 接入既有预览设计

扩展总文档拟新增的 `POST /api/workflows/preview-step`，不另建公开任意代码执行 API。共用 definition、stepId 和权限/图大小检查，新增 previewMode；省略时为 workflow，保持已有预览请求兼容。草稿无需至少一个 API，也不要求其他节点完成绑定，但已填写的非法引用/依赖仍需诊断。

| previewMode | 样本来源 | 实际验证范围 | 不作出的结论 |
| --- | --- | --- | --- |
| code_inputs | 命名 sampleInput 对象，按 inputSchema 渲染 | 当前代码的输入类型、main、返回 Schema 和运行限额 | 不判断分支激活、不解析上游映射、不验证 assign、不证明流程可部署 |
| workflow | 原有 trigger/sampleOutputs/sampleStatuses/sampleVariables | scope、来源解析、代码结果及临时变量赋值 | 不调用真实上游、不证明样本来自实际路径 |

代码 UI 默认使用“填写输入样本”，只填当前命名输入，避免为三个标量准备三份完整 API 响应。展开“验证上游字段映射”后使用 workflow 模式。code_inputs 仅适用于 code 节点；sampleInput 按字面 JSON 传入，不展开字符串中的模板，不保存到节点 input 或真实运行接口。请求同时携带该模式与 trigger/sampleOutputs/sampleStatuses/sampleVariables/sampleResult/sampleRunMetadata 时拒绝，避免两套样本覆盖关系不清；workflow 模式不接受 sampleInput。workflow 中需要运行时间来源时显式填写 sampleRunMetadata，字段、派生业务日期及缺样本行为按节点第 2.8 节。

以下是与完整 definition、stepId 一起提交的请求字段片段：

```json
{
  "previewMode": "code_inputs",
  "sampleInput": { "quantity": 2, "unitPriceCents": 1000, "discountCents": 100 }
}
```

sampleInput 的键、类型和 nullable 按正式输入 Schema 检查，必填字段不能遗漏；允许空值的控件明确选择 null，不以清空输入框代替。所有模式的样本集合上限仍为 128 KiB，解析后还需满足代码 1 MiB 限额；正式执行不受预览样本请求上限约束。样本只保留当前页面内存，模式切换后不混用旧响应。

两种代码预览均是真正的 **样本 JavaScript 执行**，共用正式 CodeRunner、profile、并发和限制。workflow 模式先判断 scope，未激活节点不启动用户代码执行，assignmentPreviewStatus=not_applicable；缺少合法输入样本为 unavailable，不能拿初值假装祖先已更新的流程变量。code_inputs 作为单节点计算验证不求值 scope，响应省略 activationEvaluation，并返回 validationScope=code_only、mappingPreviewStatus=not_evaluated、assignmentPreviewStatus=not_applicable。workflow 返回 validationScope=workflow_sample，不能用 code_only 的成功结果把整条路径标成可执行。

API 预览仍只构建请求；代码在受限进程中计算，只有 workflow 模式用结果计算临时 assign。代码结果来自实际样本执行，不接受 sampleResult 伪造当前节点输出。不创建 operation/job、不持久化样本、不改变工作流变量、不触发 provider。前端显示输入/输出脱敏预览、耗时、诊断、日志、validationScope 和 assignmentPreviewStatus；业务结果超限时报错，展示摘要超限则明确截断，两者不可混淆。

预览仅 owner/admin/developer 可执行，viewer 禁止 POST。服务端限制请求体、频率和每工作区在途预览，建议每工作区最多 1 个，且仍受实例总闸门约束；输入变化不自动执行，用户点击才请求。HTTP 请求取消后终止本次 worker，不新增工作流运行取消功能。

### 5.2 能力、事件与快照

新增管理端只读 `GET /api/workflows/capabilities`，返回 v2Enabled、code.enabled、code.local（available/runtimeProfile/limits/capacityPolicy/reasonCode）及 code.backgroundCheck="on_execution"；不返回二进制路径/宿主配置，也不提供未经探测的后台 ready=true。注册路由时先于 /{id} 匹配。编辑器按 v2/代码开关与本地能力共同控制“执行样本”和部署编译；含代码流程的新触发检查开关，后台任务领取后实际检查执行环境，按[角色部署规则](#deployment)处理。该接口不改变 runtime token 的发现/授权范围，已接受运行的关闭开关语义同第 3.6 节。

开始/完成事件沿用总文档结构，补 code 执行耗时、runtimeProfile、输出/日志/变量摘要、错误位置。事件内输入、输出、日志和变量内容共用限长脱敏策略，合计最多 4 KiB 预览内容，避免每项各 4 KiB 无上限叠加；日志禁止持久化原始任意文本，可保留固定“用户日志”类型的脱敏预览，safejson 不能保证识别任意文本秘密，默认不记录自由文本内容。业务输入、完整源码和完整中间输出不写入通用日志。

触发快照冻结源码、input/outputSchema、输入映射、assign、profile 及实际 runner 构建标识（包括 goja 固定版本），和既有绑定、trigger、runMetadata、initialVariables 一起加密。编辑后排队运行仍用原快照；不把今天的源码或时间套入历史。部署升级应先排空旧构建的在途/排队代码运行，或保留对应执行构建；版本不兼容时明确失败，不能静默用新引擎解释旧快照。

历史 view 返回类型、标题、声明字段、profile 和执行摘要，默认不返回完整源码；错误行号属于当次快照，不能直接跳到已改变的当前源码后宣称是原位置。当前版本与历史指纹一致时才可定位当前编辑器，否则显示历史版本提示。完整源码的历史下载/编辑不在首版范围。

<a id="audit-projection"></a>
### 5.3 保存、部署与审计投影

当前 saveWorkflow/deployWorkflow 把整个 saved 对象传给 safejson.Marshal 后审计。safejson 按字段名脱敏，无法理解 code 字符串中的内容；因此不应继续用整对象序列化产生新的工作流审计记录，也不应全局把名为 code 的字段都屏蔽，误伤普通业务错误码。

新增小型 workflowAuditSummary 投影，保存/部署/暂停等记录只含工作流 ID/版本/状态、变更节点 ID/类型、变更类别和每个代码节点的源码 SHA-256/字节数/runtimeProfile。输入/输出声明只记录字段名、类型及 required/nullable，不记录 description 中的任意文本、源码、固定参数、默认输入、样本、完整变量值或自由文本日志。before/after 均用相同投影，再经过 safejson 和总大小限制；摘要超限标注截断。摘要用于确认变更，不是源码备份或编译通过凭据。

当前工作流详情仍按现有定义读取权限展示可编辑代码；运行源码仍在加密快照中，本文不把工作流定义表宣称为已加密存储。代码中的业务秘密应通过既有 API 认证处理，不新增代码内凭据入口。旧审计记录不自动迁移或清理，沿用原保留策略；本次必须阻止新源码和样本进入通用审计/日志。

## 6. 接入文件及完整执行顺序

| 位置 | 开发内容 |
| --- | --- |
| `internal/workflow/definition.go`、`validate.go` | code 类型、Schema/限额、依赖读取、scope、assign、v1/v2 保真、结构化诊断 |
| `internal/workflow/engine.go` | code 分派及 CodeRunner 注入，复用变量候选提交、预算、状态/事件、失败停止 |
| `cmd/apihub/main.go`、`internal/config/config.go` | 每进程单例 CodeRunner、配置与启动探测，向 HTTP/后台注入；读取/新触发/在途任务开关语义分开 |
| 拟新增 `internal/workflow/code.go` | 小型请求/返回契约、受限进程调用、输入/输出复核；复杂时再拆 supervisor 文件 |
| 拟新增 `cmd/workflow-code-worker` 及独立内部包 | goja profile、compile/execute、严格 JSON 转换、限长协议；主服务不直接调用 VM |
| `internal/store/workflows.go` | 序列化不丢 code 字段，加密快照保存源码/profile/构建标识/runMetadata；只为 API 解析绑定；实现禁用计划推进与受控恢复重排 |
| `internal/httpapi/workflows.go`、`internal/httpapi/api.go`、`internal/background/workflow.go` | 共用执行器；部署编译移出 audited 外层事务、短事务内原子审计；两模式预览/能力/权限/源码审计投影 |
| runtime 发现/预检、服务初始化 | 全部 API 授权照常检查，本地节点不要求 action；启动与健康检查固定 worker 配置 |
| `web/src/pages/workflow-model.ts` 等拟新增编辑器文件 | code 联合类型、来源树、卡片、输入/输出表、编辑器、样本面板、诊断定位、复制/删除和引用检查 |
| scripts/build.sh、配置示例、README、测试脚本及调度维护命令 | 同批分发主服务与 worker，固定 launcher/角色检查；关闭、排空、按 resumeAt 重排后启用的操作流程；真实子进程集成与回归 |

一次正式代码步骤的实现顺序固定为：

此前，整次运行已在事务外完成代码 profile/环境预检、运行身份检查，以及单并发策略要求的活跃运行登记和已有交互清空；未通过时不调用任何业务 API，退出时释放登记。runMetadata 已随快照固定。执行各激活节点时继续验证身份有效性，本地节点不要求虚构 API 授权；见[节点授权契约](workflow-node-design.md#local-node-auth)。

1. 检查 scope 和依赖完成状态；未选路径记录 skipped 后结束；激活路径先检查运行身份，再解析用户输入或启动代码。
2. 固定进入节点时的 vars；解析 input 的 ValueSource，检查引用、Schema、数字精度和输入限额。
3. 在剩余运行预算内获取代码容量；启动限制已生效的 worker，写入有界请求。
4. worker 配置 profile，编译源码，执行顶层初始化，检查并调用 main；将严格 JSON 输出送回。
5. 父进程验证退出状态、唯一完整响应、输出 Schema/体积及总数据预算；失败时不发布输出。
6. 用当前候选输出及进入节点时的 vars 准备 assign，一次验证全部变更。
7. 一次发布输出/变量/成功状态，记录事件，释放资源；失败路径同样终止/回收进程并释放容量。

“一次发布”是运行内上下文提交；最终结果落库仍遵循既有运行事务和 unknown 规则，不宣称能回滚之前的外部 API。

<a id="examples"></a>
## 7. 具体样例与新增场景

### 7.1 多来源金额计算并传给下一 API

流程：开始 → 订单 API → 商品 API → 优惠 API → 计算金额 → 支付 API → 结束。三个 API 串行即可，无需并行节点；计算步骤 dependsOn 明确包含三个来源。

以下是 code 步骤片段，省略的 API/变量需在完整图中存在。开始声明 payableCents 为 integer，initial=0；API1–3 分别返回 quantity=2、unitPriceCents=1000、discountCents=100。

```json
{
  "id": "calc_total",
  "type": "code",
  "title": "计算订单金额",
  "language": "javascript",
  "runtimeProfile": "js-v1",
  "dependsOn": ["get_order", "get_product", "get_discount"],
  "input": {
    "quantity": "{{get_order.quantity}}",
    "unitPriceCents": "{{get_product.unitPriceCents}}",
    "discountCents": "{{get_discount.discountCents}}"
  },
  "inputSchema": {
    "type": "object",
    "properties": {
      "quantity": { "type": "integer" },
      "unitPriceCents": { "type": "integer" },
      "discountCents": { "type": "integer" }
    },
    "required": ["quantity", "unitPriceCents", "discountCents"]
  },
  "code": "function main(input) {\n  if (input.quantity < 0 || input.unitPriceCents < 0 || input.discountCents < 0) throw new Error('negative amount');\n  var subtotal = input.quantity * input.unitPriceCents;\n  if (subtotal > 9007199254740991) throw new Error('amount too large');\n  var total = subtotal - input.discountCents;\n  if (total < 0) throw new Error('discount exceeds subtotal');\n  return { totalCents: total };\n}",
  "outputSchema": {
    "type": "object",
    "properties": { "totalCents": { "type": "integer", "description": "应付金额，单位分" } },
    "required": ["totalCents"]
  },
  "assign": [{ "variable": "payableCents", "value": "{{calc_total.totalCents}}" }]
}
```

预期输出 `{ "totalCents": 1900 }`。支付 API 的 amountCents 点选 calc_total.totalCents，或在其依赖包含该写入节点后读取 vars.payableCents；结束只映射需要返回的字段。金额异常时停止，不自动把负数改成 0。

### 7.2 扩展覆盖矩阵

以下场景补充原有 1–15，不改变原场景编号，也不要求为简单场景增加代码节点。

| 新增场景 | 节点组合与输入 | 核对结果及边界 |
| --- | --- | --- |
| C1：多 API 算一个值 | 三个 API → 代码 → API/结束 | 上例返回 1900，下游收到 number，不能把映射当文本公式 |
| C2：两列表按 ID 关联 | 订单 API + 商品 API → 代码 → 结束 | 订单 productId=P1 与商品 P1 名称匹配；声明 items 数组；重复商品键、缺失匹配明确抛错或按业务显式处理，不默认取第一条 |
| C3：嵌套结构展开和汇总 | API → 数据处理（可选过滤）→ 代码 → 结束 | 两笔同客户金额 100/200 返回该客户 sumCents=300；原输入不变，超限不返回部分成功 |
| C4：结合流程变量选择规则 | 开始声明 rate → API → 条件 → 分支代码赋值 → 公共 API | 分支可用不同公式，仅命中路径更新；公共 API 等待汇合、只调用一次，读取正确值 |
| C5：规范字段后决定调用 | API1 + API2 → 代码 → 条件 → API3/结束 | 从不同路径得到统一 orderId、canSubmit；0/false/空数组按类型保留；缺必需值失败，不用真值兜底掩盖错误 |
| C6：按本次业务日期组装查询 | 开始（定时）→ 代码 → 查询 API → 结束 | 代码 date 输入点选运行信息 businessDate，返回命名 query 对象；采用节点第 2.8 节跨日例，发送日期为 2026-10-03，排队跨日及重读不变；无需计算时直接映射给 API |

日期计算、字符串转数值等采用显式 JS 逻辑；没有被定义的业务舍入、时区、重复键、缺失处理不由平台猜测。数组迭代只处理 input 内数据，原场景 15 的“单条 API 逐条调用”仍不支持。

<a id="development"></a>
## 8. 详细开发顺序与阶段出口

各阶段成果均须可审查；本节是任务清单，不预填完成或通过。与总文档阶段零/一/二合并实施，避免维护两套 UI 或执行器。

固定推进顺序为 **v2 保存/快照保真 → API＋代码纵向执行链 → 流程变量/数据处理/多分支 → 完整可视化及历史联调**。UI 原型可提前验证，但不能等所有节点表单完成后才验证后端；未完成的执行类型不得部署，完整功能开放前保持新能力开关关闭。

### 阶段 A：运行可行性和交互原型（总阶段零）

1. 锁定 goja 版本、js-v1 能力清单和许可，验证 main、对象/数组、map/filter/reduce、常用字符串/数字/正则和 UTC 日期操作；ES6 语法逐项验证，不凭编辑器支持推断。
2. 做真实独立进程最小验证：编译、返回、超时终止、递归、内存压力、原生内置长任务、进程回收、无网络/文件/环境秘密可见性。对 worker 限制生效时间和父进程不受拖垮给出证据。
3. 按第 3.6 节完成现有构建/非特权服务部署的固定启动器及资源配置，验证同一服务进程两个并发任务的总占用与分角色能力；记录冷启动耗时、峰值及是否调整 2 秒/128 MiB 默认值。
4. 用受控样本验证“选三个输入 → 写 main → 声明输出 → 下一 API 点选字段”，包含 nullable、固定模板、两种样本验证范围、展开编辑、手机、键盘和错误位置；锁定编辑器组件。

**出口：** 运行 profile、启动方式、限额、UI 原型和失败验证记录齐全。隔离不可落地时停止启用代码执行，保留设计和其他节点工作；不能跳过该项直接上线主进程 goja。

### 阶段 B：类型、校验与快照（总阶段一前半）

1. 增加 code 联合类型、Schema 与协议，确定未完成代码可保存为草稿：JSON/结构/大小仍校验，空 main 或语法错误作为草稿诊断；部署必须通过受限 compile 模式。旧 API 草稿规则不变。
2. 复用模板解析收集 input/assign 依赖与变量读写；源码原文不解析模板。支持 scope 和按分支输入，拒绝自身 input 引用、越分支和未排序读写。
3. 修改绑定遍历、快照、版本转换、runtime 发现，非 API 步骤不再要求 action/集成/账号；未知类型及旧客户端覆盖 v2 必须拒绝。
4. 设计并接通 worker 构建标识、profile 固定与升级兼容检查；保存前后、触发快照往返完全一致。复用已有 Schema/json.Number/FieldError，统一 type＋null 与流程变量 nullable；接通 kind=run、全部触发入口的 runMetadata 和变量初始化顺序，保持旧 run 步骤兼容。
5. 完成部署的事务外编译与版本摘要校验、短事务内部署/审计提交，以及 workflowAuditSummary；加入至少一个 API 的部署/发现/触发校验，草稿及 code_inputs 样本不误拦。

**出口：** v1 无损；含 code 的 v2 正确保存/部署校验/快照，源码和特殊字符串不丢失，部署编译不执行用户顶层代码、不持数据库事务等待；慢编译期间并发改定义会冲突而非部署错版本，审计无源码/业务值。

### 阶段 C：执行组件与全路径接入（总阶段一）

1. 先实现服务启动时共享的 CodeRunner、有界协议、受限启动、正式优先的容量控制、context/墙钟监督、可靠回收；复用阶段 A 验证的隔离配置。多次 NewWorkflowRunner 必须指向同一容量闸门，覆盖 C=1 的运行登记与 API 前接纳检查。
2. 完成 JSON 输入复制、nullable/数字检查、profile 初始化、compile/execute、严格输出与日志限制；父进程复核返回。先接通 API1 → API2 → 代码（共同计算）→ API3 → 结束，不依赖变量、数据处理或分支。
3. 在这条简单链上验证管理/runtime 同步、异步、定时、code_inputs/workflow 样本，以及共享容量、环境预检、逐节点身份和分类失败停止。API3 只调用一次；代码失败不重放 API1/2。此里程碑必须先通过，再扩展复杂节点。
4. 再接变量初始化/assign 原子提交、数据处理、IF/多 ELSE IF/ELSE、scope/汇合和完整 C1–C6；补总预算和开始/完成回调，覆盖不发布候选输出、未选分支、身份失效、取消和 worker 不可用。条件可承接汇合；同时完成禁用定时项跳过及恢复重排。

**出口：** 简单 API＋代码链先通过，再完成含变量的 1900 示例和 C1–C6；代码失败不重放之前 API，未选分支零次用户代码执行；各入口语义一致，共享并发不随运行次数增长，父进程在超时/内存失败后仍可处理下一正常请求。

### 阶段 D：完整编辑与样本执行（总阶段一后半）

1. 添加卡片和侧栏，复用来源、Schema、nullable、变量赋值控件，接入按需加载编辑器、三个固定模板及输入插入/补全。
2. 实现返回字段进入下游来源树、跨节点定位、字段变动影响提示；不静默改写用户代码或下游路径。
3. 扩展 preview-step 的两种模式和 capabilities 的本地/后台检查范围，展示样本输入/返回、诊断行列、适用的变量候选变更和有效限额/容量策略；接入 sampleRunMetadata、只读业务日期及开关关闭提示；缺输入/环境、资源繁忙或只验证代码时提示准确。
4. 完成自动清除过期预览、在途响应丢弃、编辑器内快捷键、撤销、复制、dirty/版本冲突和离开保护。

**出口：** 用户可在 UI 完成 C1–C6，无需手写工作流引用语法；样本执行不调用任何 API，desktop/mobile/keyboard 都可完成核心路径。

### 阶段 E：运行追踪、回归及交付（总阶段二）

1. 接通历史 profile/字段摘要、状态、耗时、受限日志和代码位置；展示原快照信息并区分历史/当前代码。
2. 完成第 9 节真实子进程、引擎集成、接口权限和浏览器测试；合并原 15 场景回归，不只验证计算函数。
3. 构建服务与 worker，按第 3.6 节逐角色演练能力关闭/启用、旧实例退出、在途排空、版本不匹配拒绝；覆盖 scheduler 停机后使用同一 resumeAt 重排、失败不启用、重入与大量禁用项不阻塞其他流程；不把本地能力当成后台集群健康，也不让旧读取器领取 v2 任务。
4. 文档记录实际命令、退出状态、测试环境及未运行项目；本地测试、前端构建、嵌入资源重建、服务更新和真实 provider 验收分别报告。

**出口：** 功能与隔离均通过对应环境验证后才可启用。回退版本前禁止向不识别 code 的旧服务分发 v2 图；暂停含代码流程的新触发、处理在途任务并保留定义/历史，不能删字段完成“兼容”。

## 9. 验收与测试安排

| 层次 | 必测项目 | 通过标准 |
| --- | --- | --- |
| 定义/持久化 | v1/v2、code 字段往返、草稿错误、部署编译、Schema、源码内模板文本、限额 | 不降级、不丢字段；部署不执行顶层代码 |
| 输入/输出 | 多来源、深对象、nullable、缺失/null/空字符串/0/false/空数组、错类型、精度、返回额外/缺字段、循环/访问器/Proxy/Promise | 共用 Schema 规则；nullable 不等于可遗漏；异常不静默转换 |
| 事务与审计 | 慢编译并发修改、事务等待、部署审计失败、源码含自由文本、超长摘要 | 编译不占事务；新版本不冒用旧检查；部署与审计全成/全不成；不写原始源码/样本 |
| 共享并发 | 多 Runner/工作区混合请求；C=2 预览最多占 1、正式等待优先；C=1 正式登记阻止新预览、已有交互在 API 前等待；取消与超时释放 | 总额与交互子限额不超限；正式忙与交互忙可区分；单并发接纳失败零次业务 API；不重放已完成 API，不倍增闸门或泄漏登记 |
| 真实 worker | 无限循环、深递归、内置长操作、超限内存、超大 stdout/stderr、初始化/返回阶段错误、取消 | 系统限制实际生效；子进程可回收；父进程和下一任务正常 |
| 隔离 | 环境/网络/文件能力、继承描述符、最小权限、宿主挂载、并发与资源门限 | 没有可达的服务凭据/出站能力；限额在用户代码执行前生效 |
| 引擎 | C1–C6、变量原子提交、无序读写、互斥/空分支、共享汇合 | 结果及调用次数准确；失效分支不启动 worker；旧 API 不重放 |
| 全入口与身份 | 管理/runtime 同步/异步/定时、纯代码草稿与部署、排队/最后 API 后 Token 失效、viewer/跨工作区/超频 | 单节点样本可用，纯计算不可部署；身份拒绝停止本地计算与最终发布；API 授权不绕过 |
| 样本范围 | code_inputs 字面样本、两模式互斥、必填/null、分支未知、assign、旧请求不带 mode | code_only 不声称验证映射/激活/赋值；workflow 对齐正式解析；原接口请求兼容 |
| 分类失败 | API 响应 Schema、代码输出 Schema、输入错误、赋值/权限/环境失败、unknown | 仅 v2 api_response 可按配置继续；v1 保留原行为；各入口诊断一致，无 API 自动重放 |
| 角色与升级 | all/api/worker/scheduler、开关关闭、缺 worker、构建不匹配、旧实例排空 | 本地/后台能力不混淆；预检失败零次业务 API；已接受任务/历史不被开关误删除 |
| 调度开关 | 混合 v1/v2/code、禁用项多于批次上限、关闭期间到期、scheduler 停机、按 resumeAt 恢复及失败重入 | 禁用项不入队/不整批回滚；其他流程不饥饿；未来计划保留、过期计划不补跑；不恢复暂停项、不改变在途快照 |
| 运行时间 | C6、时区/夏令时、跨日触发与领取、手动 scheduledFor=null、时间样本缺失/伪造派生值、旧 run 步骤 | 快照时间稳定，按计划或触发时间正确派生日期；样本使用同一函数、缺失不取当前时间；只映射显式输入、不开放 JS 时钟 |
| 快照/历史 | 排队后改代码、profile/构建变化、历史删除/缺事件 | 按当次快照处理；不使用当前源码伪造历史 |
| UI | 字段选择/补全、返回树、样本过期、行列定位、快捷键、20 混合步骤、两种桌面及 390×844 手机 | 核心配置无需手写引用；无画布快捷键误操作、遮挡或控制台错误 |

后端先运行变更包测试及真实 worker 集成，再运行项目规定的检查；前端运行相关 Vitest 和构建。数据库/队列集成依赖 PostgreSQL/Redis，实际通过才记录成功。模拟 API 用于核对调用次数与请求体，不能把 mock 或 Node.js 执行示例替代真实 goja/隔离验收。只修改本文时不需要启动服务或重建资源。

本次交付仍是确定性的多 API 编排：简单映射用来源选择，常见列表处理用数据处理，多来源计算用 JavaScript，分流用条件，外部调用用 API。
