import { useCanWrite } from "../permissions";
import { useResourcePages, PageControls } from "../pagination";

import { ArrowRight, Plus } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router";
import { useDemo, type DemoSystem } from "../demo";
import { Badge, Button, Card, Field, Modal, PageHeader, fieldClass } from "../ui";
import { api } from "../api";

import {
  authInstancesFor,
  authMethodCatalog,
  BuildFlow,
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
        description="统一管理内置与自定义系统、系统分组，以及系统可使用的认证实例。"
        actions={<Badge tone="info">系统分组 · 认证模板 · 认证实例</Badge>}
      />
      <BuildFlow current="systems" />
      <SystemDirectoryPage />
    </div>
  );
}

function SystemDirectoryPage() {
  const demo = useDemo();
  const canWrite = useCanWrite();
  const [query, setQuery] = useState("");
  const [scope, setScope] = useState("全部系统");
  const [group, setGroup] = useState("全部分组");
  const [selected, setSelected] = useState<DemoSystem | null>(null);
  const [systemOpen, setSystemOpen] = useState(false);
  const [groupsOpen, setGroupsOpen] = useState(false);
  const [newGroup, setNewGroup] = useState("");
  const [systemName, setSystemName] = useState("企业 ERP");
  const [systemId, setSystemId] = useState("internal-erp");
  const [systemGroup, setSystemGroup] = useState("企业内部");
  const [systemAuthTemplate, setSystemAuthTemplate] = useState("username-password-token");
  const [groupRows, setGroupRows] = useState<Array<{ id: string; name: string; sortOrder: number }>>([]);
  const [templateRows, setTemplateRows] = useState<
    Array<{ id: string; templateKey: string; name: string; status: string }>
  >([]);
  const [selectedTemplateKeys, setSelectedTemplateKeys] = useState<string[]>([]);
  const [selectedGroupId, setSelectedGroupId] = useState("");
  const systemPage = useResourcePages(
    "/api/systems",
    {
      q: query,
      group: group === "全部分组" ? "" : group,
      scope: scope === "全部系统" ? "" : scope === "已有连接" ? "connected" : "custom",
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
    description: item.description,
    authTemplateIds: (item.authTemplateIds ?? []).map(
      (id: string) => templateRows.find((template) => template.id === id)?.templateKey ?? id,
    ),
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
  async function addSystem(event: FormEvent) {
    event.preventDefault();
    const service = systemId.trim();
    if (!(await demo.addSystem(systemName, service, systemGroup, systemAuthTemplate))) return;
    setSystemOpen(false);
  }
  function saveSystemSettings() {
    if (!selected?.id || !selectedGroupId) return;
    const templateIds = selectedTemplateKeys
      .map((key) => templateRows.find((item) => item.templateKey === key)?.id)
      .filter((id): id is string => Boolean(id));
    void Promise.all([
      api.updateSystem(selected.id, { groupId: selectedGroupId }),
      api.setSystemAuthTemplates(selected.id, templateIds),
    ])
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
      <div className="flex flex-col gap-3 xl:flex-row">
        <SearchBox value={query} onChange={setQuery} placeholder="搜索系统、标识、认证方式或分类" />
        <Tabs value={scope} onChange={setScope} items={["全部系统", "已有连接", "企业内部"]} />
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
          添加企业系统
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
                {provider.description}
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
        title={selected?.name ?? "系统"}
        description={selected?.description ?? ""}
      >
        {selected && (
          <div className="grid gap-5">
            <div className="grid grid-cols-3 gap-3">
              <MiniStat label="操作" value={selected.actions} />
              <MiniStat label="可执行" value={selected.executable} />
              <MiniStat label="连接账号" value={selected.connections} />
            </div>
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
            <div>
              <p className="mb-2 text-sm font-bold">支持的认证模板</p>
              <div className="grid gap-2 rounded-xl border border-[var(--border)] p-3 sm:grid-cols-2">
                {templateRows
                  .filter((template) => template.status === "published")
                  .map((template) => {
                    const inUse = demo.authInstances.some(
                      (instance) =>
                        instance.systemIds.includes(selected.service) && instance.templateId === template.templateKey,
                    );
                    return (
                      <label key={template.id} className="flex items-center gap-2 text-sm">
                        <input
                          type="checkbox"
                          checked={selectedTemplateKeys.includes(template.templateKey)}
                          disabled={inUse || !canWrite}
                          onChange={(event) =>
                            setSelectedTemplateKeys((items) =>
                              event.target.checked
                                ? [...new Set([...items, template.templateKey])]
                                : items.filter((item) => item !== template.templateKey),
                            )
                          }
                        />
                        <span>{template.name}</span>
                        {inUse && <Badge tone="info">实例使用中</Badge>}
                      </label>
                    );
                  })}
              </div>
              <p className="mt-2 text-xs text-[var(--muted-text)]">已有认证实例使用的模板不能移除。</p>
            </div>
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
            <div className="flex justify-end">
              <Button
                write
                variant="secondary"
                disabled={!selectedTemplateKeys.length || !selectedGroupId}
                onClick={saveSystemSettings}
              >
                保存系统设置
              </Button>
              <Button asChild>
                <Link to={`/integrations?new=1&provider=${selected.service}`}>创建集成配置</Link>
              </Button>
            </div>
          </div>
        )}
      </Modal>
      <Modal
        open={systemOpen}
        onOpenChange={setSystemOpen}
        title="添加企业内部系统"
        description="只登记可复用的系统定义；API 地址和认证实例在集成配置中选择。"
      >
        <form className="grid gap-4" onSubmit={addSystem}>
          <Field label="系统名称">
            <input className={fieldClass} value={systemName} onChange={(event) => setSystemName(event.target.value)} />
          </Field>
          <Field label="系统标识" hint="用于 API 和配置引用，建议使用小写字母和连字符。">
            <input className={fieldClass} value={systemId} onChange={(event) => setSystemId(event.target.value)} />
          </Field>
          <Field label="系统分组">
            <select className={fieldClass} value={systemGroup} onChange={(event) => setSystemGroup(event.target.value)}>
              {demo.systemGroups.map((item) => (
                <option key={item}>{item}</option>
              ))}
            </select>
          </Field>
          <Field label="默认认证模板" hint="保存系统后，可以在认证中心创建多个兼容实例。">
            <select
              className={fieldClass}
              value={systemAuthTemplate}
              onChange={(event) => setSystemAuthTemplate(event.target.value)}
            >
              <optgroup label="内置和企业模板">
                {authMethodCatalog.map((method) => (
                  <option key={method.id} value={method.id}>
                    {method.name}
                    {method.executable ? "" : "（需 Go 扩展）"}
                  </option>
                ))}
              </optgroup>
              <optgroup label="自定义模板">
                {demo.authSchemes
                  .filter((scheme) => scheme.status === "published")
                  .map((scheme) => (
                    <option key={scheme.id} value={scheme.id}>
                      {scheme.name}
                    </option>
                  ))}
              </optgroup>
            </select>
          </Field>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => setSystemOpen(false)}>
              取消
            </Button>
            <Button write type="submit">
              保存系统
            </Button>
          </div>
        </form>
      </Modal>
      <Modal
        open={groupsOpen}
        onOpenChange={setGroupsOpen}
        title="系统分组"
        description="分组用于筛选和管理系统，不影响运行时权限。"
      >
        <div className="grid gap-4">
          <div className="grid gap-2">
            {groupRows.map((item) => (
              <ListRow
                key={item.id}
                title={item.name}
                meta={`${systems.filter((system) => system.category === item.name).length} 个系统`}
                action={
                  <div className="flex gap-1">
                    <Button
                      variant="ghost"
                      onClick={() => {
                        const name = window.prompt("新的分组名称", item.name)?.trim();
                        if (!name || name === item.name) return;
                        void api
                          .updateSystemGroup(item.id, { name, sortOrder: item.sortOrder })
                          .then(() => Promise.all([loadCatalogMetadata(), demo.reload()]))
                          .then(() => demo.notify("系统分组已更新"))
                          .catch((error) => demo.notify(error));
                      }}
                    >
                      重命名
                    </Button>
                    <Button
                      write
                      variant="ghost"
                      disabled={systems.some((system) => system.category === item.name)}
                      title={
                        systems.some((system) => system.category === item.name) ? "请先移走该分组下的系统" : "删除分组"
                      }
                      onClick={() =>
                        window.confirm(`确认删除系统分组“${item.name}”吗？`) &&
                        void api
                          .deleteSystemGroup(item.id)
                          .then(() => Promise.all([loadCatalogMetadata(), demo.reload()]))
                          .then(() => demo.notify("系统分组已删除"))
                          .catch((error) => demo.notify(error))
                      }
                    >
                      删除
                    </Button>
                  </div>
                }
              />
            ))}
          </div>
          <form
            className="flex gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              const name = newGroup.trim();
              if (!canWrite || !name) return;
              void api
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
            <Button write disabled={!newGroup.trim()}>
              <Plus className="size-4" /> 添加分组
            </Button>
          </form>
        </div>
      </Modal>
    </div>
  );
}
