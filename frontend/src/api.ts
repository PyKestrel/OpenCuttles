import type {
  AndroidVersion,
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
  PerfSnapshot,
  Principal,
  UINode,
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
  androidVersions: () => request<AndroidVersion[]>("/api/v1/android-versions"),
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

  // Interactive device control (Phase 0 endpoints, guarded by the "control" permission).
  // The screenshot is consumed directly as an <img> src (cookie auth is sent automatically
  // for same-origin requests); a cache-busting token forces a fresh capture.
  controlScreenshotSrc: (id: string, token: number) =>
    `/api/v1/instances/${id}/control/screenshot?t=${token}`,
  controlUITree: (id: string) => request<UINode>(`/api/v1/instances/${id}/control/ui-tree`),
  controlCurrentActivity: (id: string) =>
    request<{ activity: string }>(`/api/v1/instances/${id}/control/current-activity`),
  controlPerf: (id: string) => request<PerfSnapshot>(`/api/v1/instances/${id}/control/perf`),
  controlLogcat: (id: string, lines = 300) =>
    request<{ logcat: string }>(`/api/v1/instances/${id}/control/logcat?lines=${lines}`),
  controlTap: (id: string, x: number, y: number) =>
    request<{ status: string }>(`/api/v1/instances/${id}/control/input/tap`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ x, y }),
    }),
  controlSwipe: (id: string, x: number, y: number, x2: number, y2: number, duration = 200) =>
    request<{ status: string }>(`/api/v1/instances/${id}/control/input/swipe`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ x, y, x2, y2, duration }),
    }),
  controlText: (id: string, text: string) =>
    request<{ status: string }>(`/api/v1/instances/${id}/control/input/text`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ text }),
    }),
  controlKey: (id: string, key: string) =>
    request<{ status: string }>(`/api/v1/instances/${id}/control/input/key`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ key }),
    }),
  controlRotate: (id: string, orientation: number) =>
    request<{ status: string }>(`/api/v1/instances/${id}/control/rotate`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ orientation }),
    }),
  controlListApps: (id: string, thirdPartyOnly = false) =>
    request<{ packages: string[] }>(
      `/api/v1/instances/${id}/control/apps${thirdPartyOnly ? "?thirdParty=1" : ""}`,
    ),
  controlLaunchApp: (id: string, pkg: string) =>
    request<{ status: string }>(`/api/v1/instances/${id}/control/apps/launch`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ package: pkg }),
    }),
  controlInstallApp: (id: string, file: File) => {
    const form = new FormData();
    form.append("apk", file);
    // No Content-Type header: the browser sets the multipart boundary.
    return request<{ status: string; file: string }>(
      `/api/v1/instances/${id}/control/apps/install`,
      { method: "POST", body: form },
    );
  },
  controlShell: (id: string, command: string) =>
    request<{ output: string }>(`/api/v1/instances/${id}/control/shell`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ command }),
    }),
};
