import type {
  AgentModelConfig,
  AgentModelUpdate,
  AndroidVersion,
  AuditEvent,
  BootstrapStatus,
  Build,
  CaseHealth,
  CreateImagePayload,
  CreateInstancePayload,
  CycleRun,
  DeviceTest,
  HealthReport,
  Host,
  Image,
  ImportResult,
  Instance,
  LoginResponse,
  NotificationConfig,
  NotificationUpdate,
  Operation,
  RunnerDownload,
  PerfSnapshot,
  Platform,
  Principal,
  TestCase,
  TestCycle,
  TestRun,
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

// download fetches a file endpoint and triggers a browser save, honoring the
// server's Content-Disposition filename when present.
async function download(path: string, fallbackName: string): Promise<void> {
  const response = await fetch(path, { credentials: "include" });
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(body.error ?? response.statusText);
  }
  const blob = await response.blob();
  const disposition = response.headers.get("Content-Disposition") ?? "";
  const match = /filename="?([^";]+)"?/.exec(disposition);
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = match?.[1] ?? fallbackName;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
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
  // Onboard a desktop target; returns the one-time enrollment token the runner
  // presents to dial home. Same endpoint, keyed by platform.
  onboardDesktop: (name: string, platform: Exclude<Platform, "android">) =>
    request<{ instance: Instance; enrollmentToken: string }>("/api/v1/instances", {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ name, platform }),
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

  // Vision-grounded device tests (guarded by the "test" permission).
  tests: () => request<DeviceTest[]>("/api/v1/tests"),
  createTest: (name: string, steps: string[]) =>
    request<DeviceTest>("/api/v1/tests", {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ name, steps }),
    }),
  deleteTest: (id: string) =>
    request<{ status: string }>(`/api/v1/tests/${id}`, { method: "DELETE" }),
  runTest: (id: string, instanceId: string) =>
    request<TestRun>(`/api/v1/tests/${id}/run`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ instanceId }),
    }),
  testRuns: () => request<TestRun[]>("/api/v1/tests/runs"),
  testRun: (id: string) => request<TestRun>(`/api/v1/tests/runs/${id}`),
  testArtifactUrl: (runId: string, name: string) =>
    `/api/v1/tests/runs/${runId}/artifacts/${encodeURIComponent(name)}`,

  // QMetry-style test cases, cycles, cycle runs, and builds.
  runnerDownloads: () =>
    request<{ runners: RunnerDownload[] }>("/api/v1/runner/downloads").then((r) => r.runners ?? []),
  downloadRunner: (platform: string, arch: string) =>
    download(
      `/api/v1/runner/download?platform=${encodeURIComponent(platform)}&arch=${encodeURIComponent(arch)}`,
      platform === "windows" ? "opencuttles-runner.exe" : "opencuttles-runner",
    ),

  cases: () => request<TestCase[]>("/api/v1/cases"),
  caseFolders: () => request<string[]>("/api/v1/cases/folders"),
  createCaseFolder: (path: string) =>
    request<{ status: string }>("/api/v1/cases/folders", { method: "POST", headers: jsonHeaders, body: JSON.stringify({ path }) }),
  deleteCaseFolder: (path: string) =>
    request<{ status: string }>("/api/v1/cases/folders", { method: "DELETE", headers: jsonHeaders, body: JSON.stringify({ path }) }),
  createCase: (c: Partial<TestCase>) =>
    request<TestCase>("/api/v1/cases", { method: "POST", headers: jsonHeaders, body: JSON.stringify(c) }),
  updateCase: (id: string, c: Partial<TestCase>) =>
    request<TestCase>(`/api/v1/cases/${id}`, { method: "PUT", headers: jsonHeaders, body: JSON.stringify(c) }),
  deleteCase: (id: string) => request<{ status: string }>(`/api/v1/cases/${id}`, { method: "DELETE" }),
  importCases: (file: File) => {
    const form = new FormData();
    form.append("file", file);
    return request<ImportResult>("/api/v1/cases/import", { method: "POST", body: form });
  },
  caseHealth: () => request<CaseHealth[]>("/api/v1/cases/health"),
  exportCases: (format: "csv" | "xlsx", folder?: string) =>
    download(
      `/api/v1/cases/export?format=${format}${folder ? `&folder=${encodeURIComponent(folder)}` : ""}`,
      `cases-export.${format}`,
    ),

  cycles: () => request<TestCycle[]>("/api/v1/cycles"),
  createCycle: (c: Partial<TestCycle>) =>
    request<TestCycle>("/api/v1/cycles", { method: "POST", headers: jsonHeaders, body: JSON.stringify(c) }),
  updateCycle: (id: string, c: Partial<TestCycle>) =>
    request<TestCycle>(`/api/v1/cycles/${id}`, { method: "PUT", headers: jsonHeaders, body: JSON.stringify(c) }),
  deleteCycle: (id: string) => request<{ status: string }>(`/api/v1/cycles/${id}`, { method: "DELETE" }),
  updateCycleCases: (id: string, caseIds: string[]) =>
    request<TestCycle>(`/api/v1/cycles/${id}/cases`, { method: "PUT", headers: jsonHeaders, body: JSON.stringify({ caseIds }) }),
  updateCycleSchedule: (id: string, sched: { cron: string; timezone?: string; onNewBuild: boolean; enabled: boolean }) =>
    request<TestCycle>(`/api/v1/cycles/${id}/schedule`, { method: "PUT", headers: jsonHeaders, body: JSON.stringify(sched) }),
  runCycle: (id: string, opts?: { instanceId?: string; buildId?: string }) =>
    request<CycleRun>(`/api/v1/cycles/${id}/run`, { method: "POST", headers: jsonHeaders, body: JSON.stringify(opts ?? {}) }),

  cycleRuns: () => request<CycleRun[]>("/api/v1/cycle-runs"),
  cycleRun: (id: string) => request<{ run: CycleRun; cases: TestRun[] }>(`/api/v1/cycle-runs/${id}`),
  deleteCycleRun: (id: string) => request<{ status: string }>(`/api/v1/cycle-runs/${id}`, { method: "DELETE" }),
  exportCycleRun: (id: string, format: "junit" | "csv" | "xlsx") =>
    download(
      `/api/v1/cycle-runs/${id}/export?format=${format}`,
      `cyclerun-${id}.${format === "junit" ? "xml" : format}`,
    ),

  builds: (platform?: Platform) =>
    request<Build[]>(`/api/v1/builds${platform ? `?platform=${platform}` : ""}`),
  uploadBuild: (platform: Platform, file: File, version?: string, note?: string) => {
    const form = new FormData();
    form.append("platform", platform);
    form.append("artifact", file);
    if (version) form.append("version", version);
    if (note) form.append("note", note);
    return request<Build>("/api/v1/builds", { method: "POST", body: form });
  },

  // Agent model configuration (admin only). The API key is write-only: it is
  // never returned by GET; POST with apiFieldset omitted keeps the stored key.
  agentModel: () => request<AgentModelConfig>("/api/v1/agent/model"),
  saveAgentModel: (payload: AgentModelUpdate) =>
    request<AgentModelConfig>("/api/v1/agent/model", {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify(payload),
    }),
  testAgentModel: (payload: AgentModelUpdate) =>
    request<{ ok: boolean; message: string }>("/api/v1/agent/model/test", {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify(payload),
    }),

  notifications: () => request<NotificationConfig>("/api/v1/settings/notifications"),
  saveNotifications: (payload: NotificationUpdate) =>
    request<NotificationConfig>("/api/v1/settings/notifications", {
      method: "PUT",
      headers: jsonHeaders,
      body: JSON.stringify(payload),
    }),
};
