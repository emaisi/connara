import { useId } from "react";
import { Button, Field, fieldClass } from "../ui";
import {
  authPreview,
  defaultAuthRequest,
  defaultVerificationRequest,
  type AuthCondition,
  type AuthRequest,
  type AuthRequestOptions,
  type AuthValue,
} from "../auth-request";

type Credential = { name: string; label?: string; secret: boolean };
function ValueEditor({
  value,
  onChange,
  fields,
  runtime = false,
}: {
  value: AuthValue;
  onChange: (value: AuthValue) => void;
  fields: Credential[];
  runtime?: boolean;
}) {
  return (
    <div className="grid min-w-0 gap-2 sm:grid-cols-2">
      <select
        aria-label="值来源"
        className={fieldClass}
        value={value.source}
        onChange={(e) =>
          onChange({
            source: e.target.value as AuthValue["source"],
            ...(e.target.value === "literal" ? { value: "" } : { name: "" }),
          })
        }
      >
        <option value="credential">凭据字段</option>
        <option value="literal">固定值</option>
        <option value="instance_secret">实例固定密钥</option>
        {runtime && <option value="runtime">运行时令牌</option>}
      </select>
      {value.source === "credential" ? (
        <select
          aria-label="凭据引用"
          className={fieldClass}
          value={value.name ?? ""}
          onChange={(e) => onChange({ ...value, name: e.target.value })}
        >
          <option value="">选择字段</option>
          {fields.map((field) => (
            <option key={field.name} value={field.name}>
              {field.label || field.name}
            </option>
          ))}
        </select>
      ) : value.source === "runtime" ? (
        <select
          aria-label="令牌引用"
          className={fieldClass}
          value={value.name ?? ""}
          onChange={(e) => onChange({ ...value, name: e.target.value })}
        >
          <option value="">选择令牌</option>
          {["token", "access_token", "refresh_token"].map((name) => (
            <option key={name}>{name}</option>
          ))}
        </select>
      ) : (
        <input
          aria-label={value.source === "literal" ? "固定参数值" : "实例密钥名称"}
          className={fieldClass}
          value={value.source === "literal" ? String(value.value ?? "") : (value.name ?? "")}
          onChange={(e) =>
            onChange(
              value.source === "literal"
                ? {
                    ...value,
                    value:
                      typeof value.value === "number"
                        ? Number(e.target.value)
                        : typeof value.value === "boolean"
                          ? e.target.value === "true"
                          : e.target.value,
                  }
                : { ...value, name: e.target.value },
            )
          }
        />
      )}
      {value.source === "literal" && (
        <select
          aria-label="固定值类型"
          className={fieldClass}
          value={value.value === null ? "null" : typeof value.value}
          onChange={(e) => {
            const type = e.target.value;
            onChange({
              ...value,
              value:
                type === "null"
                  ? null
                  : type === "number"
                    ? Number(value.value) || 0
                    : type === "boolean"
                      ? String(value.value) === "true"
                      : String(value.value ?? ""),
            });
          }}
        >
          <option value="string">文本</option>
          <option value="number">数字</option>
          <option value="boolean">布尔</option>
          <option value="null">空值</option>
        </select>
      )}
    </div>
  );
}
export function ConditionEditor({
  value,
  onChange,
  label = "业务成功条件",
}: {
  value?: AuthCondition;
  onChange: (value?: AuthCondition) => void;
  label?: string;
}) {
  return (
    <div className="grid gap-2">
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={Boolean(value)}
          onChange={(e) => onChange(e.target.checked ? { path: "$.code", operator: "equals", value: 0 } : undefined)}
        />
        {label}
      </label>
      {value && (
        <div className="grid min-w-0 gap-2 sm:grid-cols-[1fr_auto_1fr]">
          <input
            aria-label={`${label}字段`}
            className={fieldClass}
            value={value.path}
            onChange={(e) => onChange({ ...value, path: e.target.value })}
          />
          <span className="self-center text-sm">等于</span>
          <ValueEditor
            value={{ source: "literal", value: value.value }}
            fields={[]}
            onChange={(v) => onChange({ ...value, value: v.value ?? null })}
          />
        </div>
      )}
    </div>
  );
}
export function AuthRequestEditor({
  value,
  onChange,
  fields,
  runtime = false,
  verification = false,
}: {
  value: AuthRequest;
  onChange: (value: AuthRequest) => void;
  fields: Credential[];
  runtime?: boolean;
  verification?: boolean;
}) {
  const id = useId();
  return (
    <section className="grid min-w-0 gap-3">
      {value.method === "GET" && value.schemaVersion !== 2 && (
        <p className="text-sm text-amber-700">旧 GET 配置曾发送请求体。修改后将按查询参数发送，请重新测试。</p>
      )}
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="请求方法">
          <select
            id={id}
            aria-label="请求方法"
            className={fieldClass}
            value={value.method}
            onChange={(e) => {
              const method = e.target.value as AuthRequest["method"];
              onChange({
                ...value,
                schemaVersion: 2,
                method,
                bodyType: method === "GET" ? "none" : "json",
                parameters: value.parameters?.map((p) => ({ ...p, target: method === "GET" ? "query" : "body" })),
              });
            }}
          >
            <option>POST</option>
            <option>GET</option>
          </select>
        </Field>
        {value.method === "POST" && (
          <Field label="请求体格式">
            <select
              aria-label="请求体格式"
              className={fieldClass}
              value={value.bodyType}
              onChange={(e) =>
                onChange({ ...value, schemaVersion: 2, bodyType: e.target.value as AuthRequest["bodyType"] })
              }
            >
              <option value="json">JSON</option>
              <option value="form">表单</option>
            </select>
          </Field>
        )}
      </div>
      <details className="min-w-0 rounded-lg border border-[var(--border)] p-3">
        <summary className="cursor-pointer text-sm font-medium">高级请求配置</summary>
        <div className="mt-3 grid min-w-0 gap-3">
          {!verification && (
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={value.credentialMode === "mapped"}
                onChange={(e) =>
                  onChange({
                    ...value,
                    schemaVersion: 2,
                    credentialMode: e.target.checked ? "mapped" : "all",
                    parameters: e.target.checked
                      ? fields.map((field) => ({
                          name: field.name,
                          target: value.method === "GET" ? "query" : "body",
                          value: { source: "credential", name: field.name },
                        }))
                      : [],
                  })
                }
              />
              自定义参数映射
            </label>
          )}
          {value.credentialMode !== "mapped" && (
            <p className="text-xs text-[var(--muted-text)]">按凭据字段名称发送参数。</p>
          )}
          {value.credentialMode === "mapped" && (
            <>
              <p className="text-xs text-[var(--muted-text)]">
                JSON 嵌套参数可写 login.account；只发送下面配置的参数。
              </p>
              {value.parameters?.map((param, index) => (
                <div key={index} className="grid min-w-0 gap-2 rounded border border-[var(--border)] p-2">
                  <input
                    aria-label={`参数 ${index + 1} 名称`}
                    className={fieldClass}
                    value={param.name}
                    onChange={(e) =>
                      onChange({
                        ...value,
                        parameters: value.parameters?.map((p, i) => (i === index ? { ...p, name: e.target.value } : p)),
                      })
                    }
                  />
                  <select
                    aria-label={`参数 ${index + 1} 位置`}
                    className={fieldClass}
                    value={param.target}
                    onChange={(e) =>
                      onChange({
                        ...value,
                        parameters: value.parameters?.map((p, i) =>
                          i === index ? { ...p, target: e.target.value as "query" | "body" } : p,
                        ),
                      })
                    }
                  >
                    <option value="query">查询参数</option>
                    {value.method === "POST" && <option value="body">请求体</option>}
                  </select>
                  <ValueEditor
                    fields={fields}
                    runtime={runtime}
                    value={param.value}
                    onChange={(v) =>
                      onChange({
                        ...value,
                        parameters: value.parameters?.map((p, i) => (i === index ? { ...p, value: v } : p)),
                      })
                    }
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    onClick={() => onChange({ ...value, parameters: value.parameters?.filter((_, i) => i !== index) })}
                  >
                    删除参数
                  </Button>
                </div>
              ))}
              <Button
                type="button"
                variant="secondary"
                onClick={() =>
                  onChange({
                    ...value,
                    parameters: [
                      ...(value.parameters ?? []),
                      {
                        name: "",
                        target: value.method === "GET" ? "query" : "body",
                        value: { source: "literal", value: "" },
                      },
                    ],
                  })
                }
              >
                添加参数
              </Button>
            </>
          )}
          <p className="text-sm font-medium">认证请求头</p>
          {value.headers?.map((header, index) => (
            <div key={index} className="grid min-w-0 gap-2 rounded border border-[var(--border)] p-2">
              <input
                aria-label={`认证请求头 ${index + 1}`}
                className={fieldClass}
                value={header.name}
                onChange={(e) =>
                  onChange({
                    ...value,
                    headers: value.headers?.map((h, i) => (i === index ? { ...h, name: e.target.value } : h)),
                  })
                }
              />
              <ValueEditor
                fields={fields}
                runtime={runtime}
                value={header.value}
                onChange={(v) =>
                  onChange({ ...value, headers: value.headers?.map((h, i) => (i === index ? { ...h, value: v } : h)) })
                }
              />
              <Button
                type="button"
                variant="ghost"
                onClick={() => onChange({ ...value, headers: value.headers?.filter((_, i) => i !== index) })}
              >
                删除请求头
              </Button>
            </div>
          ))}
          <Button
            type="button"
            variant="secondary"
            onClick={() =>
              onChange({
                ...value,
                headers: [...(value.headers ?? []), { name: "", value: { source: "literal", value: "" } }],
              })
            }
          >
            添加请求头
          </Button>
          <details>
            <summary className="cursor-pointer text-sm">查看请求预览（敏感值已隐藏）</summary>
            <pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-all rounded bg-[var(--bg)] p-2 text-xs">
              {JSON.stringify(authPreview(value, fields), null, 2)}
            </pre>
          </details>
        </div>
      </details>
    </section>
  );
}
export function AuthOptionsEditor({
  value,
  onChange,
  templateRequest,
  fields,
  login,
  verificationPath,
  onVerificationPath,
}: {
  value: AuthRequestOptions;
  onChange: (value: AuthRequestOptions) => void;
  templateRequest: AuthRequest;
  fields: Credential[];
  login: boolean;
  verificationPath: string;
  onVerificationPath: (path: string) => void;
}) {
  return (
    <section className="grid min-w-0 gap-3">
      {login && (
        <>
          <p className="text-sm text-[var(--muted-text)]">
            {(value.requestOverride ?? templateRequest).method} ·{" "}
            {(value.requestOverride ?? templateRequest).bodyType.toUpperCase()} ·{" "}
            {value.requestOverride ? "实例覆盖" : "继承模板"}
          </p>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={Boolean(value.requestOverride)}
              onChange={(e) =>
                onChange({
                  ...value,
                  requestOverride: e.target.checked ? { ...templateRequest, schemaVersion: 2 } : null,
                })
              }
            />
            覆盖模板请求配置
          </label>
          {value.requestOverride && (
            <AuthRequestEditor
              value={value.requestOverride}
              onChange={(requestOverride) => onChange({ ...value, requestOverride })}
              fields={fields}
            />
          )}
          <Field label="有效期">
            <select
              aria-label="有效期方式"
              className={fieldClass}
              value={value.response.expiry.mode}
              onChange={(e) =>
                onChange({
                  ...value,
                  response: {
                    ...value.response,
                    expiry: {
                      mode: e.target.value as "field" | "fixed" | "none",
                      format: "duration_seconds",
                      seconds: 3600,
                    },
                  },
                })
              }
            >
              <option value="field">读取响应字段</option>
              <option value="fixed">固定有效秒数</option>
              <option value="none">无过期时间</option>
            </select>
          </Field>
          {value.response.expiry.mode === "fixed" && (
            <input
              aria-label="固定有效秒数"
              type="number"
              min="1"
              className={fieldClass}
              value={value.response.expiry.seconds ?? 3600}
              onChange={(e) =>
                onChange({
                  ...value,
                  response: {
                    ...value.response,
                    expiry: { ...value.response.expiry, seconds: Number(e.target.value) },
                  },
                })
              }
            />
          )}
        </>
      )}
      <details className="min-w-0 rounded-lg border border-[var(--border)] p-3">
        <summary className="cursor-pointer text-sm font-medium">认证注入覆盖</summary>
        <div className="mt-3 grid gap-3">
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={value.injectionOverride != null}
              onChange={(e) =>
                onChange({
                  ...value,
                  injectionOverride: e.target.checked
                    ? [{ target: "header", name: "Authorization", template: "Bearer {{token}}" }]
                    : null,
                })
              }
            />
            替换模板注入规则
          </label>
          {value.injectionOverride?.map((rule, index) => (
            <div key={index} className="grid gap-2 sm:grid-cols-2">
              <select
                aria-label={`覆盖注入 ${index + 1} 位置`}
                className={fieldClass}
                value={rule.target}
                onChange={(e) =>
                  onChange({
                    ...value,
                    injectionOverride: value.injectionOverride?.map((r, i) =>
                      i === index ? { ...r, target: e.target.value } : r,
                    ),
                  })
                }
              >
                <option value="header">请求头</option>
                <option value="query">查询参数</option>
                <option value="cookie">Cookie</option>
              </select>
              <input
                aria-label={`覆盖注入 ${index + 1} 名称`}
                className={fieldClass}
                value={rule.name}
                onChange={(e) =>
                  onChange({
                    ...value,
                    injectionOverride: value.injectionOverride?.map((r, i) =>
                      i === index ? { ...r, name: e.target.value } : r,
                    ),
                  })
                }
              />
              <input
                aria-label={`覆盖注入 ${index + 1} 模板`}
                className={fieldClass}
                value={rule.template}
                onChange={(e) =>
                  onChange({
                    ...value,
                    injectionOverride: value.injectionOverride?.map((r, i) =>
                      i === index ? { ...r, template: e.target.value } : r,
                    ),
                  })
                }
              />
              <Button
                type="button"
                variant="ghost"
                onClick={() =>
                  onChange({ ...value, injectionOverride: value.injectionOverride?.filter((_, i) => i !== index) })
                }
              >
                删除规则
              </Button>
            </div>
          ))}
          {value.injectionOverride && (
            <Button
              type="button"
              variant="secondary"
              onClick={() =>
                onChange({
                  ...value,
                  injectionOverride: [
                    ...(value.injectionOverride ?? []),
                    { target: "header", name: "", template: "{{token}}" },
                  ],
                })
              }
            >
              添加覆盖规则
            </Button>
          )}
        </div>
      </details>
      <details className="min-w-0 rounded-lg border border-[var(--border)] p-3">
        <summary className="cursor-pointer text-sm font-medium">高级响应与验证配置</summary>
        <div className="mt-3 grid gap-3">
          {login && (
            <>
              {value.response.expiry.mode === "field" && (
                <Field label="有效期格式">
                  <select
                    aria-label="有效期格式"
                    className={fieldClass}
                    value={value.response.expiry.format ?? "duration_seconds"}
                    onChange={(e) =>
                      onChange({
                        ...value,
                        response: { ...value.response, expiry: { ...value.response.expiry, format: e.target.value } },
                      })
                    }
                  >
                    {[
                      ["duration_seconds", "剩余秒数"],
                      ["duration_milliseconds", "剩余毫秒"],
                      ["unix_seconds", "Unix 秒时间戳"],
                      ["unix_milliseconds", "Unix 毫秒时间戳"],
                      ["rfc3339", "日期时间（RFC3339）"],
                    ].map(([key, label]) => (
                      <option key={key} value={key}>
                        {label}
                      </option>
                    ))}
                  </select>
                </Field>
              )}
              <ConditionEditor
                value={value.response.successCondition}
                onChange={(successCondition) =>
                  onChange({ ...value, response: { ...value.response, successCondition } })
                }
              />
              <Field label="令牌到期后">
                <select
                  className={fieldClass}
                  aria-label="刷新方式"
                  value={value.refresh.mode}
                  onChange={(e) =>
                    onChange({
                      ...value,
                      refresh: {
                        ...value.refresh,
                        mode: e.target.value as "relogin" | "refresh_token",
                        refreshTokenPath: value.refresh.refreshTokenPath ?? "$.refresh_token",
                        request: value.refresh.request ?? {
                          ...defaultAuthRequest(),
                          bodyType: "form",
                          credentialMode: "mapped",
                          parameters: [
                            {
                              name: "refresh_token",
                              target: "body",
                              value: { source: "runtime", name: "refresh_token" },
                            },
                          ],
                        },
                      },
                    })
                  }
                >
                  <option value="relogin">使用原凭据重新登录</option>
                  <option value="refresh_token">使用刷新令牌</option>
                </select>
              </Field>
              {value.refresh.mode === "refresh_token" && (
                <>
                  <Field label="刷新令牌 JSON 路径">
                    <input
                      className={fieldClass}
                      value={value.refresh.refreshTokenPath ?? ""}
                      onChange={(e) =>
                        onChange({ ...value, refresh: { ...value.refresh, refreshTokenPath: e.target.value } })
                      }
                    />
                  </Field>
                  <AuthRequestEditor
                    value={value.refresh.request ?? defaultAuthRequest()}
                    fields={fields}
                    runtime
                    onChange={(request) => onChange({ ...value, refresh: { ...value.refresh, request } })}
                  />
                  <label className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      checked={Boolean(value.refresh.fallbackOnInvalidRefreshToken)}
                      onChange={(e) =>
                        onChange({
                          ...value,
                          refresh: {
                            ...value.refresh,
                            fallbackOnInvalidRefreshToken: e.target.checked,
                            invalidRefreshCondition: e.target.checked
                              ? { path: "$.code", operator: "equals", value: 401 }
                              : undefined,
                          },
                        })
                      }
                    />
                    刷新凭据失效时重新登录一次
                  </label>
                  {value.refresh.fallbackOnInvalidRefreshToken && (
                    <ConditionEditor
                      label="刷新凭据失效条件"
                      value={value.refresh.invalidRefreshCondition}
                      onChange={(invalidRefreshCondition) =>
                        onChange({ ...value, refresh: { ...value.refresh, invalidRefreshCondition } })
                      }
                    />
                  )}
                </>
              )}
            </>
          )}
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={value.verification.enabled}
              onChange={(e) =>
                onChange({
                  ...value,
                  verification: {
                    ...value.verification,
                    enabled: e.target.checked,
                    request: value.verification.request ?? defaultVerificationRequest(),
                  },
                })
              }
            />
            启用业务 API 验证
          </label>
          {value.verification.enabled && (
            <>
              <Field label="凭据验证路径">
                <input
                  className={fieldClass}
                  value={verificationPath}
                  placeholder="/me"
                  onChange={(e) => onVerificationPath(e.target.value)}
                />
              </Field>
              <AuthRequestEditor
                value={value.verification.request ?? defaultVerificationRequest()}
                fields={fields.filter((f) => !f.secret)}
                runtime
                verification
                onChange={(request) => onChange({ ...value, verification: { ...value.verification, request } })}
              />
              <ConditionEditor
                label="业务验证成功条件"
                value={value.verification.successCondition}
                onChange={(successCondition) =>
                  onChange({ ...value, verification: { ...value.verification, successCondition } })
                }
              />
            </>
          )}
        </div>
      </details>
    </section>
  );
}
