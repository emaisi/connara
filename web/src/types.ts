export interface Meta {
  name: string;
  version: string;
  workspaceId: string;
  providerCount: number;
}

export interface AdminSession {
  userId: string;
  email: string;
  displayName: string;
  role: string;
}

export interface ProviderSummary {
  service: string;
  displayName: string;
  description?: string;
  categories: string[];
  authTypes: string[];
  homepageUrl?: string;
  actionCount: number;
  executableActionCount: number;
}

export interface Action {
  id: string;
  service: string;
  name: string;
  description: string;
  requiredScopes: string[];
  providerPermissions: string[];
  inputSchema: Record<string, unknown>;
  outputSchema: Record<string, unknown>;
  executable: boolean;
}

export interface Provider extends Omit<ProviderSummary, "actionCount" | "executableActionCount"> {
  actions: Action[];
}

export interface Integration {
  id: string;
  environmentId: string;
  providerId: string;
  name: string;
  createdAt: string;
}

export interface Connection {
  id: string;
  environmentId: string;
  integrationId: string;
  providerId: string;
  name: string;
  authType: string;
  status: string;
  revision: number;
  createdAt: string;
  updatedAt: string;
}

export interface RuntimeToken {
  id: string;
  environmentId: string;
  name: string;
  status: "active" | "revoked" | string;
  allowedActions: string[];
  blockedActions: string[];
  allowedProxies: string[];
  allowedConnections: string[];
  createdAt: string;
  lastUsedAt?: string;
  revokedAt?: string;
}

export interface CreatedRuntimeToken {
  token: string;
  runtimeToken: RuntimeToken;
}

export interface Run {
  id: string;
  environmentId: string;
  tokenId?: string;
  actionId: string;
  connectionId?: string;
  status: "running" | "success" | "failed" | string;
  httpStatus?: number;
  input?: unknown;
  output?: unknown;
  error?: string;
  createdAt: string;
  completedAt?: string;
}
