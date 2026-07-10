import { useEffect, useState } from "react";
import { Camera, MonitorPlay, MoreVertical, Play, Smartphone, Square } from "lucide-react";
import { cn } from "@/lib/utils";
import { SummaryTab } from "@/components/device/SummaryTab";
import { api } from "@/api";
import type { Instance, TestRun } from "@/types";

export type DeviceTab = "summary" | "console" | "controls" | "agent" | "tests" | "logs" | "configure";

const TABS: { id: DeviceTab; label: string }[] = [
  { id: "summary", label: "Summary" },
  { id: "console", label: "Console" },
  { id: "controls", label: "Controls" },
  { id: "agent", label: "Agent" },
  { id: "tests", label: "Tests" },
  { id: "logs", label: "Logs" },
  { id: "configure", label: "Configure" },
];

export function DeviceWorkspace({
  instance,
  busy,
  onStart,
  onStop,
}: {
  instance: Instance;
  busy: boolean;
  onStart: (id: string) => void;
  onStop: (id: string) => void;
}) {
  const [tab, setTab] = useState<DeviceTab>("summary");
  const [latestRun, setLatestRun] = useState<TestRun>();

  useEffect(() => setTab("summary"), [instance.id]);

  useEffect(() => {
    let live = true;
    api
      .testRuns()
      .then((runs) => {
        if (live) {
          setLatestRun(runs.find((r) => r.instanceId === instance.id));
        }
      })
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [instance.id]);

  const running = instance.state === "running";
  const stopped = instance.state === "stopped" || instance.state === "error";

  return (
    <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
      {/* object header */}
      <div className="flex items-center gap-3 border-b bg-surface px-5 py-3.5">
        <span
          className="grid size-7.5 place-items-center rounded-lg border text-primary"
          style={{ width: 30, height: 30, background: "var(--brand-weak)", borderColor: "color-mix(in srgb, var(--primary) 30%, transparent)" }}
        >
          <Smartphone className="size-4" />
        </span>
        <h1 className="text-[18px] font-semibold tracking-tight">{instance.name}</h1>
        <span className="font-mono text-[12px] text-muted-foreground/70">{instance.deviceId || instance.id}</span>
        <span className="mx-1 h-5.5 w-px" style={{ background: "var(--border)", height: 22 }} />
        <div className="flex gap-0.5">
          <HeaderBtn title="Start" disabled={running || busy} hover="running" onClick={() => onStart(instance.id)}>
            <Play className="size-4" fill="currentColor" stroke="none" />
          </HeaderBtn>
          <HeaderBtn title="Stop" disabled={stopped || busy} hover="destructive" onClick={() => onStop(instance.id)}>
            <Square className="size-4" fill="currentColor" stroke="none" />
          </HeaderBtn>
          <HeaderBtn title="Console" disabled={!running} onClick={() => window.open(instance.consoleUrl, "_blank")}>
            <MonitorPlay className="size-4" />
          </HeaderBtn>
          <HeaderBtn title="Snapshot" disabled>
            <Camera className="size-4" />
          </HeaderBtn>
        </div>
        <button className="ml-auto flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-[13px] font-medium text-primary hover:bg-accent" style={{ borderColor: "var(--border-strong)" }}>
          <MoreVertical className="size-3.5" /> Actions
        </button>
      </div>

      {/* tabs */}
      <nav className="flex gap-0.5 overflow-auto border-b bg-surface px-4">
        {TABS.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={cn(
              "whitespace-nowrap border-b-2 border-b-transparent px-3 py-2.5 text-[13.5px] text-muted-foreground hover:text-foreground",
              tab === t.id && "border-b-primary text-foreground",
            )}
          >
            {t.label}
          </button>
        ))}
      </nav>

      {/* content */}
      <div className="flex-1 overflow-auto p-5">
        {tab === "summary" && <SummaryTab instance={instance} latestRun={latestRun} onOpenTab={setTab} />}
        {tab === "console" && <ConsoleTab instance={instance} />}
        {tab !== "summary" && tab !== "console" && <Placeholder tab={tab} />}
      </div>
    </div>
  );
}

function HeaderBtn({
  title,
  disabled,
  hover,
  onClick,
  children,
}: {
  title: string;
  disabled?: boolean;
  hover?: "running" | "destructive";
  onClick?: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      title={title}
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "grid size-7.5 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground disabled:pointer-events-none disabled:opacity-35",
        hover === "running" && "hover:text-[var(--running)]",
        hover === "destructive" && "hover:text-[var(--destructive)]",
      )}
      style={{ width: 30, height: 30 }}
    >
      {children}
    </button>
  );
}

function ConsoleTab({ instance }: { instance: Instance }) {
  if (instance.state !== "running") {
    return <Empty>Start the device to open its interactive console.</Empty>;
  }
  return (
    <iframe
      title={`${instance.name} console`}
      src={instance.consoleUrl}
      allow="autoplay; microphone; camera; clipboard-write"
      className="h-[640px] w-full rounded-xl border bg-black"
    />
  );
}

function Placeholder({ tab }: { tab: DeviceTab }) {
  return (
    <Empty>
      <span className="font-medium capitalize text-foreground">{tab}</span> is being ported into the new workspace in the next phase.
    </Empty>
  );
}

function Empty({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid min-h-[240px] place-items-center rounded-xl border border-dashed bg-secondary/40 px-6 text-center text-[13.5px] text-muted-foreground" style={{ borderColor: "var(--border-strong)" }}>
      <p className="max-w-md">{children}</p>
    </div>
  );
}
