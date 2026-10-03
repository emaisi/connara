export interface AuthValue {
  source: "credential" | "literal" | "instance_secret" | "runtime";
  name?: string;
  value?: string | number | boolean | null;
}
export interface AuthRequest {
  schemaVersion?: number;
  method: "GET" | "POST";
  bodyType: "none" | "json" | "form";
  credentialMode?: "all" | "mapped";
  parameters?: { name: string; target: "query" | "body"; value: AuthValue }[];
  headers?: { name: string; value: AuthValue }[];
  tokenPath?: string;
  expiryPath?: string;
}
export interface AuthCondition {
  path: string;
  operator: "equals";
  value: string | number | boolean | null;
}
export interface AuthRequestOptions {
  schemaVersion: 2;
  requestOverride?: AuthRequest | null;
  injectionOverride?: { target: string; name: string; template: string }[] | null;
  response: {
    successCondition?: AuthCondition;
    expiry: { mode: "field" | "fixed" | "none"; format?: string; seconds?: number };
  };
  refresh: {
    mode: "relogin" | "refresh_token";
    refreshTokenPath?: string;
    request?: AuthRequest;
    fallbackOnInvalidRefreshToken?: boolean;
    invalidRefreshCondition?: AuthCondition;
  };
  verification: { enabled: boolean; request?: AuthRequest; successCondition?: AuthCondition };
}
export interface AuthTestResult {
  configured: boolean;
  authenticated: boolean;
  apiVerified: boolean;
  message?: string;
  steps: { name: string; status: "passed" | "failed" | "skipped"; code?: string }[];
}
export const defaultAuthRequest = (): AuthRequest => ({
  schemaVersion: 2,
  method: "POST",
  bodyType: "json",
  credentialMode: "all",
  parameters: [],
  headers: [],
});
export const defaultVerificationRequest = (): AuthRequest => ({
  schemaVersion: 2,
  method: "GET",
  bodyType: "none",
  credentialMode: "mapped",
  parameters: [],
  headers: [],
});
export const defaultAuthOptions = (): AuthRequestOptions => ({
  schemaVersion: 2,
  response: { expiry: { mode: "field", format: "duration_seconds" } },
  refresh: { mode: "relogin" },
  verification: { enabled: false, request: defaultVerificationRequest() },
});
export function normalizeAuthRequest(request?: AuthRequest): AuthRequest {
  return { ...defaultAuthRequest(), ...request };
}
export function authPreview(request: AuthRequest, fields: { name: string; secret: boolean }[]) {
  const masked = (value: AuthValue): unknown =>
    value.source === "literal"
      ? value.value
      : value.source === "credential" && !fields.find((field) => field.name === value.name)?.secret
        ? `<${value.name}>`
        : "***";
  return {
    method: request.method,
    bodyType: request.bodyType,
    parameters:
      request.credentialMode === "all"
        ? fields.map((field) => ({
            name: field.name,
            target: request.method === "GET" ? "query" : "body",
            value: field.secret ? "***" : `<${field.name}>`,
          }))
        : request.parameters?.map((param) => ({ ...param, value: masked(param.value) })),
    headers: request.headers?.map((header) => ({
      name: header.name,
      value: /authorization|cookie|api-key|token|secret/i.test(header.name) ? "***" : masked(header.value),
    })),
  };
}
export function typedCredentials(
  fields: { name: string; type?: string }[],
  values: Record<string, string>,
): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(values)
      .filter(([, value]) => value !== "")
      .map(([name, value]) => {
        const type = fields.find((field) => field.name === name)?.type;
        return [name, type === "number" ? Number(value) : type === "boolean" ? value === "true" : value];
      }),
  );
}
