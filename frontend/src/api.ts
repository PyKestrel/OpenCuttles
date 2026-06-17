import type {
  AuditEvent,
  BootstrapStatus,
  CreateImagePayload,
  CreateInstancePayload,
  HealthReport,
  Host,
  Image,
  Instance,
  LoginResponse,
  Operation,
  Principal,
} from "./types";

const jsonHeaders = {
  "Content-Type": "application/json",
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { credentials: "include", ...init });
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(body.error ?? response.statusText);
  }
  return response.json() as Promise<T>;
}

export const api = {
  bootstrapStatus: () => request<BootstrapStatus>("/api/v1/bootstrap"),
  bootstrapAdmin: (payload: { username: string; displayName?: string; password: string; token?: string }) =>
    request<Principal>("/api/v1/bootstrap", {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify(payload),
    }),
  login: (payload: { username: string; password: string }) =>
    request<LoginResponse>("/api/v1/auth/login", {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify(payload),
    }),
  logout: () => request<{ status: string }>("/api/v1/auth/logout", { method: "POST" }),
  me: () => request<Principal>("/api/v1/auth/me"),
  host: () => request<Host>("/api/v1/host"),
  health: () => request<HealthReport>("/api/v1/health"),
  images: () => request<Image[]>("/api/v1/images"),
  createImage: (payload: CreateImagePayload) =>
    request<Image>("/api/v1/images", {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify(payload),
    }),
  instances: () => request<Instance[]>("/api/v1/instances"),
  createInstance: (payload: CreateInstancePayload) =>
    request<Instance>("/api/v1/instances", {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify(payload),
    }),
  startInstance: (id: string) =>
    request<{ instance: Instance; operation: Operation }>(`/api/v1/instances/${id}/start`, {
      method: "POST",
    }),
  stopInstance: (id: string) =>
    request<{ instance: Instance; operation: Operation }>(`/api/v1/instances/${id}/stop`, {
      method: "POST",
    }),
  deleteInstance: (id: string) =>
    request<Operation>(`/api/v1/instances/${id}`, {
      method: "DELETE",
    }),
  operations: () => request<Operation[]>("/api/v1/operations"),
  audit: () => request<AuditEvent[]>("/api/v1/audit"),
};
