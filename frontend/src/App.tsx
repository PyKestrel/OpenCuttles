import { FormEvent, useEffect, useMemo, useState } from "react";
import { api } from "./api";
import type { AuditEvent, HealthReport, Host, Image, Instance, Operation, Principal } from "./types";

type LoadState = {
  host?: Host;
  images: Image[];
  instances: Instance[];
  operations: Operation[];
  audit: AuditEvent[];
  health?: HealthReport;
};

const initialState: LoadState = {
  images: [],
  instances: [],
  operations: [],
  audit: [],
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
    const [host, images, instances, operations, health, audit] = await Promise.all([
      api.host(),
      api.images(),
      api.instances(),
      api.operations(),
      api.health(),
      canAdmin ? api.audit().catch(() => []) : Promise.resolve([]),
    ]);
    setData({ host, images, instances, operations, health, audit });
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
    const timer = window.setInterval(() => {
      refresh().catch((err: Error) => setError(err.message));
    }, 5000);
    return () => window.clearInterval(timer);
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
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">OC</div>
          <div>
            <strong>OpenCuttles</strong>
            <span>Android Virtualization</span>
          </div>
        </div>
        <nav>
          {visibleViews(principal).map((item) => (
            <button className={view === item ? "active" : ""} key={item} onClick={() => setView(item)}>
              {item}
            </button>
          ))}
        </nav>
        <div className="inventory">
          <h3>Inventory</h3>
          <button className="tree-node">Host: {data.host?.name ?? "local"}</button>
          {data.instances.map((instance) => (
            <button
              className={`tree-node child ${selectedInstance?.id === instance.id ? "selected" : ""}`}
              key={instance.id}
              onClick={() => setSelectedInstanceId(instance.id)}
            >
              {instance.name}
            </button>
          ))}
        </div>
      </aside>

      <main>
        <header className="topbar">
          <div>
            <span className="eyebrow">Single-host MVP</span>
            <h1>Android device control plane</h1>
          </div>
          <div className="topbar-actions">
            <span className="user-pill">{principal.displayName} · {principal.role}</span>
            <button disabled={busy} onClick={() => runAction(refresh)}>Refresh</button>
            <button className="primary" onClick={logout}>Logout</button>
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

        <section className="tabs">
          {visibleViews(principal).map((item) => (
            <button className={view === item ? "active-tab" : ""} key={item} onClick={() => setView(item)}>
              {item}
            </button>
          ))}
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
            <CreateForms images={data.images} busy={busy} canOperate={hasPermission(principal, "operate")} onAction={runAction} />
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
    return <div className="empty">No Android devices yet. Register an image and create an instance.</div>;
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

function CreateForms({
  images,
  busy,
  canOperate,
  onAction,
}: {
  images: Image[];
  busy: boolean;
  canOperate: boolean;
  onAction: (action: () => Promise<unknown>) => Promise<void>;
}) {
  const [imageName, setImageName] = useState("");
  const [imagePath, setImagePath] = useState("");
  const [instanceName, setInstanceName] = useState("");
  const [imageId, setImageId] = useState("");
  const [cpuCores, setCpuCores] = useState(2);
  const [memoryMb, setMemoryMb] = useState(4096);

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
        imageId,
        cpuCores,
        memoryMb,
      }),
    );
    setInstanceName("");
  }

  return (
    <div className="forms">
      <form onSubmit={submitImage}>
        <h3>Register image</h3>
        <label>
          Name
          <input value={imageName} onChange={(event) => setImageName(event.target.value)} placeholder="AOSP main" />
        </label>
        <label>
          Image path
          <input value={imagePath} onChange={(event) => setImagePath(event.target.value)} placeholder="/var/lib/cuttlefish/images/aosp" />
        </label>
        <button className="primary" disabled={!canOperate || busy || !imageName || !imagePath}>
          Register image
        </button>
      </form>

      <form onSubmit={submitInstance}>
        <h3>Create instance</h3>
        <label>
          Name
          <input
            value={instanceName}
            onChange={(event) => setInstanceName(event.target.value)}
            placeholder="android-01"
          />
        </label>
        <label>
          Image
          <select value={imageId} onChange={(event) => setImageId(event.target.value)}>
            <option value="">Select image</option>
            {images.map((image) => (
              <option value={image.id} key={image.id}>
                {image.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          CPU cores
          <input type="number" min="1" max="16" value={cpuCores} onChange={(event) => setCpuCores(Number(event.target.value))} />
        </label>
        <label>
          Memory MB
          <input type="number" min="1024" step="512" value={memoryMb} onChange={(event) => setMemoryMb(Number(event.target.value))} />
        </label>
        <button className="primary" disabled={!canOperate || busy || !instanceName || !imageId}>
          Create instance
        </button>
      </form>
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

function ConsolePanel({ instance }: { instance?: Instance }) {
  const canOpen = instance?.state === "running";
  return (
    <section className="panel console-panel">
      <div className="panel-title">
        <div>
          <span className="eyebrow">Console</span>
          <h2>{instance ? `${instance.name} WebRTC` : "Cuttlefish WebRTC"}</h2>
        </div>
        {instance && (
          <a className={`open-link ${canOpen ? "" : "disabled-link"}`} href={canOpen ? instance.consoleUrl : undefined} target="_blank" rel="noreferrer">
            Open console
          </a>
        )}
      </div>
      {instance && canOpen ? (
        <iframe title={`${instance.name} console`} src={instance.consoleUrl} />
      ) : instance ? (
        <div className="empty">Start the instance before opening its WebRTC console.</div>
      ) : (
        <div className="empty">Select an instance to load its WebRTC console.</div>
      )}
    </section>
  );
}

function hasPermission(principal: Principal, permission: string) {
  return principal.permissions.includes("admin") || principal.permissions.includes(permission);
}

function visibleViews(principal: Principal) {
  const base = ["dashboard", "host", "images", "instances", "operations", "settings"];
  if (hasPermission(principal, "admin")) {
    return [...base.slice(0, 5), "audit", "settings"];
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
