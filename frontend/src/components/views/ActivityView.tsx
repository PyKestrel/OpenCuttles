import { useCallback, useEffect, useState } from "react";
import { Activity, HeartPulse, ListChecks, ShieldCheck, UserRound } from "lucide-react";
import { Card, CardHeader } from "@/components/ui/card";
import { AgentModelSettings } from "@/components/views/AgentModelSettings";
import { api } from "@/api";
import type { AuditEvent, HealthReport, Host, Operation, Principal } from "@/types";
import { can } from "@/lib/permissions";

function formatBytes(bytes: number) {
  if (!bytes) return "Unknown";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

export function ActivityView({ principal, host }: { principal: Principal; host?: Host }) {
  const [operations, setOperations] = useState<Operation[]>([]);
  const [audit, setAudit] = useState<AuditEvent[]>([]);
  const [health, setHealth] = useState<HealthReport>();
  const isAdmin = can(principal, "admin");

  const refresh = useCallback(async () => {
    const [ops, h] = await Promise.all([api.operations().catch(() => []), api.health().catch(() => undefined)]);
    setOperations(ops ?? []);
    if (h) setHealth(h);
    if (isAdmin) setAudit((await api.audit().catch(() => [])) ?? []);
  }, [isAdmin]);

  useEffect(() => {
    refresh();
    const t = window.setInterval(() => !document.hidden && refresh(), 8000);
    return () => window.clearInterval(t);
  }, [refresh]);

  const prerequisites = host?.prerequisites ?? [];
  const executionMode = health?.checks.find((c) => c.name === "execution_mode")?.message;

  return (
    <div className="mx-auto max-w-6xl space-y-4 p-5">
      {isAdmin && <AgentModelSettings />}
      <div className="grid items-start gap-4 lg:grid-cols-3">
        <Card>
          <CardHeader icon={<HeartPulse className="size-[15px]" />} title="Host health" action={<StatusPill ok={health?.status === "ok"} label={health?.status ?? "unknown"} />} />
          <div className="px-4 pb-3 pt-1">
            <KV k="Host">{host?.name ?? "local host"}</KV>
            <KV k="CPU">{host?.cpuCount ?? 0} cores</KV>
            <KV k="Memory">{formatBytes(host?.memoryBytes ?? 0)}</KV>
            <KV k="Disk free">{formatBytes(host?.diskFreeBytes ?? 0)}</KV>
            <KV k="Execution mode">{executionMode ?? "unknown"}</KV>
          </div>
        </Card>

        <Card>
          <CardHeader icon={<ShieldCheck className="size-[15px]" />} title="Prerequisites" action={<span className="text-[12px] text-muted-foreground/70">{prerequisites.filter((p) => p.ok).length}/{prerequisites.length}</span>} />
          <div className="p-2">
            {prerequisites.length === 0 && <div className="px-3 py-6 text-center text-[12.5px] text-muted-foreground/70">No prerequisite data.</div>}
            {prerequisites.map((item) => (
              <div key={item.name} className="flex items-start gap-2.5 px-2.5 py-1.5">
                <span className="mt-1 size-2 shrink-0 rounded-full" style={{ background: item.ok ? "var(--running)" : "var(--destructive)" }} />
                <div className="min-w-0">
                  <div className="text-[12.5px] font-medium">{item.name}</div>
                  <div className="text-[11.5px] text-muted-foreground/80">{item.detail}{!item.ok && item.remedy ? ` — ${item.remedy}` : ""}</div>
                </div>
              </div>
            ))}
          </div>
        </Card>

        <Card>
          <CardHeader icon={<UserRound className="size-[15px]" />} title="Session" />
          <div className="px-4 pb-3 pt-1">
            <KV k="Display name">{principal.displayName}</KV>
            <KV k="Username">{principal.username}</KV>
            <KV k="Role">{principal.role}</KV>
            <KV k="Permissions" small>{principal.permissions.join(", ")}</KV>
          </div>
        </Card>
      </div>

      <div className="grid items-start gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader icon={<Activity className="size-[15px]" />} title="Recent operations" />
          <div className="max-h-[420px] overflow-auto p-2">
            {operations.length === 0 && <div className="px-3 py-8 text-center text-[13px] text-muted-foreground/70">No operations yet.</div>}
            {operations.map((op) => (
              <Row key={op.id} ok={op.status === "succeeded"} failed={op.status === "failed"} title={op.action} sub={op.message || op.status} time={op.createdAt} />
            ))}
          </div>
        </Card>

        <Card>
          <CardHeader icon={<ListChecks className="size-[15px]" />} title="Audit events" />
          <div className="max-h-[420px] overflow-auto p-2">
            {!isAdmin ? (
              <div className="px-3 py-8 text-center text-[13px] text-muted-foreground/70">Audit events require an admin role.</div>
            ) : audit.length === 0 ? (
              <div className="px-3 py-8 text-center text-[13px] text-muted-foreground/70">No audit events.</div>
            ) : (
              audit.map((e) => (
                <Row
                  key={e.id}
                  ok={e.outcome === "succeeded" || e.outcome === "accepted"}
                  failed={!(e.outcome === "succeeded" || e.outcome === "accepted")}
                  title={e.action}
                  sub={`${e.actorName || "system"} · ${e.resource}${e.resourceId ? `/${e.resourceId}` : ""}${e.message ? ` · ${e.message}` : ""}`}
                  time={e.createdAt}
                />
              ))
            )}
          </div>
        </Card>
      </div>
    </div>
  );
}

function Row({ ok, failed, title, sub, time }: { ok: boolean; failed: boolean; title: string; sub: string; time: string }) {
  const color = ok ? "var(--running)" : failed ? "var(--destructive)" : "var(--warn)";
  return (
    <div className="flex items-start gap-2.5 rounded-lg px-2.5 py-2 hover:bg-accent">
      <span className="mt-1 size-2 shrink-0 rounded-full" style={{ background: color }} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-[13px] font-medium">{title}</div>
        <div className="truncate text-[11.5px] text-muted-foreground/70">{sub}</div>
      </div>
      <time className="shrink-0 font-mono text-[10.5px] text-muted-foreground/60">{new Date(time).toLocaleTimeString()}</time>
    </div>
  );
}

function KV({ k, small, children }: { k: string; small?: boolean; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[120px_1fr] gap-2.5 border-t py-2 text-[13px] first:border-t-0" style={{ borderColor: "var(--hairline)" }}>
      <span className="text-muted-foreground">{k}</span>
      <span className={small ? "break-words text-[12px]" : "text-[13px]"}>{children}</span>
    </div>
  );
}

function StatusPill({ ok, label }: { ok: boolean; label: string }) {
  const c = ok ? "var(--running)" : "var(--destructive)";
  return (
    <span className="rounded-md px-1.5 py-0.5 text-[10.5px] font-semibold uppercase" style={{ color: c, background: `color-mix(in srgb, ${c} 12%, transparent)`, border: `1px solid color-mix(in srgb, ${c} 30%, transparent)` }}>
      {label}
    </span>
  );
}
