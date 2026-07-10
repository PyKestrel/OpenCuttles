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
  | "deleting";

export type Instance = {
  id: string;
  name: string;
  hostId: string;
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
