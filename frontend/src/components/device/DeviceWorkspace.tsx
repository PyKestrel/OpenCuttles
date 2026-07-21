import { useEffect, useMemo, useState } from "react";
import { Camera, MonitorPlay, Play, Square } from "lucide-react";
import { cn } from "@/lib/utils";
import { can } from "@/lib/permissions";
import { isLive, isProvisioned, platformIcon } from "@/lib/platform";
import { FadeIn } from "@/components/Motion";
import { SummaryTab } from "@/components/device/SummaryTab";
import { LogsTab } from "@/components/device/LogsTab";
import { ConfigureTab } from "@/components/device/ConfigureTab";
import { ConsoleWorkspace, type ConsolePane } from "@/components/device/ConsoleWorkspace";
import { ScreenshotConsole } from "@/components/device/ScreenshotConsole";
import { TestsPanel } from "@/components/tests/TestsPanel";
import { api } from "@/api";
import type { Instance, Principal, TestRun } from "@/types";

export type DeviceTab = "summary" | "console" | "tests" | "logs" | "configure";

const TAB_LABELS: Record<DeviceTab, string> = {
  summary: "Summary",
  console: "Console",
  tests: "Tests",
  logs: "Logs",
  configure: "Configure",
};

export function DeviceWorkspace({
  instance,
  instances,
  principal,
  busy,
  onStart,
  onStop,
  onDelete,
}: {
  instance: Instance;
  instances: Instance[];
  principal: Principal;
  busy: boolean;
  onStart: (id: string) => void;
  onStop: (id: string) => void;
  onDelete: (id: string) => void;
}) {
  const [tab, setTab] = useState<DeviceTab>("summary");
  const [consolePane, setConsolePane] = useState<ConsolePane>("controls");
  const [latestRun, setLatestRun] = useState<TestRun>();

  const canControl = can(principal, "control");
  const canTest = can(principal, "test");
  const canOperate = can(principal, "operate");
  // Enrollment credentials grant input on a real machine, so managing them
  // is admin-only — matching the API routes.
  const canAdmin = can(principal, "admin");
  // Three independent questions that the old single `isDesktop` conflated.
  // A physical Android handset makes the difference visible: it is not a
  // desktop, but it also has no start/stop lifecycle and no WebRTC console.
  const provisioned = isProvisioned(instance);           // has start/stop
  const webrtcConsole = instance.consoleProvider === "cuttlefish-webrtc";
  const isAndroid = (instance.platform || "android") === "android"; // has logcat
  const PlatformIcon = platformIcon(instance.platform || "android");

  const tabs = useMemo(() => {
    const t: DeviceTab[] = ["summary", "console"];
    if (canTest) t.push("tests");
    if (canControl && isAndroid) t.push("logs"); // logcat is Android-only
    t.push("configure");
    return t;
  }, [canControl, canTest, isAndroid]);

  // Summary shortcuts can jump straight to a console pane (Controls or Agent).
  function openTab(next: DeviceTab, pane?: ConsolePane) {
    setTab(next);
    if (pane) setConsolePane(pane);
  }

  useEffect(() => setTab("summary"), [instance.id]);

  useEffect(() => {
    let live = true;
    api
      .testRuns()
      .then((runs) => {
        if (live) setLatestRun(runs.find((r) => r.instanceId === instance.id));
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
          className="grid place-items-center rounded-lg border text-primary"
          style={{ width: 30, height: 30, background: "var(--brand-weak)", borderColor: "color-mix(in srgb, var(--primary) 30%, transparent)" }}
        >
          <PlatformIcon className="size-4" />
        </span>
        <h1 className="text-[18px] font-semibold tracking-tight">{instance.name}</h1>
        <span className="font-mono text-[12px] text-muted-foreground/70">
          {provisioned ? instance.deviceId || instance.id : instance.adbTarget || instance.platform}
        </span>
        <span className="mx-1 w-px" style={{ background: "var(--border)", height: 22 }} />
        {!provisioned ? (
          // Nothing to start or stop: a desktop comes online when its runner
          // dials home, a handset when ADB can reach it.
          <span className="inline-flex items-center gap-1.5 text-[12.5px] font-medium" style={{ color: isLive(instance) ? "var(--running)" : "var(--stopped)" }}>
            <span className="size-2 rounded-full" style={{ background: isLive(instance) ? "var(--running)" : "var(--stopped)" }} />
            {isLive(instance) ? "Online" : "Offline"}
          </span>
        ) : (
          <div className="flex gap-0.5">
            <HeaderBtn title="Start" disabled={running || busy || !canOperate} hover="running" onClick={() => onStart(instance.id)}>
              <Play className="size-4" fill="currentColor" stroke="none" />
            </HeaderBtn>
            <HeaderBtn title="Stop" disabled={stopped || busy || !canOperate} hover="destructive" onClick={() => onStop(instance.id)}>
              <Square className="size-4" fill="currentColor" stroke="none" />
            </HeaderBtn>
            <HeaderBtn title="Console" disabled={!running} onClick={() => window.open(instance.consoleUrl, "_blank")}>
              <MonitorPlay className="size-4" />
            </HeaderBtn>
            <HeaderBtn title="Snapshot (coming soon)" disabled>
              <Camera className="size-4" />
            </HeaderBtn>
          </div>
        )}
        <button
          onClick={() => setTab("configure")}
          className="ml-auto flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-[13px] font-medium text-primary hover:bg-accent"
          style={{ borderColor: "var(--border-strong)" }}
        >
          Configure
        </button>
      </div>

      {/* tabs */}
      <nav className="flex gap-0.5 overflow-auto border-b bg-surface px-4">
        {tabs.map((id) => (
          <button
            key={id}
            onClick={() => setTab(id)}
            className={cn(
              "whitespace-nowrap border-b-2 border-b-transparent px-3 py-2.5 text-[13.5px] text-muted-foreground hover:text-foreground",
              tab === id && "border-b-primary text-foreground",
            )}
          >
            {TAB_LABELS[id]}
          </button>
        ))}
      </nav>

      {/* content */}
      <div className="flex-1 overflow-auto p-5">
        <FadeIn id={tab}>
          {tab === "summary" && <SummaryTab instance={instance} latestRun={latestRun} onOpenTab={openTab} />}
          {tab === "console" &&
            // Keyed on the console the device actually offers, not on its
            // platform: a physical handset is Android but has no WebRTC stream,
            // so the old platform test would have sent it to the wrong console.
            (webrtcConsole ? (
              <ConsoleWorkspace instance={instance} canControl={canControl} pane={consolePane} onPane={setConsolePane} />
            ) : (
              <ScreenshotConsole instance={instance} canControl={canControl} pane={consolePane} onPane={setConsolePane} />
            ))}
          {tab === "logs" && <LogsTab instance={instance} />}
          {tab === "tests" && <TestsPanel instance={instance} instances={instances} scoped />}
          {tab === "configure" && (
            <ConfigureTab instance={instance} busy={busy} canOperate={canOperate} canAdmin={canAdmin} onDelete={onDelete} />
          )}
        </FadeIn>
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
        "grid place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground disabled:pointer-events-none disabled:opacity-35",
        hover === "running" && "hover:text-[var(--running)]",
        hover === "destructive" && "hover:text-[var(--destructive)]",
      )}
      style={{ width: 30, height: 30 }}
    >
      {children}
    </button>
  );
}
