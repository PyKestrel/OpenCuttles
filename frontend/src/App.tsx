import { FormEvent, useEffect, useMemo, useState } from "react";
import { api } from "./api";
import type {
  AndroidVersion,
  AuditEvent,
  HealthReport,
  Host,
  Image,
  Instance,
  Operation,
  Principal,
} from "./types";

type LoadState = {
  host?: Host;
  images: Image[];
  instances: Instance[];
  operations: Operation[];
  audit: AuditEvent[];
  androidVersions: AndroidVersion[];
  health?: HealthReport;
};

const initialState: LoadState = {
  images: [],
  instances: [],
  operations: [],
  audit: [],
  androidVersions: [],
};

export default function App() {
  const [data, setData] = useState<LoadState>(initialState);
  const [selectedInstanceId, setSelectedInstanceId] = useState<string>("");
  const [error, setError] = useState<string>("");
  const [busy, setBusy] = useState(false);
  const [principal, setPrincipal] = useState<Principal>();
  const [bootstrapRequired, setBootstrapRequired] = useState(false);
  const [authChecked, setAuthChecked] = useState(false);
  const [view, setView] = useState("dashboard");

  const selectedInstance = useMemo(
    () => data.instances.find((instance) => instance.id === selectedInstanceId) ?? data.instances[0],
    [data.instances, selectedInstanceId],
  );

  async function refresh() {
    const canAdmin = principal ? hasPermission(principal, "admin") : false;
    const [host, images, instances, operations, health, audit, androidVersions] = await Promise.all([
      api.host(),
      api.images(),
      api.instances(),
      api.operations(),
      api.health(),
      canAdmin ? api.audit().catch(() => []) : Promise.resolve([]),
      api.androidVersions().catch(() => []),
    ]);
    setData({
      host,
      images: images ?? [],
      instances: instances ?? [],
      operations: operations ?? [],
      health,
      audit: audit ?? [],
      androidVersions: androidVersions ?? [],
    });
  }

  useEffect(() => {
    async function boot() {
      try {
        const bootstrap = await api.bootstrapStatus();
        setBootstrapRequired(bootstrap.required);
        if (!bootstrap.required) {
          const me = await api.me();
          setPrincipal(me);
          await refresh();
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : "Unable to initialize");
      } finally {
        setAuthChecked(true);
      }
    }
    boot();
  }, []);

  useEffect(() => {
    if (!principal) {
      return;
    }
    // Resource-conscious polling: skip refreshes while the tab is hidden and
    // catch up immediately when it becomes visible again.
    const tick = () => {
      if (document.hidden) {
        return;
      }
      refresh().catch((err: Error) => setError(err.message));
    };
    const timer = window.setInterval(tick, 5000);
    const onVisibility = () => {
      if (!document.hidden) {
        tick();
      }
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [principal]);

  async function runAction(action: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await action();
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unexpected error");
    } finally {
      setBusy(false);
    }
  }

  async function handleAuthenticated(nextPrincipal: Principal) {
    setPrincipal(nextPrincipal);
    setBootstrapRequired(false);
    await refresh();
  }

  async function logout() {
    await api.logout();
    setPrincipal(undefined);
    setData(initialState);
  }

  if (!authChecked) {
    return <div className="auth-screen"><div className="auth-card">Loading OpenCuttles...</div></div>;
  }

  if (!principal || bootstrapRequired) {
    return <AuthGate bootstrapRequired={bootstrapRequired} error={error} onAuthenticated={handleAuthenticated} />;
  }

  return (
    <>
      <header className="masthead">
        <div className="masthead-brand">
          <div className="brand-mark">OC</div>
          <div className="masthead-title">
            <strong>OpenCuttles</strong>
            <span>{data.host?.name ?? "local host"}</span>
          </div>
        </div>
        <div className="masthead-actions">
          <span className="masthead-user">{principal.displayName} · {principal.role}</span>
          <button disabled={busy} onClick={() => runAction(refresh)}>Refresh</button>
          <button className="link-button" onClick={logout}>Session</button>
        </div>
      </header>
      <div className="app-shell">
        <aside className="sidebar">
          <nav className="nav-groups">
            {navGroups(principal).map((group) => (
              <div className="nav-group" key={group.title}>
                <h4>{group.title}</h4>
                {group.items.map((item) => (
                  <button className={view === item ? "active" : ""} key={item} onClick={() => setView(item)}>
                    {NAV_LABELS[item] ?? item}
                  </button>
                ))}
              </div>
            ))}
          </nav>
          <div className="inventory">
            <h4>Devices</h4>
            {data.instances.length === 0 && <span className="inventory-empty">No instances yet</span>}
            {data.instances.map((instance) => (
              <button
                className={`tree-node ${selectedInstance?.id === instance.id ? "selected" : ""}`}
                key={instance.id}
                onClick={() => {
                  setSelectedInstanceId(instance.id);
                  setView("instances");
                }}
              >
                <span className={`state-dot state-${instance.state}`} />
                {instance.name}
              </button>
            ))}
          </div>
        </aside>

        <main>
        <header className="topbar">
          <div>
            <span className="eyebrow">{NAV_LABELS[view] ?? "Overview"}</span>
            <h1>{PAGE_TITLES[view] ?? "Android device control plane"}</h1>
          </div>
        </header>

        {error && <div className="alert">{error}</div>}

        <section className="cards">
          <MetricCard label="Host CPU" value={data.host ? `${data.host.cpuCount} cores` : "Loading"} />
          <MetricCard label="Instances" value={String(data.instances.length)} />
          <MetricCard
            label="Running"
            value={String(data.instances.filter((instance) => instance.state === "running").length)}
          />
          <MetricCard
            label="Prerequisites"
            value={`${data.host?.prerequisites.filter((item) => item.ok).length ?? 0}/${data.host?.prerequisites.length ?? 0}`}
          />
        </section>

        {view === "dashboard" || view === "instances" || view === "images" ? <section className="workspace-grid">
          <div className="panel wide">
            <div className="panel-title">
              <div>
                <span className="eyebrow">Compute</span>
                <h2>Cuttlefish instances</h2>
              </div>
              <span className="muted">{data.instances.length} total</span>
            </div>
            <InstanceTable
              instances={data.instances}
              selectedId={selectedInstance?.id}
              busy={busy}
              canOperate={hasPermission(principal, "operate")}
              onSelect={setSelectedInstanceId}
              onStart={(id) => runAction(() => api.startInstance(id))}
              onStop={(id) => runAction(() => api.stopInstance(id))}
              onDelete={(id) => {
                if (window.confirm("Delete this Android instance? This cannot be undone.")) {
                  runAction(() => api.deleteInstance(id));
                }
              }}
            />
          </div>

          <div className="panel">
            <div className="panel-title">
              <div>
                <span className="eyebrow">Provisioning</span>
                <h2>Create resources</h2>
              </div>
            </div>
            <CreateForms images={data.images} androidVersions={data.androidVersions} busy={busy} canOperate={hasPermission(principal, "operate")} onAction={runAction} />
          </div>
        </section> : null}

        {view === "dashboard" || view === "host" || view === "settings" ? <section className="workspace-grid">
          <InstanceDetails instance={selectedInstance} />
          <HostHealth host={data.host} health={data.health} />
        </section> : null}

        {view === "dashboard" || view === "operations" || view === "audit" ? <section className="workspace-grid">
          <Operations operations={data.operations} />
          {hasPermission(principal, "admin") ? <AuditLog events={data.audit} /> : <ReadOnlyNotice />}
        </section> : null}

        {view === "dashboard" || view === "instances" ? <ConsolePanel instance={selectedInstance} /> : null}
        </main>
      </div>
    </>
  );
}

function AuthGate({
  bootstrapRequired,
  error,
  onAuthenticated,
}: {
  bootstrapRequired: boolean;
  error: string;
  onAuthenticated: (principal: Principal) => Promise<void>;
}) {
  const [username, setUsername] = useState("admin");
  const [displayName, setDisplayName] = useState("OpenCuttles Admin");
  const [password, setPassword] = useState("");
  const [token, setToken] = useState("");
  const [localError, setLocalError] = useState(error);
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setLocalError("");
    try {
      if (bootstrapRequired) {
        const principal = await api.bootstrapAdmin({ username, displayName, password, token });
        await api.login({ username, password });
        await onAuthenticated(principal);
      } else {
        const response = await api.login({ username, password });
        await onAuthenticated(response.principal);
      }
    } catch (err) {
      setLocalError(err instanceof Error ? err.message : "Authentication failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="auth-screen">
      <form className="auth-card" onSubmit={submit}>
        <div className="brand auth-brand">
          <div className="brand-mark">OC</div>
          <div>
            <strong>OpenCuttles</strong>
            <span>{bootstrapRequired ? "Bootstrap local admin" : "Sign in"}</span>
          </div>
        </div>
        {localError && <div className="alert">{localError}</div>}
        <label>
          Username
          <input value={username} onChange={(event) => setUsername(event.target.value)} />
        </label>
        {bootstrapRequired && (
          <label>
            Display name
            <input value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
          </label>
        )}
        <label>
          Password
          <input
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            placeholder={bootstrapRequired ? "At least 12 characters" : "Password"}
          />
        </label>
        {bootstrapRequired && (
          <label>
            Bootstrap token
            <input
              type="password"
              value={token}
              onChange={(event) => setToken(event.target.value)}
              placeholder="From OPENCUTTLES_BOOTSTRAP_TOKEN"
            />
          </label>
        )}
        <button className="primary" disabled={busy || !username || !password}>
          {bootstrapRequired ? "Create admin" : "Sign in"}
        </button>
      </form>
    </div>
  );
}

function MetricCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="metric-card">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function InstanceTable({
  instances,
  selectedId,
  busy,
  canOperate,
  onSelect,
  onStart,
  onStop,
  onDelete,
}: {
  instances: Instance[];
  selectedId?: string;
  busy: boolean;
  canOperate: boolean;
  onSelect: (id: string) => void;
  onStart: (id: string) => void;
  onStop: (id: string) => void;
  onDelete: (id: string) => void;
}) {
  if (instances.length === 0) {
    return <div className="empty">No Android devices yet. Create an instance and OpenCuttles will use the default Cuttlefish image automatically.</div>;
  }

  return (
    <table>
      <thead>
        <tr>
          <th>Name</th>
          <th>State</th>
          <th>CPU</th>
          <th>Memory</th>
          <th>ADB</th>
          <th>WebRTC</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        {instances.map((instance) => (
          <tr
            className={selectedId === instance.id ? "selected-row" : ""}
            key={instance.id}
            onClick={() => onSelect(instance.id)}
          >
            <td>
              <strong>{instance.name}</strong>
              <small>{instance.id}</small>
            </td>
            <td>
              <span className={`state state-${instance.state}`}>{instance.state}</span>
            </td>
            <td>{instance.cpuCores}</td>
            <td>{instance.memoryMb} MB</td>
            <td>localhost:{instance.adbPort}</td>
            <td>{instance.webrtcPort}</td>
            <td className="actions">
              <button disabled={!canOperate || busy || instance.state === "running"} onClick={() => onStart(instance.id)}>
                Start
              </button>
              <button disabled={!canOperate || busy || instance.state === "stopped"} onClick={() => onStop(instance.id)}>
                Stop
              </button>
              <button disabled={!canOperate || busy} onClick={() => onDelete(instance.id)}>
                Delete
              </button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

const RESOLUTION_PRESETS = [
  { id: "phone", label: "Phone · 720 × 1280 (320 dpi)", width: 720, height: 1280, dpi: 320 },
  { id: "phone-hd", label: "Phone HD · 1080 × 1920 (440 dpi)", width: 1080, height: 1920, dpi: 440 },
  { id: "tablet", label: "Tablet · 1200 × 1920 (240 dpi)", width: 1200, height: 1920, dpi: 240 },
  { id: "compact", label: "Compact · 480 × 800 (240 dpi)", width: 480, height: 800, dpi: 240 },
];

function CreateForms({
  images,
  androidVersions,
  busy,
  canOperate,
  onAction,
}: {
  images: Image[];
  androidVersions: AndroidVersion[];
  busy: boolean;
  canOperate: boolean;
  onAction: (action: () => Promise<unknown>) => Promise<void>;
}) {
  const [imageName, setImageName] = useState("");
  const [imagePath, setImagePath] = useState("");
  const [instanceName, setInstanceName] = useState("");
  const [androidVersion, setAndroidVersion] = useState("");
  const [imageId, setImageId] = useState("");
  const [cpuCores, setCpuCores] = useState(2);
  const [memoryMb, setMemoryMb] = useState(4096);
  const [resolution, setResolution] = useState(RESOLUTION_PRESETS[0].id);
  const [showAdvanced, setShowAdvanced] = useState(false);

  useEffect(() => {
    if (!androidVersion && androidVersions.length > 0) {
      setAndroidVersion(androidVersions[0].id);
    }
  }, [androidVersions, androidVersion]);

  const preset = RESOLUTION_PRESETS.find((item) => item.id === resolution) ?? RESOLUTION_PRESETS[0];

  async function submitImage(event: FormEvent) {
    event.preventDefault();
    await onAction(() => api.createImage({ name: imageName, path: imagePath }));
    setImageName("");
    setImagePath("");
  }

  async function submitInstance(event: FormEvent) {
    event.preventDefault();
    await onAction(() =>
      api.createInstance({
        name: instanceName,
        androidVersion: imageId ? undefined : androidVersion,
        imageId: imageId || undefined,
        cpuCores,
        memoryMb,
        displayWidth: preset.width,
        displayHeight: preset.height,
        dpi: preset.dpi,
      }),
    );
    setInstanceName("");
  }

  return (
    <div className="forms">
      <form onSubmit={submitInstance}>
        <h3>Deploy Android instance</h3>
        <p className="form-help">
          Choose a version and size, then deploy. OpenCuttles fetches the image automatically with <code>cvd fetch</code> and launches the device &mdash; no manual image registration required.
        </p>
        <label>
          Name
          <input
            value={instanceName}
            onChange={(event) => setInstanceName(event.target.value)}
            placeholder="android-01"
          />
        </label>
        <label>
          Android version
          <select value={androidVersion} onChange={(event) => setAndroidVersion(event.target.value)} disabled={Boolean(imageId)}>
            {androidVersions.length === 0 && <option value="">Loading versions…</option>}
            {androidVersions.map((version) => (
              <option value={version.id} key={version.id}>
                {version.label}
              </option>
            ))}
          </select>
        </label>
        <label>
          Resolution
          <select value={resolution} onChange={(event) => setResolution(event.target.value)}>
            {RESOLUTION_PRESETS.map((item) => (
              <option value={item.id} key={item.id}>
                {item.label}
              </option>
            ))}
          </select>
        </label>
        <div className="form-row">
          <label>
            CPU cores
            <input type="number" min="1" max="16" value={cpuCores} onChange={(event) => setCpuCores(Number(event.target.value))} />
          </label>
          <label>
            Memory MB
            <input type="number" min="1024" step="512" value={memoryMb} onChange={(event) => setMemoryMb(Number(event.target.value))} />
          </label>
        </div>
        <button className="primary" disabled={!canOperate || busy || !instanceName || (!androidVersion && !imageId)}>
          Deploy instance
        </button>
      </form>

      <button className="text-button" type="button" onClick={() => setShowAdvanced((value) => !value)}>
        {showAdvanced ? "Hide advanced options" : "Advanced: custom image"}
      </button>

      {showAdvanced && (
        <div className="advanced-block">
          <label>
            Use a registered image instead <span className="optional">(overrides version)</span>
            <select value={imageId} onChange={(event) => setImageId(event.target.value)}>
              <option value="">Auto-fetch selected Android version</option>
              {images.map((image) => (
                <option value={image.id} key={image.id}>
                  {image.name}
                  {image.status && image.status !== "ready" ? ` (${image.status})` : ""}
                </option>
              ))}
            </select>
          </label>

          <form onSubmit={submitImage}>
            <h3>Register custom image</h3>
            <label>
              Name
              <input value={imageName} onChange={(event) => setImageName(event.target.value)} placeholder="AOSP main" />
            </label>
            <label>
              Image path
              <input value={imagePath} onChange={(event) => setImagePath(event.target.value)} placeholder="/var/lib/opencuttles/images/aosp" />
            </label>
            <button className="primary" disabled={!canOperate || busy || !imageName || !imagePath}>
              Register image
            </button>
          </form>
        </div>
      )}
    </div>
  );
}

function InstanceDetails({ instance }: { instance?: Instance }) {
  return (
    <div className="panel">
      <div className="panel-title">
        <div>
          <span className="eyebrow">Selected device</span>
          <h2>{instance?.name ?? "No instance selected"}</h2>
        </div>
      </div>
      {instance ? (
        <dl className="details">
          <dt>State</dt>
          <dd>{instance.state}</dd>
          <dt>Console provider</dt>
          <dd>{instance.consoleProvider}</dd>
          <dt>Console URL</dt>
          <dd>{instance.consoleUrl}</dd>
          <dt>Last error</dt>
          <dd>{instance.lastError || "None"}</dd>
        </dl>
      ) : (
        <div className="empty">Create or select an instance to see details.</div>
      )}
    </div>
  );
}

function HostHealth({ host, health }: { host?: Host; health?: HealthReport }) {
  return (
    <div className="panel">
      <div className="panel-title">
        <div>
          <span className="eyebrow">Host health</span>
          <h2>{host?.name ?? "Local host"}</h2>
        </div>
        <span className={`state state-${health?.status === "ok" ? "running" : "error"}`}>
          {health?.status ?? "unknown"}
        </span>
      </div>
      <dl className="details">
        <dt>CPU</dt>
        <dd>{host?.cpuCount ?? 0} cores</dd>
        <dt>Memory</dt>
        <dd>{formatBytes(host?.memoryBytes ?? 0)}</dd>
        <dt>Disk free</dt>
        <dd>{formatBytes(host?.diskFreeBytes ?? 0)}</dd>
      </dl>
      <div className="health-list">
        {health?.checks.map((check) => (
          <div className="activity-row" key={check.name}>
            <span className={`dot dot-${check.status === "ok" ? "succeeded" : "failed"}`} />
            <div>
              <strong>{check.name}</strong>
              <small>{check.message}</small>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function Operations({ operations }: { operations: Operation[] }) {
  return (
    <div className="panel">
      <div className="panel-title">
        <div>
          <span className="eyebrow">Activity</span>
          <h2>Recent operations</h2>
        </div>
      </div>
      <div className="activity">
        {operations.length === 0 && <div className="empty">No operations yet.</div>}
        {operations.map((operation) => (
          <div className="activity-row" key={operation.id}>
            <span className={`dot dot-${operation.status}`} />
            <div>
              <strong>{operation.action}</strong>
              <small>{operation.message || operation.status}</small>
            </div>
            <time>{new Date(operation.createdAt).toLocaleTimeString()}</time>
          </div>
        ))}
      </div>
    </div>
  );
}

function AuditLog({ events }: { events: AuditEvent[] }) {
  return (
    <div className="panel">
      <div className="panel-title">
        <div>
          <span className="eyebrow">Security</span>
          <h2>Audit events</h2>
        </div>
      </div>
      <div className="activity">
        {events.length === 0 && <div className="empty">No audit events visible for this role.</div>}
        {events.map((event) => (
          <div className="activity-row" key={event.id}>
            <span className={`dot dot-${event.outcome === "succeeded" || event.outcome === "accepted" ? "succeeded" : "failed"}`} />
            <div>
              <strong>{event.action}</strong>
              <small>
                {event.actorName || "system"} · {event.resource}
                {event.resourceId ? `/${event.resourceId}` : ""} · {event.message}
              </small>
            </div>
            <time>{new Date(event.createdAt).toLocaleTimeString()}</time>
          </div>
        ))}
      </div>
    </div>
  );
}

function ReadOnlyNotice() {
  return (
    <div className="panel">
      <div className="panel-title">
        <div>
          <span className="eyebrow">Access</span>
          <h2>Read-only session</h2>
        </div>
      </div>
      <div className="empty">Your role can view inventory and health, but cannot access audit events or perform lifecycle actions.</div>
    </div>
  );
}

const PROGRESS_STATES: Record<string, string> = {
  provisioning: "Fetching Android image (cvd fetch)…",
  starting: "Launching Cuttlefish device…",
  booting: "Waiting for Android to boot…",
};

function ConsolePanel({ instance }: { instance?: Instance }) {
  const canOpen = instance?.state === "running";
  const progressLabel = instance ? PROGRESS_STATES[instance.state] : undefined;
  return (
    <section className="panel console-panel">
      <div className="panel-title">
        <div>
          <span className="eyebrow">Interactive console</span>
          <h2>{instance ? `${instance.name} · ${instance.deviceId || "device"}` : "Cuttlefish WebRTC"}</h2>
        </div>
        {instance && (
          <a className={`open-link ${canOpen ? "" : "disabled-link"}`} href={canOpen ? instance.consoleUrl : undefined} target="_blank" rel="noreferrer">
            Open in new tab
          </a>
        )}
      </div>
      {instance && canOpen ? (
        <iframe className="console-frame" title={`${instance.name} console`} src={instance.consoleUrl} allow="autoplay; microphone; camera; clipboard-write" />
      ) : progressLabel ? (
        <div className="console-progress">
          <div className="spinner" />
          <strong>{progressLabel}</strong>
          <small>This can take several minutes the first time an image is downloaded.</small>
        </div>
      ) : instance?.state === "error" ? (
        <div className="alert">Deployment failed: {instance.lastError || "unknown error"}</div>
      ) : instance ? (
        <div className="empty">Start the instance to open its interactive console.</div>
      ) : (
        <div className="empty">Select an instance to load its interactive console.</div>
      )}
    </section>
  );
}

function hasPermission(principal: Principal, permission: string) {
  return principal.permissions.includes("admin") || principal.permissions.includes(permission);
}

const NAV_LABELS: Record<string, string> = {
  dashboard: "Overview",
  instances: "Instances",
  images: "Images",
  operations: "Activity",
  host: "Host",
  audit: "Audit",
  settings: "Settings",
};

const PAGE_TITLES: Record<string, string> = {
  dashboard: "Overview",
  instances: "Android instances",
  images: "Images",
  operations: "Activity log",
  host: "Host health",
  audit: "Audit events",
  settings: "Settings",
};

const NAV_GROUP_DEFS: { title: string; items: string[] }[] = [
  { title: "System", items: ["dashboard", "instances", "images", "operations"] },
  { title: "Tools", items: ["host", "audit", "settings"] },
];

function navGroups(principal: Principal) {
  const allowed = new Set(visibleViews(principal));
  return NAV_GROUP_DEFS.map((group) => ({
    title: group.title,
    items: group.items.filter((item) => allowed.has(item)),
  })).filter((group) => group.items.length > 0);
}

function visibleViews(principal: Principal) {
  const base = ["dashboard", "host", "images", "instances", "operations", "settings"];
  if (hasPermission(principal, "admin")) {
    return [...base, "audit"];
  }
  return base;
}

function formatBytes(bytes: number) {
  if (!bytes) {
    return "Unknown";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}
