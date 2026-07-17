export type Prerequisite = {
  name: string;
  ok: boolean;
  detail: string;
  remedy?: string;
};

export type Host = {
  id: string;
  name: string;
  cpuCount: number;
  memoryBytes: number;
  diskFreeBytes: number;
  prerequisites: Prerequisite[];
  updatedAt: string;
};

export type ImageStatus = "pending" | "fetching" | "ready" | "error";

export type Image = {
  id: string;
  name: string;
  path: string;
  androidApi?: string;
  description?: string;
  buildTarget?: string;
  versionId?: string;
  status?: ImageStatus;
  sizeBytes?: number;
  lastError?: string;
  createdAt: string;
};

export type AndroidVersion = {
  id: string;
  label: string;
  branch: string;
  buildTarget: string;
  description?: string;
};

export type InstanceState =
  | "provisioning"
  | "starting"
  | "booting"
  | "running"
  | "stopping"
  | "stopped"
  | "error"
  | "deleting"
  | "online"
  | "offline";

export type Platform = "android" | "windows" | "linux" | "macos";

export type Instance = {
  id: string;
  name: string;
  hostId: string;
  platform: Platform;
  controlEndpoint?: string;
  imageId: string;
  androidVersion?: string;
  state: InstanceState;
  cpuCores: number;
  memoryMb: number;
  displayWidth: number;
  displayHeight: number;
  dpi: number;
  adbPort: number;
  webrtcPort: number;
  deviceId: string;
  consoleProvider: string;
  consoleUrl: string;
  lastError?: string;
  createdAt: string;
  updatedAt: string;
};

export type Operation = {
  id: string;
  instanceId?: string;
  action: string;
  status: string;
  message?: string;
  createdAt: string;
  finishedAt?: string;
};

export type Principal = {
  userId: string;
  username: string;
  displayName: string;
  role: string;
  permissions: string[];
};

export type BootstrapStatus = {
  required: boolean;
};

export type LoginResponse = {
  principal: Principal;
  expiresAt: string;
};

export type AuditEvent = {
  id: string;
  actorId?: string;
  actorName?: string;
  action: string;
  resource: string;
  resourceId?: string;
  outcome: string;
  message?: string;
  sourceIp?: string;
  userAgent?: string;
  requestId?: string;
  createdAt: string;
};

export type HealthReport = {
  status: string;
  checks: { name: string; status: string; message: string }[];
  generatedAt: string;
};

export type UINode = {
  text?: string;
  resourceId?: string;
  class?: string;
  contentDesc?: string;
  package?: string;
  clickable?: boolean;
  scrollable?: boolean;
  focused?: boolean;
  bounds?: string;
  center?: { x: number; y: number };
  children?: UINode[];
};

export type PerfSnapshot = {
  package?: string;
  batteryLevel: number;
  totalPssKb?: number;
};

export type DeviceTest = {
  id: string;
  name: string;
  steps: string[];
  createdAt: string;
};

export type StepResult = {
  index: number;
  text: string;
  verb: string;
  target?: string;
  value?: string;
  x?: number;
  y?: number;
  modelOutput?: string;
  pass: boolean;
  status?: "pass" | "fail" | "blocked";
  detail?: string;
  durationMs: number;
  screenshot?: string;
  battery?: number;
};

export type TestRun = {
  id: string;
  testId: string;
  testName?: string;
  instanceId: string;
  status: "running" | "passed" | "failed";
  passed: boolean;
  steps: StepResult[];
  video?: string;
  error?: string;
  startedAt: string;
  finishedAt?: string;
  cycleRunId?: string;
  caseId?: string;
};

// QMetry-style test management.
export type TestStep = {
  index: number;
  action: string;
  testData?: string;
  expected?: string;
};

export type TestCase = {
  id: string;
  summary: string;
  description?: string;
  precondition?: string;
  priority?: string;
  status?: string;
  labels: string[];
  components: string[];
  folderPath?: string;
  steps: TestStep[];
  externalKey?: string;
  createdAt: string;
  updatedAt: string;
};

export type TestCycle = {
  id: string;
  name: string;
  platform: Platform;
  buildId?: string;
  environment?: string;
  caseIds: string[];
  cron?: string;
  /** IANA zone (e.g. "America/New_York") the cron's wall-clock fields use. Empty = UTC. */
  timezone?: string;
  onNewBuild: boolean;
  enabled: boolean;
  lastRunAt?: string;
  nextRunAt?: string;
  createdAt: string;
};

export type CycleTotals = {
  cases: number;
  pass: number;
  fail: number;
  blocked: number;
  notRun: number;
};

export type CycleRun = {
  id: string;
  cycleId: string;
  cycleName?: string;
  trigger: "manual" | "cron" | "build";
  buildId?: string;
  instanceId?: string;
  status: "running" | "passed" | "failed";
  totals: CycleTotals;
  startedAt: string;
  finishedAt?: string;
};

export type Build = {
  id: string;
  platform: Platform;
  filename: string;
  path: string;
  sizeBytes: number;
  version?: string;
  status: string;
  note?: string;
  createdAt: string;
};

export type ImportResult = {
  casesParsed: number;
  stepsParsed: number;
  rowsSkipped: number;
  warnings: string[];
};

export type AgentModelPreset = {
  label: string;
  providerId: string;
  api: string;
  baseUrl: string;
  model: string;
  needsKey: boolean;
};

export type AgentModelConfig = {
  providerId: string;
  api: string;
  baseUrl: string;
  model: string;
  headers?: Record<string, string>;
  keySet: boolean;
  secretStorageEnabled: boolean;
  supportedApis: string[];
  presets: AgentModelPreset[];
};

// apiKey is tri-state on the wire: omit to keep the stored key, "" to clear it,
// a value to set it.
export type AgentModelUpdate = {
  providerId: string;
  api: string;
  baseUrl: string;
  model: string;
  headers?: Record<string, string>;
  apiKey?: string;
};

export type CaseHistoryPoint = {
  runId: string;
  cycleRunId?: string;
  /** pass | fail | blocked */
  status: string;
  startedAt: string;
  durationMs: number;
};

/** Cross-run health for a test case: is it passing, and can it be trusted? */
export type CaseHealth = {
  caseId: string;
  summary: string;
  runs: number;
  pass: number;
  fail: number;
  blocked: number;
  /** 0..1 */
  passRate: number;
  /** status changes between consecutive runs */
  flips: number;
  flaky: boolean;
  lastStatus: string;
  avgMs: number;
  /** oldest-first, for sparklines */
  history: CaseHistoryPoint[];
};

export type NotificationConfig = {
  url: string;
  onlyOnFailure: boolean;
  secretHeader: string;
  secretSet: boolean;
  secretStorageEnabled: boolean;
};

// secret is tri-state on the wire: omit to keep the stored secret, "" to clear
// it, a value to set it.
export type NotificationUpdate = {
  url: string;
  onlyOnFailure: boolean;
  secretHeader: string;
  secret?: string;
};

export type CreateImagePayload = {
  name: string;
  path: string;
  androidApi?: string;
  description?: string;
};

export type CreateInstancePayload = {
  name: string;
  imageId?: string;
  androidVersion?: string;
  cpuCores: number;
  memoryMb: number;
  displayWidth?: number;
  displayHeight?: number;
  dpi?: number;
};
