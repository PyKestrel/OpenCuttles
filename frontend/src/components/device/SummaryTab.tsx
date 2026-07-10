import { useEffect, useState } from "react";
import { Cpu, FlaskConical, Info, Smartphone, Sparkles } from "lucide-react";
import { Card, CardHeader } from "@/components/ui/card";
import { StatusDot } from "@/components/StatusDot";
import { api } from "@/api";
import type { DeviceTab } from "@/components/device/DeviceWorkspace";
import type { Instance, TestRun } from "@/types";

export function SummaryTab({
  instance,
  latestRun,
  onOpenTab,
}: {
  instance: Instance;
  latestRun?: TestRun;
  onOpenTab: (tab: DeviceTab, pane?: "controls" | "agent") => void;
}) {
  const running = instance.state === "running";
  const [shotToken, setShotToken] = useState(() => Date.now());
  const [shotFailed, setShotFailed] = useState(false);

  // Gentle live screenshot while running (heavier auto-refresh lives on the Controls tab).
  useEffect(() => {
    if (!running) {
      return;
    }
    const t = window.setInterval(() => {
      if (!document.hidden) {
        setShotToken(Date.now());
      }
    }, 4000);
    return () => window.clearInterval(t);
  }, [running]);

  return (
    <div className="space-y-4">
      <div className="grid items-start gap-4 lg:grid-cols-[1fr_1.15fr]">
        {/* Screen */}
        <Card>
          <CardHeader icon={<Smartphone className="size-[15px]" />} title="Screen" />
          <div className="p-4">
            <div className="mx-auto aspect-[9/17.6] max-w-[160px] overflow-hidden rounded-xl border bg-[#06090c]" style={{ borderColor: "var(--border-strong)" }}>
              {running && !shotFailed ? (
                <img
                  src={api.controlScreenshotSrc(instance.id, shotToken)}
                  alt={`${instance.name} screen`}
                  className="size-full object-cover"
                  draggable={false}
                  onError={() => setShotFailed(true)}
                />
              ) : (
                <div className="grid size-full place-items-center px-4 text-center text-[12px] text-muted-foreground/70">
                  {running ? "Loading screen…" : "Device is not running"}
                </div>
              )}
            </div>
            <div className="mt-3.5 flex gap-2">
              <a
                href={running ? instance.consoleUrl : undefined}
                target="_blank"
                rel="noreferrer"
                aria-disabled={!running}
                className="flex-1 rounded-lg px-3 py-2 text-center text-[12px] font-medium text-primary-foreground data-[off=true]:pointer-events-none data-[off=true]:opacity-50"
                data-off={!running}
                style={{ background: "var(--primary-strong)" }}
              >
                Launch console
              </a>
              <button
                onClick={() => onOpenTab("console", "controls")}
                className="flex-1 rounded-lg border bg-secondary px-3 py-2 text-[12px] font-medium hover:bg-accent"
                style={{ borderColor: "var(--border-strong)" }}
              >
                Open controls
              </button>
            </div>
          </div>
        </Card>

        {/* Device details */}
        <Card>
          <CardHeader
            icon={<Info className="size-[15px]" />}
            title="Device details"
            action={<button className="text-[12px] font-medium text-primary">Edit</button>}
          />
          <div className="px-4 pb-3 pt-1">
            <Detail k="Power state">
              <span className="inline-flex items-center gap-1.5 font-sans font-medium" style={{ color: stateTextColor(instance.state) }}>
                <StatusDot state={instance.state} />
                {cap(instance.state)}
              </span>
            </Detail>
            <Detail k="Android" sans>{instance.androidVersion || "—"}</Detail>
            <Detail k="Device ID">{instance.deviceId || "—"}</Detail>
            <Detail k="ADB">127.0.0.1:{instance.adbPort}</Detail>
            <Detail k="WebRTC console">operator :{instance.webrtcPort}</Detail>
            <Detail k="Display">
              {instance.displayWidth && instance.displayHeight
                ? `${instance.displayWidth} × ${instance.displayHeight} · ${instance.dpi} dpi`
                : "—"}
            </Detail>
            <Detail k="Resources">
              {instance.cpuCores} vCPU · {instance.memoryMb} MB
            </Detail>
            <Detail k="Image" sans>{instance.imageId || "—"}</Detail>
            {instance.state === "error" && instance.lastError && (
              <Detail k="Last error" sans>
                <span style={{ color: "var(--destructive)" }}>{instance.lastError}</span>
              </Detail>
            )}
          </div>
        </Card>
      </div>

      {/* restrained bento row */}
      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader
            icon={<FlaskConical className="size-[15px]" />}
            title="Latest test"
            action={
              latestRun ? (
                <span className="rounded-[5px] px-1.5 font-mono text-[10.5px]" style={badgeStyle(latestRun.passed)}>
                  {latestRun.status.toUpperCase()}
                </span>
              ) : undefined
            }
          />
          <div className="p-4">
            {latestRun ? (
              <>
                <div className="text-[13px] font-medium">{latestRun.testName || latestRun.testId}</div>
                <div className="mt-0.5 text-[12px] text-muted-foreground/80">
                  {latestRun.steps.filter((s) => s.pass).length}/{latestRun.steps.length} steps
                </div>
                <div className="mt-3 flex gap-1.5">
                  {latestRun.steps.map((s) => (
                    <span key={s.index} className="h-1.5 flex-1 rounded-full" style={{ background: s.pass ? "var(--running)" : "var(--destructive)", opacity: 0.85 }} />
                  ))}
                </div>
              </>
            ) : (
              <button onClick={() => onOpenTab("tests")} className="text-[13px] text-primary">
                No runs yet — author a test →
              </button>
            )}
          </div>
        </Card>

        <Card>
          <CardHeader icon={<Sparkles className="size-[15px]" />} title="Agent" action={<span className="text-[12px] text-muted-foreground/70">MiniCPM5-1B</span>} />
          <div className="p-4 text-[13px] text-muted-foreground">
            Drive this device in natural language.
            <button onClick={() => onOpenTab("console", "agent")} className="mt-2 block text-[13px] text-primary">
              Open the agent →
            </button>
          </div>
        </Card>

        <Card>
          <CardHeader icon={<Cpu className="size-[15px]" />} title="Resources" />
          <div className="p-4">
            <div className="font-mono text-[22px] font-bold tabular-nums tracking-tight">
              {instance.cpuCores}
              <span className="ml-1 text-[14px] font-medium text-muted-foreground">vCPU</span>
            </div>
            <div className="text-[12px] text-muted-foreground/80">{instance.memoryMb} MB memory</div>
          </div>
        </Card>
      </div>
    </div>
  );
}

function Detail({ k, sans, children }: { k: string; sans?: boolean; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[130px_1fr] gap-2.5 border-t py-2 text-[13px] first:border-t-0" style={{ borderColor: "var(--hairline)" }}>
      <span className="text-muted-foreground">{k}</span>
      <span className={sans ? "text-[13px]" : "font-mono text-[12.5px]"}>{children}</span>
    </div>
  );
}

function cap(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
function stateTextColor(state: Instance["state"]) {
  if (state === "running") return "var(--running)";
  if (state === "error") return "var(--destructive)";
  return "var(--foreground)";
}
function badgeStyle(pass: boolean): React.CSSProperties {
  const c = pass ? "var(--running)" : "var(--destructive)";
  return { color: c, background: `color-mix(in srgb, ${c} 12%, transparent)`, border: `1px solid color-mix(in srgb, ${c} 30%, transparent)` };
}
