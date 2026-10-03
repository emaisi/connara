import { api, type AdminConnection } from "../api";

export function accountVerificationPath(config?: Record<string, unknown>) {
  const request = config?.authRequest as { schemaVersion?: number; verification?: { enabled?: boolean } } | undefined;
  if (request?.schemaVersion === 2 && !request.verification?.enabled) return "";
  return typeof config?.verificationPath === "string" ? config.verificationPath : "";
}

interface SaveConnectionInput {
  verifyAfterSave?: boolean;
  integrationId: string;
  connectionKey: string;
  name: string;
  endUserKey: string;
  endUserName?: string;
  credentials: Record<string, unknown>;
  connectionId?: string;
  revision?: number;
  credentialsChanged?: boolean;
  onSaved: (connection: AdminConnection) => void;
  onRevision?: (revision: number) => void;
}

export function validHttpUrl(value: string) {
  try {
    const url = new URL(value.trim());
    return ["http:", "https:"].includes(url.protocol) && !url.username && !url.password && !url.hash;
  } catch {
    return false;
  }
}

export function oauthReturnPath(integration: string, connectionKey: string, name: string, endUserKey: string) {
  return `/auth?section=accounts&${new URLSearchParams({ integration, connectionKey, accountName: name, endUserKey })}`;
}

export async function saveOrFindIntegration(input: {
  integrationKey: string;
  name: string;
  systemId: string;
  authInstanceId: string;
  baseUrl: string;
}) {
  const find = async () => {
    const existing = (await api.integrations()).find((item) => item.integrationKey === input.integrationKey);
    if (!existing) return undefined;
    if (
      existing.systemId !== input.systemId ||
      existing.authInstanceId !== input.authInstanceId ||
      existing.baseUrl !== input.baseUrl ||
      existing.name !== input.name
    ) {
      throw new Error("集成标识已被其他配置占用，请检查高级设置中的集成 ID");
    }
    return existing;
  };
  const existing = await find();
  if (existing) return existing;
  try {
    return await api.saveIntegration({ ...input, status: "ready", settings: {} });
  } catch (saveError) {
    const recovered = await find();
    if (recovered) return recovered;
    throw saveError;
  }
}

export async function saveAccount(input: SaveConnectionInput) {
  let connectionId = input.connectionId;
  let committed: AdminConnection | undefined;
  if (input.connectionId) {
    if (input.credentialsChanged) {
      let connection: AdminConnection;
      try {
        connection = await api.updateConnection(input.connectionId, {
          revision: input.revision,
          integrationId: input.integrationId,
          connectionKey: input.connectionKey,
          name: input.name,
          endUserKey: input.endUserKey,
          endUserName: input.endUserName,
          credentials: input.credentials,
        });
      } catch (error) {
        const current = (await api.connections()).find((item) => item.id === input.connectionId);
        if (current && current.revision !== input.revision) {
          input.onRevision?.(current.revision);
          throw new Error("账号版本已变化，更新结果可能未收到或账号被其他人修改。请确认凭据后重试更新。");
        }
        throw error;
      }
      committed = connection;
      input.onSaved(connection);
    }
  } else {
    let connection: AdminConnection;
    try {
      connection = await api.saveConnection({
        integrationId: input.integrationId,
        connectionKey: input.connectionKey,
        name: input.name,
        endUserKey: input.endUserKey,
        endUserName: input.endUserName,
        credentials: input.credentials,
      });
    } catch (saveError) {
      const existing = (await api.connections()).find((item) => item.connectionKey === input.connectionKey);
      if (
        !existing ||
        existing.integrationId !== input.integrationId ||
        existing.endUserKey !== input.endUserKey ||
        existing.name !== input.name
      ) {
        throw saveError;
      }
      connection = existing;
    }
    committed = connection;
    input.onSaved(connection);
    connectionId = connection.id;
  }
  if (input.verifyAfterSave) {
    const result = await api.verifyConnection(connectionId!);
    return result.connection;
  }
  if (committed) return committed;
  const saved = (await api.connections()).find((row) => row.id === connectionId);
  if (!saved) throw new Error("账号已保存，请刷新账号列表查看");
  return saved;
}
