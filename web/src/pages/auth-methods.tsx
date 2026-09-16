import { KeyRound, Plus } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { useDemo, type DemoAuthInstance, type DemoAuthScheme } from "../demo";
import { Badge, Button, Card, Field, Modal, PageHeader, fieldClass } from "../ui";
import { api } from "../api";

import {
  isExecutableAuthTemplate,
  type AuthMethod,
  authMethodCatalog,
  authTemplateName,
  type CustomCredentialField,
  type CustomInjectionRule,
  BuildFlow,
  Tabs,
  KeyValues,
  Th,
  Td,
} from "./core-shared";
export function AuthMethodsPage() {
  const demo = useDemo();
  const [searchParams] = useSearchParams();
  const [section, setSection] = useState(
    searchParams.get("section") === "instances" || demo.authInstances.length > 0 ? "认证实例" : "内置方式",
  );
  const initialSection = useRef(false);
  const [nextSystem, setNextSystem] = useState("");
  useEffect(() => {
    if (!demo.loading && !initialSection.current) {
      initialSection.current = true;
      if (demo.authInstances.length && !searchParams.get("section")) setSection("认证实例");
    }
  }, [demo.loading, demo.authInstances, searchParams]);
  const [selected, setSelected] = useState<AuthMethod | null>(null);
  const [showSchemeEditor, setShowSchemeEditor] = useState(false);
  const [showInstanceEditor, setShowInstanceEditor] = useState(false);
  const [editingSchemeId, setEditingSchemeId] = useState<string | null>(null);
  const [editingInstanceId, setEditingInstanceId] = useState<string | null>(null);
  const [customId, setCustomId] = useState("enterprise-ticket-auth");
  const [customName, setCustomName] = useState("企业票据认证");
  const [flow, setFlow] = useState("静态凭据");
  const [credentialFields, setCredentialFields] = useState<CustomCredentialField[]>([
    { name: "api_token", label: "访问令牌", secret: true },
    { name: "tenant_id", label: "租户 ID", secret: false },
  ]);
  const [injectionRules, setInjectionRules] = useState<CustomInjectionRule[]>([
    { target: "请求头", name: "Authorization", template: "Bearer {{api_token}}" },
    { target: "请求头", name: "X-Tenant-ID", template: "{{tenant_id}}" },
  ]);
  const [instanceId, setInstanceId] = useState("auth-instance-1");
  const [instanceName, setInstanceName] = useState("新认证实例");
  const [instanceTemplateId, setInstanceTemplateId] = useState("oauth2");
  const [instanceStatus, setInstanceStatus] = useState<DemoAuthInstance["status"]>("ready");
  const [instanceSystemIds, setInstanceSystemIds] = useState<string[]>([searchParams.get("system") ?? "internal-api"]);
  const [instanceTokenEndpoint, setInstanceTokenEndpoint] = useState("");
  const [instanceRefreshEndpoint, setInstanceRefreshEndpoint] = useState("");
  const [instanceTokenPath, setInstanceTokenPath] = useState("$.access_token");
  const [instanceExpiryPath, setInstanceExpiryPath] = useState("$.expires_in");
  const [instanceHeaderName, setInstanceHeaderName] = useState("Authorization");
  const [instanceHeaderValue, setInstanceHeaderValue] = useState("Bearer {{access_token}}");
  const [instanceAuthorizationUrl, setInstanceAuthorizationUrl] = useState("");
  const [verificationPath, setVerificationPath] = useState("");
  const [instanceScopes, setInstanceScopes] = useState("");
  const [instanceOAuthClientId, setInstanceOAuthClientId] = useState("");
  const [instanceOAuthClientSecret, setInstanceOAuthClientSecret] = useState("");
  const visibleMethods = authMethodCatalog.filter((item) => item.group === section);
  const systems = demo.customSystems;
  const instanceScheme = demo.authSchemes.find((scheme) => scheme.id === instanceTemplateId);
  const instanceMethod = authMethodCatalog.find((method) => method.id === instanceTemplateId);
  const instanceUsesTokenEndpoint =
    instanceTemplateId === "oauth2" ||
    instanceTemplateId === "username-password-token" ||
    instanceTemplateId === "client-credentials" ||
    (instanceScheme != null && instanceScheme.flow !== "静态凭据");

  function newCustomScheme() {
    setShowSchemeEditor(true);
    setEditingSchemeId(null);
    setCustomId(`custom-auth-${demo.authSchemes.length + 1}`);
    setCustomName("新认证模板");
    setFlow("静态凭据");
    setCredentialFields([{ name: "api_token", label: "访问令牌", secret: true }]);
    setInjectionRules([{ target: "请求头", name: "Authorization", template: "Bearer {{api_token}}" }]);
  }

  function editCustomScheme(scheme: DemoAuthScheme) {
    setShowSchemeEditor(true);
    setEditingSchemeId(scheme.id);
    setCustomId(scheme.id);
    setCustomName(scheme.name);
    setFlow(scheme.flow);
    setCredentialFields(scheme.credentialFields.map((field) => ({ ...field })));
    setInjectionRules(scheme.injectionRules.map((rule) => ({ ...rule })));
  }

  async function saveCustomScheme() {
    const name = customName.trim();
    const id = customId.trim();
    if (!name || !id) return;
    const current = demo.authSchemes.find((scheme) => scheme.id === editingSchemeId);
    const saved = await demo.saveAuthScheme({
      id,
      name,
      flow,
      status: current?.status ?? "draft",
      credentialFields,
      injectionRules,
      tokenEndpoint: "",
      refreshEndpoint: "",
      tokenPath: "",
      expiryPath: "",
      updatedAt: "刚刚",
    });
    if (!saved) return;
    setEditingSchemeId(id);
    setShowSchemeEditor(false);
  }

  function selectInstanceTemplate(templateId: string) {
    const scheme = demo.authSchemes.find((item) => item.id === templateId);
    const headerInjection = scheme?.injectionRules.find((rule) => rule.target === "请求头");
    setInstanceTemplateId(templateId);
    setInstanceTokenEndpoint(templateId === "username-password-token" ? "https://erp.corp.example/api/login" : "");
    setInstanceRefreshEndpoint("");
    setInstanceTokenPath(
      templateId === "oauth2" || templateId === "client-credentials"
        ? "$.access_token"
        : templateId === "username-password-token"
          ? "$.data.token"
          : scheme && scheme.flow !== "静态凭据"
            ? "$.data.access_token"
            : "",
    );
    setInstanceExpiryPath(
      templateId === "oauth2" || templateId === "client-credentials"
        ? "$.expires_in"
        : templateId === "username-password-token"
          ? "$.data.expires_in"
          : scheme && scheme.flow !== "静态凭据"
            ? "$.data.expires_in"
            : "",
    );
    const builtinHeaderValues: Record<string, string> = {
      oauth2: "Bearer {{access_token}}",
      api_key: "Bearer {{apiKey}}",
      basic: "Basic {{basic_token}}",
      "username-password-token": "Bearer {{token}}",
      "client-credentials": "Bearer {{access_token}}",
    };
    setInstanceHeaderName(headerInjection?.name ?? (scheme || templateId === "no_auth" ? "" : "Authorization"));
    setInstanceHeaderValue(headerInjection?.template ?? builtinHeaderValues[templateId] ?? "");
    setInstanceSystemIds((items) =>
      items.filter((systemId) =>
        systems.some((system) => system.service === systemId && system.authTemplateIds.includes(templateId)),
      ),
    );
  }

  function newAuthInstance(templateId?: string) {
    const requestedSystem = systems.find((system) => system.service === searchParams.get("system"));
    const resolvedTemplateId = templateId ?? requestedSystem?.authTemplateIds[0] ?? "oauth2";
    if (!isExecutableAuthTemplate(resolvedTemplateId, demo.authSchemes)) {
      demo.notify("请先发布可执行的认证模板，再添加可用实例");
      return;
    }
    setSection("认证实例");
    setShowInstanceEditor(true);
    setEditingInstanceId(null);
    setInstanceId(`auth-instance-${demo.authInstances.length + 1}`);
    setInstanceName("新认证实例");
    setInstanceStatus("ready");
    setInstanceAuthorizationUrl("");
    setInstanceScopes("");
    setInstanceOAuthClientId("");
    setVerificationPath("");
    setInstanceOAuthClientSecret("");
    const compatibleRequestedSystem = requestedSystem?.authTemplateIds.includes(resolvedTemplateId)
      ? requestedSystem
      : undefined;
    setInstanceSystemIds(compatibleRequestedSystem ? [compatibleRequestedSystem.service] : []);
    selectInstanceTemplate(resolvedTemplateId);
  }

  function editAuthInstance(instance: DemoAuthInstance) {
    setShowInstanceEditor(true);
    setEditingInstanceId(instance.id);
    setInstanceId(instance.instanceKey ?? instance.id);
    setInstanceName(instance.name);
    setInstanceTemplateId(instance.templateId);
    setInstanceStatus(instance.status);
    setInstanceSystemIds([...instance.systemIds]);
    setInstanceTokenEndpoint(instance.tokenEndpoint);
    setInstanceRefreshEndpoint(instance.refreshEndpoint);
    setInstanceTokenPath(instance.tokenPath);
    setInstanceExpiryPath(instance.expiryPath);
    setInstanceHeaderName(instance.headerName);
    setInstanceHeaderValue(instance.headerValueTemplate);
    setInstanceAuthorizationUrl(instance.authorizationUrl ?? "");
    setInstanceScopes((instance.scopes ?? []).join(" "));
    setVerificationPath(String(instance.publicConfig?.verificationPath ?? ""));
    setInstanceOAuthClientId("");
    setInstanceOAuthClientSecret("");
  }

  async function saveAuthInstance() {
    const id = instanceId.trim();
    const name = instanceName.trim();
    if (!id || !name || !instanceTemplateId || !instanceSystemIds[0]) {
      demo.notify("请填写实例名称、标识并选择所属系统");
      return;
    }
    if (instanceStatus === "ready" && !isExecutableAuthTemplate(instanceTemplateId, demo.authSchemes)) {
      demo.notify("只有已发布且后端支持的认证模板才能创建可用实例");
      return;
    }
    if (
      instanceTemplateId === "oauth2" &&
      (!instanceTokenEndpoint || !instanceAuthorizationUrl || (!editingInstanceId && !instanceOAuthClientId))
    ) {
      demo.notify("OAuth 2.0 实例需要授权地址、Token 地址和客户端 ID");
      return;
    }
    const saved = await demo.saveAuthInstance({
      id: editingInstanceId ?? "",
      instanceKey: id,
      name,
      templateId: instanceTemplateId,
      systemIds: instanceSystemIds,
      status: instanceStatus,
      tokenEndpoint: instanceTokenEndpoint,
      refreshEndpoint: instanceRefreshEndpoint,
      tokenPath: instanceTokenPath,
      expiryPath: instanceExpiryPath,
      headerName: instanceHeaderName,
      headerValueTemplate: instanceHeaderValue,
      publicConfig: { verificationPath },
      authorizationUrl: instanceAuthorizationUrl,
      scopes: instanceScopes.split(/[ ,\n]+/).filter(Boolean),
      oauthClientId: instanceOAuthClientId,
      oauthClientSecret: instanceOAuthClientSecret,
      updatedAt: "刚刚",
    });
    if (saved) {
      setShowInstanceEditor(false);
      const target = searchParams.get("returnTo");
      if (target && target.startsWith("/integrations?")) window.location.assign(target);
      else {
        setNextSystem(instanceSystemIds[0]);
        demo.notify("认证实例已保存，可继续创建该系统的集成配置");
      }
    }
  }

  function testAuthInstance(id: string) {
    void api
      .testAuthInstance(id)
      .then((result) => demo.notify(result.message ?? "认证实例配置有效"))
      .then(() => demo.reload())
      .catch((error) => demo.notify(error));
  }

  return (
    <div className="grid gap-6">
      <PageHeader
        title="认证中心"
        description="先用模板定义规则，再创建认证实例并绑定兼容系统；真实账号凭据按连接账号隔离保存。"
        actions={<Badge tone="info">认证模板 · 认证实例</Badge>}
      />
      <BuildFlow current="auth" />
      {nextSystem && (
        <Card className="p-4">
          <p className="mb-2 text-sm">认证实例已保存，继续配置该系统的 API 地址。</p>
          <Button asChild>
            <Link to={`/integrations?new=1&system=${encodeURIComponent(nextSystem)}`}>继续创建集成</Link>
          </Button>
        </Card>
      )}

      <Card className="grid gap-4 p-5 lg:grid-cols-[1fr_auto] lg:items-center">
        <div>
          <p className="font-bold">授权能力分层</p>
          <p className="mt-1 text-sm leading-6 text-[var(--muted-text)]">
            常见协议由平台内置；企业身份通过标准预设或内部网关接入；特殊字段和令牌交换使用声明式规则。
          </p>
        </div>
        <div className="flex gap-2">
          <Badge tone="success">6 种可直接执行</Badge>
          <Badge tone="warning">7 种需扩展</Badge>
        </div>
      </Card>
      <Tabs value={section} onChange={setSection} items={["内置方式", "企业认证", "自定义模板", "认证实例"]} />
      {(section === "内置方式" || section === "企业认证") && (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {visibleMethods.map((method) => (
            <button key={method.id} onClick={() => setSelected(method)} className="text-left">
              <Card className="h-full p-5 transition hover:-translate-y-0.5 hover:border-blue-300 hover:shadow-md">
                <div className="flex items-start justify-between gap-3">
                  <span className="grid size-10 place-items-center rounded-xl bg-blue-50 text-blue-600 dark:bg-blue-950 dark:text-blue-300">
                    <KeyRound className="size-5" />
                  </span>
                  <Badge tone={method.executable ? "success" : "warning"}>
                    {method.executable ? "可直接使用" : "需 Go 扩展或网关"}
                  </Badge>
                </div>
                <h2 className="mt-4 font-bold">{method.name}</h2>
                <p className="mt-2 min-h-12 text-sm leading-6 text-[var(--muted-text)]">{method.summary}</p>
                <p className="mt-4 border-t border-[var(--border)] pt-4 text-xs leading-5 text-[var(--muted-text)]">
                  {method.lifecycle}
                </p>
              </Card>
            </button>
          ))}
        </div>
      )}
      {section === "自定义模板" && (
        <>
          {!showSchemeEditor && (
            <Card className="overflow-hidden">
              <div className="flex flex-col justify-between gap-3 border-b border-[var(--border)] p-5 sm:flex-row sm:items-center">
                <div>
                  <h2 className="font-bold">自定义认证模板</h2>
                  <p className="mt-1 text-sm text-[var(--muted-text)]">
                    模板只定义字段和执行规则；保存后请基于模板创建认证实例。
                  </p>
                </div>
                <Button write onClick={newCustomScheme}>
                  <Plus className="size-4" /> 新建认证模板
                </Button>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full min-w-[980px] text-left text-sm">
                  <thead className="bg-[var(--muted)] text-xs text-[var(--muted-text)]">
                    <tr>
                      <Th>模板名称</Th>
                      <Th>获取方式</Th>
                      <Th>注入位置</Th>
                      <Th>实例数量</Th>
                      <Th>状态</Th>
                      <Th>更新时间</Th>
                      <Th />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[var(--border)]">
                    {demo.authSchemes.map((scheme) => (
                      <tr key={scheme.id}>
                        <Td>
                          <p translate="no" className="font-semibold">
                            {scheme.name}
                          </p>
                          <code className="text-xs text-[var(--muted-text)]">{scheme.id}</code>
                        </Td>
                        <Td>{scheme.flow}</Td>
                        <Td>
                          {scheme.injectionRules[0]
                            ? `${scheme.injectionRules[0].target} · ${scheme.injectionRules[0].name}`
                            : "未配置"}
                        </Td>
                        <Td>{demo.authInstances.filter((instance) => instance.templateId === scheme.id).length}</Td>
                        <Td>
                          <Badge
                            tone={
                              scheme.status === "published"
                                ? "success"
                                : scheme.status === "draft"
                                  ? "warning"
                                  : "neutral"
                            }
                          >
                            {scheme.status === "published" ? "已发布" : scheme.status === "draft" ? "草稿" : "已停用"}
                          </Badge>
                        </Td>
                        <Td>{scheme.updatedAt}</Td>
                        <Td>
                          <div className="flex justify-end gap-1">
                            <Button write variant="ghost" onClick={() => editCustomScheme(scheme)}>
                              编辑
                            </Button>
                            <Button variant="ghost" onClick={() => demo.copyAuthScheme(scheme.id)}>
                              复制
                            </Button>
                            <Button
                              write
                              variant="ghost"
                              disabled={scheme.status !== "published"}
                              title={scheme.status === "published" ? "基于此模板添加实例" : "请先发布模板"}
                              onClick={() => newAuthInstance(scheme.id)}
                            >
                              添加实例
                            </Button>
                            <Button write variant="ghost" onClick={() => demo.toggleAuthScheme(scheme.id)}>
                              {scheme.status === "published" ? "停用" : "发布"}
                            </Button>
                            <Button
                              write
                              variant="ghost"
                              disabled={demo.authInstances.some((instance) => instance.templateId === scheme.id)}
                              title={
                                demo.authInstances.some((instance) => instance.templateId === scheme.id)
                                  ? "请先移除基于该模板的认证实例"
                                  : "删除模板"
                              }
                              onClick={() =>
                                window.confirm(`确认删除认证模板“${scheme.name}”吗？此操作无法撤销。`) &&
                                demo.deleteAuthScheme(scheme.id)
                              }
                            >
                              删除
                            </Button>
                          </div>
                        </Td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          )}
          {showSchemeEditor && (
            <div className="grid gap-5 xl:grid-cols-[1.35fr_0.65fr]">
              <Card className="grid gap-6 p-5">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <h2 className="font-bold">{editingSchemeId ? `编辑 ${customName}` : "新建认证模板"}</h2>
                    <p className="mt-1 text-sm leading-6 text-[var(--muted-text)]">
                      定义最终用户需要填写的凭据、如何换取令牌，以及请求发出前如何注入认证信息。
                    </p>
                  </div>
                  <Button variant="secondary" onClick={() => setShowSchemeEditor(false)}>
                    返回列表
                  </Button>
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="模板 ID" hint="发布后建议保持稳定。">
                    <input
                      className={fieldClass}
                      value={customId}
                      disabled={Boolean(editingSchemeId)}
                      onChange={(event) => setCustomId(event.target.value)}
                    />
                  </Field>
                  <Field label="方案名称">
                    <input
                      className={fieldClass}
                      value={customName}
                      onChange={(event) => setCustomName(event.target.value)}
                    />
                  </Field>
                  <Field label="凭据获取方式">
                    <select className={fieldClass} value={flow} onChange={(event) => setFlow(event.target.value)}>
                      <option>静态凭据</option>
                      <option>两步换取令牌</option>
                      <option>表单换取令牌</option>
                    </select>
                  </Field>
                </div>
                <div className="rounded-xl bg-blue-50 p-3 text-sm leading-6 text-blue-700 dark:bg-blue-950 dark:text-blue-300">
                  模板不直接绑定系统。请保存模板后创建认证实例，在实例中填写实际地址并选择适用系统。
                </div>
                <section className="grid gap-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <h3 className="text-sm font-bold">凭据字段</h3>
                      <p className="text-xs text-[var(--muted-text)]">决定连接账号界面向用户收集哪些值。</p>
                    </div>
                    <Button
                      write
                      type="button"
                      variant="secondary"
                      onClick={() =>
                        setCredentialFields((items) => [
                          ...items,
                          { name: `field_${items.length + 1}`, label: "新凭据字段", secret: true },
                        ])
                      }
                    >
                      <Plus className="size-4" /> 添加字段
                    </Button>
                  </div>
                  {credentialFields.map((field, index) => (
                    <div
                      className="grid gap-2 rounded-xl border border-[var(--border)] p-3 sm:grid-cols-[1fr_1fr_auto_auto]"
                      key={`${field.name}-${index}`}
                    >
                      <input
                        aria-label={`凭据字段 ${index + 1} 标识`}
                        className={fieldClass}
                        value={field.name}
                        onChange={(event) =>
                          setCredentialFields((items) =>
                            items.map((item, itemIndex) =>
                              itemIndex === index ? { ...item, name: event.target.value } : item,
                            ),
                          )
                        }
                      />
                      <input
                        aria-label={`凭据字段 ${index + 1} 标签`}
                        className={fieldClass}
                        value={field.label}
                        onChange={(event) =>
                          setCredentialFields((items) =>
                            items.map((item, itemIndex) =>
                              itemIndex === index ? { ...item, label: event.target.value } : item,
                            ),
                          )
                        }
                      />
                      <label className="flex items-center gap-2 whitespace-nowrap text-xs text-[var(--muted-text)]">
                        <input
                          type="checkbox"
                          checked={field.secret}
                          onChange={(event) =>
                            setCredentialFields((items) =>
                              items.map((item, itemIndex) =>
                                itemIndex === index ? { ...item, secret: event.target.checked } : item,
                              ),
                            )
                          }
                        />
                        敏感字段
                      </label>
                      <Button
                        write
                        type="button"
                        variant="ghost"
                        disabled={credentialFields.length === 1}
                        onClick={() =>
                          setCredentialFields((items) => items.filter((_, itemIndex) => itemIndex !== index))
                        }
                      >
                        删除
                      </Button>
                    </div>
                  ))}
                </section>
                <section className="grid gap-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <h3 className="text-sm font-bold">请求注入规则</h3>
                      <p className="text-xs text-[var(--muted-text)]">仅允许受控模板写入请求头、查询参数或 Cookie。</p>
                    </div>
                    <Button
                      write
                      type="button"
                      variant="secondary"
                      onClick={() =>
                        setInjectionRules((items) => [
                          ...items,
                          { target: "请求头", name: `X-Custom-${items.length + 1}`, template: "{{api_token}}" },
                        ])
                      }
                    >
                      <Plus className="size-4" /> 添加规则
                    </Button>
                  </div>
                  {injectionRules.map((rule, index) => (
                    <div
                      className="grid gap-2 rounded-xl border border-[var(--border)] p-3 md:grid-cols-[8rem_1fr_1.4fr_auto]"
                      key={`${rule.name}-${index}`}
                    >
                      <select
                        aria-label={`注入规则 ${index + 1} 位置`}
                        className={fieldClass}
                        value={rule.target}
                        onChange={(event) =>
                          setInjectionRules((items) =>
                            items.map((item, itemIndex) =>
                              itemIndex === index ? { ...item, target: event.target.value } : item,
                            ),
                          )
                        }
                      >
                        <option>请求头</option>
                        <option>查询参数</option>
                        <option>Cookie</option>
                      </select>
                      <input
                        aria-label={`注入规则 ${index + 1} 名称`}
                        className={fieldClass}
                        value={rule.name}
                        onChange={(event) =>
                          setInjectionRules((items) =>
                            items.map((item, itemIndex) =>
                              itemIndex === index ? { ...item, name: event.target.value } : item,
                            ),
                          )
                        }
                      />
                      <input
                        aria-label={`注入规则 ${index + 1} 模板`}
                        className={fieldClass}
                        value={rule.template}
                        onChange={(event) =>
                          setInjectionRules((items) =>
                            items.map((item, itemIndex) =>
                              itemIndex === index ? { ...item, template: event.target.value } : item,
                            ),
                          )
                        }
                      />
                      <Button
                        write
                        type="button"
                        variant="ghost"
                        disabled={injectionRules.length === 1}
                        onClick={() =>
                          setInjectionRules((items) => items.filter((_, itemIndex) => itemIndex !== index))
                        }
                      >
                        删除
                      </Button>
                    </div>
                  ))}
                </section>
                <div className="rounded-xl bg-amber-50 p-3 text-sm leading-6 text-amber-800 dark:bg-amber-950 dark:text-amber-200">
                  复杂加密算法、多阶段挑战或厂商 SDK 认证应实现为经过审核的 Go 认证扩展，不能在控制台执行任意脚本。
                </div>
                <div className="flex justify-end gap-2">
                  <Button variant="ghost" onClick={newCustomScheme}>
                    清空
                  </Button>
                  <Button write onClick={saveCustomScheme}>
                    {editingSchemeId ? "保存修改" : "保存为草稿"}
                  </Button>
                </div>
              </Card>
              <div className="grid content-start gap-4">
                <Card className="p-5">
                  <h2 className="font-bold">运行时执行顺序</h2>
                  <ol className="mt-4 grid gap-3 text-sm text-[var(--muted-text)]">
                    {["校验用户输入", "读取加密凭据", "换取或刷新令牌", "注入请求并脱敏日志"].map((item, index) => (
                      <li className="flex items-center gap-3" key={item}>
                        <span className="grid size-7 shrink-0 place-items-center rounded-full bg-blue-50 text-xs font-bold text-blue-700 dark:bg-blue-950 dark:text-blue-300">
                          {index + 1}
                        </span>
                        {item}
                      </li>
                    ))}
                  </ol>
                </Card>
                <Card className="p-5">
                  <h2 className="font-bold">模板概况</h2>
                  <div className="mt-3 grid gap-3 text-sm">
                    <KeyValues
                      items={[
                        ["模板总数", demo.authSchemes.length],
                        ["已发布", demo.authSchemes.filter((scheme) => scheme.status === "published").length],
                        ["草稿", demo.authSchemes.filter((scheme) => scheme.status === "draft").length],
                        ["已停用", demo.authSchemes.filter((scheme) => scheme.status === "disabled").length],
                      ]}
                    />
                  </div>
                </Card>
              </div>
            </div>
          )}
        </>
      )}
      {section === "认证实例" && (
        <>
          {!showInstanceEditor && (
            <Card className="overflow-hidden">
              <div className="flex flex-col justify-between gap-3 border-b border-[var(--border)] p-5 sm:flex-row sm:items-center">
                <div>
                  <h2 className="font-bold">认证实例</h2>
                  <p className="mt-1 text-sm text-[var(--muted-text)]">
                    实例基于模板保存实际认证端点和系统绑定，不保存最终用户密码。
                  </p>
                </div>
                <Button write onClick={() => newAuthInstance()}>
                  <Plus className="size-4" /> 添加认证实例
                </Button>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full min-w-[1080px] text-left text-sm">
                  <thead className="bg-[var(--muted)] text-xs text-[var(--muted-text)]">
                    <tr>
                      <Th>实例名称</Th>
                      <Th>认证模板</Th>
                      <Th>绑定系统</Th>
                      <Th>Token 配置</Th>
                      <Th>状态</Th>
                      <Th />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[var(--border)]">
                    {demo.authInstances.map((instance) => (
                      <tr key={instance.id}>
                        <Td>
                          <p translate="no" className="font-semibold">
                            {instance.name}
                          </p>
                          <code className="text-xs text-[var(--muted-text)]">
                            {instance.instanceKey ?? instance.id}
                          </code>
                        </Td>
                        <Td>{authTemplateName(instance.templateId, demo.authSchemes)}</Td>
                        <Td>
                          {systems
                            .filter((system) => instance.systemIds.includes(system.service))
                            .map((system) => system.name)
                            .join("、") || "未绑定"}
                        </Td>
                        <Td>
                          <p className="max-w-56 truncate">{instance.tokenEndpoint || "无需交换令牌"}</p>
                          <code className="text-xs text-[var(--muted-text)]">{instance.tokenPath || "—"}</code>
                        </Td>
                        <Td>
                          <Badge
                            tone={
                              instance.status === "ready"
                                ? "success"
                                : instance.status === "draft"
                                  ? "warning"
                                  : "neutral"
                            }
                          >
                            {instance.status === "ready" ? "可用" : instance.status === "draft" ? "草稿" : "已停用"}
                          </Badge>
                        </Td>
                        <Td>
                          <div className="flex justify-end gap-1">
                            <Button
                              aria-label={`测试 ${instance.name}`}
                              variant="ghost"
                              onClick={() => testAuthInstance(instance.id)}
                            >
                              测试
                            </Button>
                            <Button
                              write
                              aria-label={`编辑 ${instance.name}`}
                              variant="ghost"
                              onClick={() => editAuthInstance(instance)}
                            >
                              编辑
                            </Button>
                            <Button
                              write
                              aria-label={`${instance.status === "ready" ? "停用" : "启用"} ${instance.name}`}
                              variant="ghost"
                              onClick={() => demo.toggleAuthInstance(instance.id)}
                            >
                              {instance.status === "ready" ? "停用" : "启用"}
                            </Button>
                          </div>
                        </Td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          )}
          {showInstanceEditor && (
            <div className="grid gap-5 xl:grid-cols-[1.35fr_0.65fr]">
              <Card className="grid gap-6 p-5">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <h2 className="font-bold">{editingInstanceId ? `编辑 ${instanceName}` : "添加认证实例"}</h2>
                    <p className="mt-1 text-sm leading-6 text-[var(--muted-text)]">
                      选择模板，填写实际认证端点，再绑定与该模板兼容的系统。
                    </p>
                  </div>
                  <Button variant="secondary" onClick={() => setShowInstanceEditor(false)}>
                    返回实例列表
                  </Button>
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="实例 ID" hint="供系统、集成和 API 稳定引用。">
                    <input
                      className={fieldClass}
                      value={instanceId}
                      disabled={Boolean(editingInstanceId)}
                      onChange={(event) => setInstanceId(event.target.value)}
                    />
                  </Field>
                  <Field label="实例名称">
                    <input
                      className={fieldClass}
                      value={instanceName}
                      onChange={(event) => setInstanceName(event.target.value)}
                    />
                  </Field>
                  <Field label="认证模板">
                    <select
                      className={fieldClass}
                      value={instanceTemplateId}
                      onChange={(event) => selectInstanceTemplate(event.target.value)}
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
                        {demo.authSchemes.map((scheme) => (
                          <option key={scheme.id} value={scheme.id} disabled={scheme.status !== "published"}>
                            {scheme.name} · {scheme.status === "published" ? "已发布" : "未发布"}
                          </option>
                        ))}
                      </optgroup>
                    </select>
                  </Field>
                  <Field label="实例状态">
                    <select
                      className={fieldClass}
                      value={instanceStatus}
                      onChange={(event) => setInstanceStatus(event.target.value as DemoAuthInstance["status"])}
                    >
                      <option value="ready">可用</option>
                      <option value="draft">草稿</option>
                      <option value="disabled">已停用</option>
                    </select>
                  </Field>
                </div>
                <Field label="所属系统" hint="一个认证实例只保存一个目标系统的实际端点和应用配置。">
                  <select
                    className={fieldClass}
                    value={instanceSystemIds[0] ?? ""}
                    onChange={(event) => setInstanceSystemIds(event.target.value ? [event.target.value] : [])}
                    required
                  >
                    <option value="">选择系统</option>
                    {systems
                      .filter((system) => system.authTemplateIds.includes(instanceTemplateId))
                      .map((system) => (
                        <option key={system.service} value={system.service}>
                          {system.name} · {system.category}
                        </option>
                      ))}
                  </select>
                </Field>
                <section className="grid gap-4 sm:grid-cols-2">
                  {instanceTemplateId === "oauth2" && (
                    <>
                      <Field label="授权地址">
                        <input
                          className={fieldClass}
                          value={instanceAuthorizationUrl}
                          onChange={(event) => setInstanceAuthorizationUrl(event.target.value)}
                          placeholder="https://provider.example.com/oauth/authorize"
                        />
                      </Field>
                      <Field label="权限范围" hint="多个 Scope 使用空格分隔。">
                        <input
                          className={fieldClass}
                          value={instanceScopes}
                          onChange={(event) => setInstanceScopes(event.target.value)}
                          placeholder="openid profile api.read"
                        />
                      </Field>
                      <Field label="客户端 ID" hint={editingInstanceId ? "留空表示不修改" : undefined}>
                        <input
                          className={fieldClass}
                          value={instanceOAuthClientId}
                          onChange={(event) => setInstanceOAuthClientId(event.target.value)}
                        />
                      </Field>
                      <Field label="客户端密钥" hint="加密保存；编辑时留空表示不修改。">
                        <input
                          type="password"
                          className={fieldClass}
                          value={instanceOAuthClientSecret}
                          onChange={(event) => setInstanceOAuthClientSecret(event.target.value)}
                        />
                      </Field>
                    </>
                  )}
                  {instanceUsesTokenEndpoint && (
                    <>
                      <Field label="认证 / Token 地址" hint="该系统实例的登录或换取令牌地址。">
                        <input
                          className={fieldClass}
                          value={instanceTokenEndpoint}
                          onChange={(event) => setInstanceTokenEndpoint(event.target.value)}
                          placeholder="https://system.example.com/api/login"
                        />
                      </Field>
                      <Field label="刷新地址" hint="可选；OAuth 未填写时复用 Token 地址。">
                        <input
                          className={fieldClass}
                          value={instanceRefreshEndpoint}
                          onChange={(event) => setInstanceRefreshEndpoint(event.target.value)}
                        />
                      </Field>
                      <Field label="访问令牌 JSON 路径">
                        <input
                          className={fieldClass}
                          value={instanceTokenPath}
                          onChange={(event) => setInstanceTokenPath(event.target.value)}
                          placeholder="$.data.token"
                        />
                      </Field>
                      <Field label="过期时间 JSON 路径">
                        <input
                          className={fieldClass}
                          value={instanceExpiryPath}
                          onChange={(event) => setInstanceExpiryPath(event.target.value)}
                          placeholder="$.data.expires_in"
                        />
                      </Field>
                    </>
                  )}
                  <Field
                    label="凭据验证路径"
                    hint="可选。保存后使用 GET 请求验证，例如 /me；留空仅检查配置，不标记为已验证。"
                  >
                    <input
                      className={fieldClass}
                      value={verificationPath}
                      onChange={(event) => setVerificationPath(event.target.value)}
                      placeholder="/me"
                    />
                  </Field>
                  {instanceTemplateId !== "no_auth" && (
                    <>
                      <Field label="Header 名称">
                        <input
                          className={fieldClass}
                          value={instanceHeaderName}
                          onChange={(event) => setInstanceHeaderName(event.target.value)}
                          placeholder="Authorization"
                        />
                      </Field>
                      <Field label="Header 值模板">
                        <input
                          className={fieldClass}
                          value={instanceHeaderValue}
                          onChange={(event) => setInstanceHeaderValue(event.target.value)}
                          placeholder="Bearer {{token}}"
                        />
                      </Field>
                    </>
                  )}
                </section>
                <div className="flex justify-end gap-2">
                  {editingInstanceId && (
                    <Button write variant="secondary" onClick={() => testAuthInstance(editingInstanceId)}>
                      检查已保存配置
                    </Button>
                  )}
                  <Button write onClick={saveAuthInstance}>
                    {editingInstanceId ? "保存修改" : "保存认证实例"}
                  </Button>
                </div>
              </Card>
              <div className="grid content-start gap-4">
                <Card className="p-5">
                  <h2 className="font-bold">当前模板</h2>
                  <div className="mt-3">
                    <KeyValues
                      items={[
                        ["模板", instanceMethod?.name ?? instanceScheme?.name ?? "未选择"],
                        ["认证流程", instanceMethod?.mode ?? instanceScheme?.flow ?? "—"],
                        [
                          "凭据字段",
                          instanceMethod?.fields.join("、") ??
                            instanceScheme?.credentialFields.map((field) => field.label).join("、") ??
                            "—",
                        ],
                        [
                          "注入规则",
                          instanceScheme?.injectionRules.map((rule) => `${rule.target} ${rule.name}`).join("、") ??
                            "由内置模板管理",
                        ],
                      ]}
                    />
                  </div>
                </Card>
                <Card className="p-5">
                  <h2 className="font-bold">实例与连接的边界</h2>
                  <p className="mt-3 text-sm leading-6 text-[var(--muted-text)]">
                    实例保存认证地址、应用配置和解析规则；连接账号只保存某个用户、租户或服务账号的真实凭据。
                  </p>
                </Card>
              </div>
            </div>
          )}
        </>
      )}
      <Modal
        open={Boolean(selected)}
        onOpenChange={(open) => !open && setSelected(null)}
        title={`认证模板 · ${selected?.name ?? "认证方式"}`}
        description={selected?.summary ?? "查看模板定义，并基于模板创建认证实例。"}
      >
        {selected && (
          <div className="grid gap-4">
            <KeyValues
              items={[
                ["运行模式", selected.mode],
                ["凭据生命周期", selected.lifecycle],
                ["配置字段", selected.fields.join("、")],
              ]}
            />
            {!selected.executable && (
              <div className="rounded-xl bg-amber-50 p-3 text-sm leading-6 text-amber-800 dark:bg-amber-950 dark:text-amber-200">
                当前只提供配置模板和实例模型，运行时会失败关闭。请先实现并审核对应的 Go 认证扩展，或通过企业网关换成短期
                Token。
              </div>
            )}
            <div className="flex justify-end gap-2">
              <Button
                write
                disabled={!selected.executable}
                onClick={() => {
                  newAuthInstance(selected.id);
                  setSelected(null);
                }}
              >
                使用此模板添加实例
              </Button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
