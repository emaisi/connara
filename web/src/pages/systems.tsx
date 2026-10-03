import { useCanWrite } from "../permissions";
import { useResourcePages, PageControls } from "../pagination";

import { ArrowRight, Plus } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router";
import {
  DEFAULT_CUSTOM_SYSTEM_DESCRIPTION,
  systemDescriptionForDisplay,
  statusLabel,
  useDemo,
  type DemoSystem,
} from "../demo";
import { useLanguage } from "../i18n";
import { Badge, Button, Card, Field, Modal, MutationForm, PageHeader, fieldClass } from "../ui";
import { api } from "../api";

import {
  authInstancesFor,
  authInstanceName,
  authTemplateName,
  availableAuthMethods,
  SearchBox,
  Tabs,
  Logo,
  MiniStat,
  ListRow,
} from "./core-shared";
export function DemoProvidersPage() {
  return (
    <div className="grid gap-6">
      <PageHeader
        title="系统目录"
        description="统一管理目录系统与自建系统、系统分组，以及系统可使用的认证实例。"
        actions={<Badge tone="info">系统分组 · 认证模板 · 认证实例</Badge>}
      />

      <SystemDirectoryPage />
    </div>
  );
}

type AuthTemplateOption = { id: string; templateKey: string; name: string; status: string };

function SystemAuthTemplatePicker({
  name,
  templates,
  selectedKeys,
  defaultKey,
  inUseKeys = [],
  disabled = false,
  onChange,
}: {
  name: string;
  templates: AuthTemplateOption[];
  selectedKeys: string[];
  defaultKey: string;
  inUseKeys?: string[];
  disabled?: boolean;
  onChange: (selectedKeys: string[], defaultKey: string) => void;
}) {
  const { t } = useLanguage();
  return (
    <fieldset className="min-w-0" disabled={disabled}>
      <legend className="text-sm font-bold">支持的认证模板</legend>
      <p className="mt-1 text-xs text-[var(--muted-text)]">可选择多个模板，并指定新建认证实例时优先使用的默认模板。</p>
      <div className="mt-2 grid gap-2 rounded-xl border border-[var(--border)] p-3 sm:grid-cols-2">
        {templates.map((template) => {
          const selected = selectedKeys.includes(template.templateKey);
          const inUse = inUseKeys.includes(template.templateKey);
          return (
            <div key={template.id} className="flex min-w-0 flex-wrap items-center justify-between gap-2 text-sm">
              <label className="flex min-w-0 items-center gap-2">
                <input
                  type="checkbox"
                  checked={selected}
                  disabled={inUse}
                  onChange={(event) => {
                    const next = event.target.checked
                      ? [...selectedKeys, template.templateKey]
                      : selectedKeys.filter((key) => key !== template.templateKey);
                    onChange(next, next.includes(defaultKey) ? defaultKey : (next[0] ?? ""));
                  }}
                />
                <span className="min-w-0 break-words">{template.name}</span>
              </label>
              <div className="flex items-center gap-2">
                {inUse && <Badge tone="info">实例使用中</Badge>}
                <label className="flex items-center gap-1 text-xs text-[var(--muted-text)]">
                  <input
                    type="radio"
                    name={name}
                    aria-label={`${t("设为默认")}: ${t(template.name)}`}
                    checked={selected && defaultKey === template.templateKey}
                    disabled={!selected}
                    onChange={() => onChange(selectedKeys, template.templateKey)}
                  />
                  默认
                </label>
              </div>
            </div>
          );
        })}
      </div>
      {inUseKeys.length > 0 && (
        <p className="mt-2 text-xs text-[var(--muted-text)]">已有认证实例使用的模板不能移除。</p>
      )}
    </fieldset>
  );
}

function SystemDirectoryPage() {
  const demo = useDemo();
  const navigate = useNavigate();
  const { t } = useLanguage();
  const canWrite = useCanWrite();
  const [query, setQuery] = useState("");
  const [scope, setScope] = useState("全部系统");
  const [group, setGroup] = useState("全部分组");
  const [selected, setSelected] = useState<DemoSystem | null>(null);
  const [detailTab, setDetailTab] = useState("API");
  const [systemApis, setSystemApis] = useState<any[]>([]);
  const [apiQuery, setApiQuery] = useState("");
  const [apiStatus, setApiStatus] = useState("");
  const [apisLoading, setApisLoading] = useState(false);
  const [apisError, setApisError] = useState(false);
  const filteredApis = systemApis.filter(
    (row) =>
      (!apiStatus || row.status === apiStatus) &&
      [row.name, row.description, row.actionKey, row.relativePath, row.httpMethod].some((value) =>
        String(value ?? "")
          .toLowerCase()
          .includes(apiQuery.trim().toLowerCase()),
      ),
  );
  useEffect(() => {
    setDetailTab("API");
    setSystemApis([]);
    setApiQuery("");
    setApiStatus("");
    setApisError(false);
    setApisLoading(Boolean(selected));
    if (!selected) return;
    let cancelled = false;
    void api
      .actions()
      .then((rows) => {
        if (!cancelled) setSystemApis(rows.filter((row) => row.systemKey === selected.service));
      })
      .catch((error) => {
        if (!cancelled) {
          setApisError(true);
          demo.notify(error);
        }
      })
      .finally(() => {
        if (!cancelled) setApisLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [selected]);

  const [systemOpen, setSystemOpen] = useState(false);
  const [groupsOpen, setGroupsOpen] = useState(false);
  const [newGroup, setNewGroup] = useState("");
  const [editingGroupId, setEditingGroupId] = useState("");
  const [editingGroupName, setEditingGroupName] = useState("");
  const [systemName, setSystemName] = useState("ERP");
  const [systemId, setSystemId] = useState("internal-erp");
  const [systemGroup, setSystemGroup] = useState("内部系统");
  const [systemTemplateKeys, setSystemTemplateKeys] = useState(["username-password-token"]);
  const [systemDefaultTemplateKey, setSystemDefaultTemplateKey] = useState("username-password-token");
  const [groupRows, setGroupRows] = useState<Array<{ id: string; name: string; sortOrder: number }>>([]);
  const [templateRows, setTemplateRows] = useState<
    Array<{ id: string; templateKey: string; name: string; source: string; status: string }>
  >([]);
  const [selectedTemplateKeys, setSelectedTemplateKeys] = useState<string[]>([]);
  const [selectedDefaultTemplateKey, setSelectedDefaultTemplateKey] = useState("");
  const [selectedGroupId, setSelectedGroupId] = useState("");
  const systemPage = useResourcePages(
    "/api/systems",
    {
      q: query,
      group: group === "全部分组" ? "" : group,
      scope: scope === "已有连接" ? "connected" : scope === "自建系统" ? "custom" : "",
    },
    demo.authenticated,
  );
  const systems: DemoSystem[] = systemPage.items.map((item) => ({
    id: item.id,
    service: item.systemKey,
    name: item.name,
    source: item.source,
    groupId: item.groupId,
    letter: item.name.slice(0, 2).toUpperCase(),
    category: item.groupName || "未分组",
    actions: item.actionCount,
    executable: item.executableCount,
    connections: item.connectionCount,
    description: systemDescriptionForDisplay(item.description, item.source),
    authTemplateIds: (item.authTemplateIds ?? []).map(
      (id: string) => templateRows.find((template) => template.id === id)?.templateKey ?? id,
    ),
    defaultAuthTemplateId:
      templateRows.find((template) => template.id === item.defaultAuthTemplateId)?.templateKey ??
      item.defaultAuthTemplateId ??
      "",
  }));
  function loadCatalogMetadata() {
    return Promise.all([api.systemGroups(), api.authTemplates()]).then(([groups, templates]) => {
      setGroupRows(groups);
      setTemplateRows(templates);
    });
  }
  useEffect(() => {
    if (!demo.authenticated) return;
    void loadCatalogMetadata().catch((error) => demo.notify(error));
  }, [demo.authenticated, demo.systemGroups.length]);
  const filtered = systems;
  const selectableTemplates = templateRows.filter(
    (template) =>
      template.status === "published" &&
      (template.source === "custom" || availableAuthMethods.some((method) => method.id === template.templateKey)),
  );
  const editTemplates = templateRows.filter(
    (template) => selectableTemplates.includes(template) || selectedTemplateKeys.includes(template.templateKey),
  );
  const originalDefaultTemplateKey = selected?.defaultAuthTemplateId || selected?.authTemplateIds[0] || "";
  const authSelectionChanged = Boolean(
    selected &&
    (selectedDefaultTemplateKey !== originalDefaultTemplateKey ||
      selectedTemplateKeys.length !== selected.authTemplateIds.length ||
      selectedTemplateKeys.some((key) => !selected.authTemplateIds.includes(key))),
  );
  const systemGroupValue = demo.systemGroups.includes(systemGroup) ? systemGroup : (demo.systemGroups[0] ?? "");
  async function addSystem(event: FormEvent) {
    event.preventDefault();
    const service = systemId.trim();
    if (!(await demo.addSystem(systemName, service, systemGroupValue, systemTemplateKeys, systemDefaultTemplateKey)))
      return;
    setSystemOpen(false);
    navigate(`/auth?section=instances&create=1&system=${encodeURIComponent(service)}`);
  }
  function saveSystemSettings() {
    if (!selected?.id || !selectedGroupId || !selectedTemplateKeys.includes(selectedDefaultTemplateKey)) return;
    const changes: Promise<unknown>[] = [];
    if (selectedGroupId !== (selected.groupId ?? "")) {
      changes.push(api.updateSystem(selected.id, { groupId: selectedGroupId }));
    }
    if (authSelectionChanged) {
      const templateIds = selectedTemplateKeys
        .map((key) => templateRows.find((item) => item.templateKey === key)?.id)
        .filter((id): id is string => Boolean(id));
      const defaultTemplateId = templateRows.find((item) => item.templateKey === selectedDefaultTemplateKey)?.id;
      if (!defaultTemplateId || templateIds.length !== selectedTemplateKeys.length) {
        demo.notify("认证模板尚未加载完成，请稍后重试");
        return;
      }
      changes.push(api.setSystemAuthTemplates(selected.id, templateIds, defaultTemplateId));
    }
    if (!changes.length) return;
    return Promise.all(changes)
      .then(() => demo.reload())
      .then(() => {
        setSelected(null);
        demo.notify(`${selected.name} 的系统设置已更新`);
      })
      .catch((error) => demo.notify(error));
  }
  return (
    <div className="grid gap-6">
      <PageControls
        page={systemPage}
        onClear={() => {
          setQuery("");
          setScope("全部系统");
          setGroup("全部分组");
        }}
      />
      <div className="flex min-w-0 flex-col gap-3 xl:flex-row">
        <SearchBox value={query} onChange={setQuery} placeholder="搜索系统、标识、认证方式或分类" />
        <Tabs value={scope} onChange={setScope} items={["全部系统", "已有连接", "自建系统"]} />
        <select className={`${fieldClass} xl:w-44`} value={group} onChange={(event) => setGroup(event.target.value)}>
          <option>全部分组</option>
          {demo.systemGroups.map((item) => (
            <option key={item}>{item}</option>
          ))}
        </select>
        <Button className="shrink-0" variant="secondary" onClick={() => setGroupsOpen(true)}>
          管理分组
        </Button>
        <Button write className="shrink-0" onClick={() => setSystemOpen(true)}>
          <Plus className="size-4" />
          添加系统
        </Button>
      </div>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {filtered.map((provider) => (
          <button
            key={provider.service}
            className="text-left"
            onClick={() => {
              setSelected(provider);
              setSelectedTemplateKeys(provider.authTemplateIds);
              setSelectedDefaultTemplateKey(provider.defaultAuthTemplateId || provider.authTemplateIds[0] || "");
              setSelectedGroupId(provider.groupId ?? "");
            }}
          >
            <Card className="h-full p-5 transition hover:-translate-y-0.5 hover:border-blue-300 hover:shadow-md">
              <div className="flex items-start justify-between">
                <Logo letters={provider.letter} />
                {provider.connections ? (
                  <Badge tone="success">{provider.connections} 已连接</Badge>
                ) : (
                  <Badge>未连接</Badge>
                )}
              </div>
              <h2 translate="no" className="mt-4 font-bold">
                {provider.name}
              </h2>
              <p className="font-mono text-xs text-[var(--muted-text)]">{provider.service}</p>
              <p translate="no" className="mt-3 min-h-10 text-sm leading-5 text-[var(--muted-text)]">
                {provider.source === "custom" && provider.description === DEFAULT_CUSTOM_SYSTEM_DESCRIPTION
                  ? t(provider.description)
                  : provider.description}
              </p>
              <div className="mt-4 flex flex-wrap gap-1.5">
                {authInstancesFor(provider, demo.authInstances).map((instance) => (
                  <Badge key={instance.id}>{instance.name}</Badge>
                ))}
                {!authInstancesFor(provider, demo.authInstances).length && <Badge>未绑定认证实例</Badge>}
              </div>
              <div className="mt-4 flex justify-between border-t border-[var(--border)] pt-4 text-xs text-[var(--muted-text)]">
                <span>{provider.category}</span>
                <span>
                  {provider.executable}/{provider.actions} 可执行
                </span>
              </div>
            </Card>
          </button>
        ))}
      </div>
      <Modal
        open={Boolean(selected)}
        onOpenChange={(open) => !open && setSelected(null)}
        className="w-[min(94vw,56rem)]"
        unsavedChanges={authSelectionChanged || Boolean(selected && selectedGroupId !== (selected.groupId ?? ""))}
        title={selected?.name ?? "系统"}
        description={selected?.description ?? ""}
      >
        {selected && (
          <div className="grid gap-5">
            <div className="grid grid-cols-3 gap-3">
              <MiniStat label="操作" value={selected.actions} />
              <MiniStat label="可执行" value={selected.executable} />
              <MiniStat label="账号" value={selected.connections} />
            </div>
            <Tabs value={detailTab} onChange={setDetailTab} items={["API", "集成", "系统设置"]} />
            {detailTab === "API" && (
              <div className="grid gap-3">
                <div className="flex flex-wrap items-center gap-2">
                  <div className="min-w-0 flex-1 basis-48">
                    <SearchBox value={apiQuery} onChange={setApiQuery} placeholder="搜索 API 名称、功能或路径" />
                  </div>
                  <select
                    aria-label="API 定义状态"
                    className={`${fieldClass} !w-28 shrink-0`}
                    value={apiStatus}
                    onChange={(event) => setApiStatus(event.target.value)}
                  >
                    <option value="">全部状态</option>
                    <option value="active">已启用</option>
                    <option value="draft">草稿</option>
                    <option value="disabled">停用</option>
                  </select>
                  <Button asChild write>
                    <Link to={`/actions?new=1&system=${encodeURIComponent(selected.service)}`}>添加 API</Link>
                  </Button>
                </div>
                <div className="overflow-hidden rounded-lg border border-[var(--border)]">
                  <div
                    aria-hidden="true"
                    className="hidden grid-cols-[minmax(0,1fr)_minmax(0,1.5fr)_minmax(0,1fr)] gap-4 bg-[var(--muted)] px-3 py-2 text-xs text-[var(--muted-text)] md:grid"
                  >
                    <span>API 功能</span>
                    <span>功能说明</span>
                    <span>接口</span>
                  </div>
                  {filteredApis.map((row) => (
                    <Link
                      key={row.id}
                      className="grid gap-1 border-t border-[var(--border)] px-3 py-2.5 text-sm first:border-t-0 hover:bg-[var(--muted)] focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:-outline-offset-2 md:grid-cols-[minmax(0,1fr)_minmax(0,1.5fr)_minmax(0,1fr)] md:items-center md:gap-4"
                      to={`/actions?api=${encodeURIComponent(row.id)}&system=${encodeURIComponent(selected.service)}`}
                    >
                      <span className="min-w-0 break-words font-semibold">{row.name || row.actionKey}</span>
                      <span className="min-w-0 break-words text-[var(--muted-text)]">
                        {row.description?.trim() || "暂无功能说明"}
                      </span>
                      <code className="hidden min-w-0 break-all text-xs text-[var(--muted-text)] md:block">
                        {row.httpMethod} {row.relativePath}
                      </code>
                    </Link>
                  ))}
                  {(apisLoading || apisError || !filteredApis.length) && (
                    <p role={apisError ? "alert" : "status"} className="p-3 text-sm text-[var(--muted-text)]">
                      {apisLoading
                        ? "正在加载 API…"
                        : apisError
                          ? "API 加载失败，请关闭后重试。"
                          : systemApis.length
                            ? "没有符合筛选条件的 API。"
                            : "暂无 API 定义，可以先添加接口再配置访问环境。"}
                    </p>
                  )}
                </div>
              </div>
            )}
            {detailTab === "集成" && (
              <div className="grid gap-3">
                <Button asChild write className="w-fit">
                  <Link to={`/integrations?new=1&system=${encodeURIComponent(selected.service)}`}>创建集成配置</Link>
                </Button>
                <div className="divide-y divide-[var(--border)] overflow-hidden rounded-lg border border-[var(--border)]">
                  {demo.integrations
                    .filter((i) => i.provider === selected.service)
                    .map((i) => {
                      const instance = demo.authInstances.find((item) => item.id === i.authInstanceId);
                      const accounts = demo.connections.filter((item) => item.integration === i.name);
                      const activeAccounts = accounts.filter((item) => item.status === "active").length;
                      const environment = i.settings?.environment;
                      return (
                        <Link
                          key={i.id}
                          className="grid gap-2 px-3 py-3 text-sm hover:bg-[var(--muted)] focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:-outline-offset-2"
                          to={`/integrations?integration=${encodeURIComponent(i.id)}`}
                        >
                          <div className="flex flex-wrap items-center gap-2">
                            <strong className="min-w-0 break-all">{i.name}</strong>
                            <Badge>
                              {environment === "production"
                                ? "生产"
                                : environment === "test"
                                  ? "测试"
                                  : environment === "custom"
                                    ? "自定义环境"
                                    : String(environment || "自定义环境")}
                            </Badge>
                            <Badge
                              tone={i.status === "ready" ? "success" : i.status === "draft" ? "warning" : "neutral"}
                            >
                              {statusLabel(i.status)}
                            </Badge>
                          </div>
                          <div className="grid min-w-0 gap-x-4 gap-y-1 text-xs text-[var(--muted-text)] md:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)_auto]">
                            <span className="min-w-0 break-all">
                              <span className="font-medium">地址：</span>
                              {i.baseUrl || "未配置"}
                            </span>
                            <span className="min-w-0 break-words">
                              <span className="font-medium">认证：</span>
                              {i.authInstanceId ? authInstanceName(i.authInstanceId, demo.authInstances) : "未绑定"}
                              {instance && (
                                <>
                                  {" "}
                                  · {authTemplateName(instance.templateId, demo.authSchemes)} ·{" "}
                                  {statusLabel(instance.status)}
                                </>
                              )}
                            </span>
                            <span>
                              账号：{accounts.length} 个 · {activeAccounts} 个正常
                            </span>
                          </div>
                        </Link>
                      );
                    })}
                  {!demo.integrations.some((i) => i.provider === selected.service) && (
                    <p className="p-3 text-sm text-[var(--muted-text)]">
                      暂无集成配置。创建集成后，可在这里查看访问地址、环境、认证实例和账号情况。
                    </p>
                  )}
                </div>
              </div>
            )}
            {detailTab === "系统设置" && (
              <>
                <Field label="系统分组" hint="仅用于目录筛选和管理，不改变运行时权限。">
                  <select
                    className={fieldClass}
                    disabled={!canWrite}
                    value={selectedGroupId}
                    onChange={(event) => setSelectedGroupId(event.target.value)}
                  >
                    {groupRows.map((item) => (
                      <option key={item.id} value={item.id}>
                        {item.name}
                      </option>
                    ))}
                  </select>
                </Field>
                <div>
                  <p className="mb-2 text-sm font-bold">已绑定认证实例</p>
                  <div className="flex flex-wrap gap-2">
                    {demo.authInstances
                      .filter((instance) => instance.systemIds.includes(selected.service))
                      .map((instance) => (
                        <Badge tone={instance.status === "ready" ? "success" : "warning"} key={instance.id}>
                          {instance.name}
                        </Badge>
                      ))}
                  </div>
                </div>
                <SystemAuthTemplatePicker
                  name="edit-system-default-template"
                  templates={editTemplates}
                  selectedKeys={selectedTemplateKeys}
                  defaultKey={selectedDefaultTemplateKey}
                  inUseKeys={demo.authInstances
                    .filter((instance) => instance.systemIds.includes(selected.service))
                    .map((instance) => instance.templateId)}
                  disabled={!canWrite}
                  onChange={(keys, defaultKey) => {
                    setSelectedTemplateKeys(keys);
                    setSelectedDefaultTemplateKey(defaultKey);
                  }}
                />
                <div className="rounded-xl border border-[var(--border)] p-4">
                  <p className="text-sm font-bold">配置认证实例</p>
                  <p className="mt-1 text-xs text-[var(--muted-text)]">
                    认证实例属于具体系统，不能复用其他系统的端点或应用凭据。
                  </p>
                  <Link
                    className="mt-3 inline-flex items-center gap-2 text-sm font-semibold text-blue-600 hover:underline"
                    to={`/auth?section=instances&system=${selected.service}`}
                  >
                    <Plus className="size-4" /> 为该系统添加认证实例
                  </Link>
                </div>
                <div className="rounded-xl border border-[var(--border)] p-4">
                  <p className="text-sm font-bold">API 操作</p>
                  <p className="mt-1 text-xs text-[var(--muted-text)]">
                    当前数据库中有 {selected.actions} 个定义，其中 {selected.executable} 个可执行。
                  </p>
                  <Link
                    className="mt-3 inline-flex items-center gap-2 text-sm font-semibold text-blue-600 hover:underline"
                    to={`/actions?system=${encodeURIComponent(selected.service)}`}
                  >
                    查看该系统的真实操作
                    <ArrowRight className="size-4" />
                  </Link>
                </div>
                <div className="flex flex-wrap justify-end gap-2">
                  <Button
                    write
                    variant="secondary"
                    disabled={
                      !selectedGroupId ||
                      !selectedTemplateKeys.includes(selectedDefaultTemplateKey) ||
                      (authSelectionChanged &&
                        !templateRows.some((template) => template.templateKey === selectedDefaultTemplateKey)) ||
                      (!authSelectionChanged && selectedGroupId === (selected.groupId ?? ""))
                    }
                    onClick={saveSystemSettings}
                  >
                    保存系统设置
                  </Button>
                  <Button asChild variant="secondary">
                    <Link to={`/integrations?new=1&provider=${selected.service}`}>创建集成配置</Link>
                  </Button>
                  <Button asChild write>
                    <Link to={`/auth?section=instances&create=1&system=${encodeURIComponent(selected.service)}`}>
                      <Plus className="size-4" /> 接入此系统
                    </Link>
                  </Button>
                </div>
              </>
            )}
          </div>
        )}
      </Modal>
      <Modal
        open={systemOpen}
        onOpenChange={setSystemOpen}
        title="添加系统"
        description="只登记可复用的系统定义；API 地址和认证实例在集成配置中选择。"
      >
        <MutationForm className="grid gap-4" onSubmit={addSystem}>
          <Field label="系统名称">
            <input className={fieldClass} value={systemName} onChange={(event) => setSystemName(event.target.value)} />
          </Field>
          <Field label="系统标识" hint="用于 API 和配置引用，建议使用小写字母和连字符。">
            <input className={fieldClass} value={systemId} onChange={(event) => setSystemId(event.target.value)} />
          </Field>
          <Field
            label="系统分组"
            hint={!demo.systemGroups.length ? "暂无系统分组，请先在“管理分组”中创建。" : undefined}
          >
            <select
              className={fieldClass}
              value={systemGroupValue}
              disabled={!demo.systemGroups.length}
              onChange={(event) => setSystemGroup(event.target.value)}
            >
              {demo.systemGroups.map((item) => (
                <option key={item}>{item}</option>
              ))}
            </select>
          </Field>
          <SystemAuthTemplatePicker
            name="create-system-default-template"
            templates={selectableTemplates}
            selectedKeys={systemTemplateKeys}
            defaultKey={systemDefaultTemplateKey}
            onChange={(keys, defaultKey) => {
              setSystemTemplateKeys(keys);
              setSystemDefaultTemplateKey(defaultKey);
            }}
          />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => setSystemOpen(false)}>
              取消
            </Button>
            <Button
              write
              type="submit"
              disabled={
                !systemGroupValue ||
                !systemTemplateKeys.includes(systemDefaultTemplateKey) ||
                !selectableTemplates.some((template) => template.templateKey === systemDefaultTemplateKey)
              }
            >
              保存系统
            </Button>
          </div>
        </MutationForm>
      </Modal>
      <Modal
        open={groupsOpen}
        onOpenChange={(open) => {
          setGroupsOpen(open);
          if (!open) setEditingGroupId("");
        }}
        title="系统分组"
        description="分组用于筛选和管理系统，不影响运行时权限。"
      >
        <div className="grid gap-4">
          <div className="grid gap-2">
            {groupRows.map((item) =>
              editingGroupId === item.id ? (
                <MutationForm
                  key={item.id}
                  className="grid gap-3 rounded-xl border border-blue-500/40 bg-[var(--muted)] p-3"
                  onSubmit={(event) => {
                    event.preventDefault();
                    const name = editingGroupName.trim();
                    if (!name || name === item.name) return;
                    return api
                      .updateSystemGroup(item.id, { name, sortOrder: item.sortOrder })
                      .then(() => {
                        if (group === item.name) setGroup(name);
                        if (systemGroup === item.name) setSystemGroup(name);
                        return Promise.all([loadCatalogMetadata(), demo.reload()]);
                      })
                      .then(() => {
                        setEditingGroupId("");
                        demo.notify("系统分组已更新");
                      })
                      .catch((error) => demo.notify(error));
                  }}
                >
                  <Field label="分组名称">
                    <input
                      autoFocus
                      required
                      className={fieldClass}
                      value={editingGroupName}
                      onChange={(event) => setEditingGroupName(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === "Escape") {
                          event.preventDefault();
                          event.stopPropagation();
                          setEditingGroupId("");
                        }
                      }}
                    />
                  </Field>
                  <div className="flex justify-end gap-2">
                    <Button type="button" variant="secondary" onClick={() => setEditingGroupId("")}>
                      取消
                    </Button>
                    <Button
                      write
                      type="submit"
                      disabled={!editingGroupName.trim() || editingGroupName.trim() === item.name}
                    >
                      保存
                    </Button>
                  </div>
                </MutationForm>
              ) : (
                <ListRow
                  key={item.id}
                  title={item.name}
                  meta={`${systems.filter((system) => system.category === item.name).length} 个系统`}
                  action={
                    <div className="flex gap-1">
                      <Button
                        write
                        variant="ghost"
                        disabled={Boolean(editingGroupId)}
                        onClick={() => {
                          setEditingGroupId(item.id);
                          setEditingGroupName(item.name);
                        }}
                      >
                        重命名
                      </Button>
                      <Button
                        write
                        variant="ghost"
                        disabled={systems.some((system) => system.category === item.name)}
                        title={
                          systems.some((system) => system.category === item.name)
                            ? "请先移走该分组下的系统"
                            : "删除分组"
                        }
                        onClick={() => {
                          if (!window.confirm(`确认删除系统分组“${item.name}”吗？`)) return;
                          return api
                            .deleteSystemGroup(item.id)
                            .then(() => Promise.all([loadCatalogMetadata(), demo.reload()]))
                            .then(() => demo.notify("系统分组已删除"))
                            .catch((error) => demo.notify(error));
                        }}
                      >
                        删除
                      </Button>
                    </div>
                  }
                />
              ),
            )}
          </div>
          <MutationForm
            className="flex gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              const name = newGroup.trim();
              if (!name) return;
              return api
                .saveSystemGroup({ name, sortOrder: groupRows.length * 10 })
                .then(() => Promise.all([loadCatalogMetadata(), demo.reload()]))
                .then(() => {
                  setSystemGroup(name);
                  setNewGroup("");
                  demo.notify(`系统分组 ${name} 已添加`);
                })
                .catch((error) => demo.notify(error));
            }}
          >
            <input
              aria-label="新分组名称"
              disabled={!canWrite}
              className={fieldClass}
              value={newGroup}
              onChange={(event) => setNewGroup(event.target.value)}
              placeholder="例如：财务系统"
            />
            <Button write type="submit" disabled={!newGroup.trim()}>
              <Plus className="size-4" /> 添加分组
            </Button>
          </MutationForm>
        </div>
      </Modal>
    </div>
  );
}
