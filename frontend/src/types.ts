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

export type Image = {
  id: string;
  name: string;
  path: string;
  androidApi?: string;
  description?: string;
  createdAt: string;
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
  state: InstanceState;
  cpuCores: number;
  memoryMb: number;
  adbPort: number;
  webrtcPort: number;
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

export type CreateImagePayload = {
  name: string;
  path: string;
  androidApi?: string;
  description?: string;
};

export type CreateInstancePayload = {
  name: string;
  imageId?: string;
  cpuCores: number;
  memoryMb: number;
};
