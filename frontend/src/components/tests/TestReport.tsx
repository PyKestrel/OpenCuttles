import { useRef, useState } from "react";
import { Check, Link2, Play, X } from "lucide-react";
import { api } from "@/api";
import type { StepResult, TestRun } from "@/types";

// A replayable run report: video with per-step seek + a grounded step timeline.
export function TestReport({ run }: { run: TestRun }) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [copied, setCopied] = useState(false);

  const passed = run.steps.filter((s) => s.pass).length;
  const total = run.steps.length;

  function copyLink() {
    const url = `${window.location.origin}${window.location.pathname}#run-${run.id}`;
    void navigator.clipboard?.writeText(url);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  }

  function seekToStep(index: number) {
    const video = videoRef.current;
    if (!video || !Number.isFinite(video.duration) || total === 0) return;
    video.currentTime = (index / total) * video.duration;
    void video.play().catch(() => undefined);
  }

  const statusColor =
    run.status === "passed" ? "var(--running)" : run.status === "running" ? "var(--warn)" : "var(--destructive)";

  return (
    <div className="overflow-hidden rounded-xl border bg-card" style={{ boxShadow: "var(--card-shadow)" }}>
      <div className="flex items-center gap-3 border-b px-4 py-3" style={{ borderColor: "var(--hairline)" }}>
        <div className="min-w-0">
          <div className="text-[10.5px] uppercase tracking-[0.06em] text-muted-foreground/70">Report</div>
          <h3 className="truncate text-[14px] font-semibold">{run.testName || run.testId}</h3>
        </div>
        <span className="ml-auto rounded-md px-2 py-0.5 text-[11px] font-semibold uppercase" style={{ color: statusColor, background: `color-mix(in srgb, ${statusColor} 12%, transparent)`, border: `1px solid color-mix(in srgb, ${statusColor} 30%, transparent)` }}>
          {run.status}
        </span>
        <button onClick={copyLink} className="inline-flex items-center gap-1.5 rounded-lg border bg-secondary px-2.5 py-1.5 text-[12px] font-medium hover:bg-accent">
          <Link2 className="size-3.5" /> {copied ? "Copied" : "Share"}
        </button>
      </div>

      <div className="p-4">
        {/* summary bar */}
        <div className="mb-4 flex items-center gap-3">
          <div className="flex flex-1 gap-1">
            {run.steps.map((s) => (
              <span key={s.index} className="h-1.5 flex-1 rounded-full" style={{ background: s.pass ? "var(--running)" : "var(--destructive)", opacity: 0.9 }} />
            ))}
            {total === 0 && <span className="h-1.5 flex-1 rounded-full bg-muted" />}
          </div>
          <span className="font-mono text-[12px] tabular-nums text-muted-foreground">{passed}/{total} steps</span>
        </div>

        {run.error && (
          <div className="mb-4 rounded-lg border px-3 py-2 text-[13px]" style={{ borderColor: "color-mix(in srgb, var(--destructive) 35%, transparent)", background: "color-mix(in srgb, var(--destructive) 10%, transparent)", color: "var(--destructive)" }}>
            {run.error}
          </div>
        )}

        {run.video && (
          <video
            ref={videoRef}
            className="mb-4 w-full rounded-lg border bg-black"
            style={{ borderColor: "var(--border-strong)", maxHeight: 420 }}
            src={api.testArtifactUrl(run.id, run.video)}
            controls
            muted
          />
        )}

        <ol className="space-y-2.5">
          {run.steps.map((step) => (
            <StepRow key={step.index} run={run} step={step} onSeek={() => seekToStep(step.index)} />
          ))}
        </ol>
        {run.status === "running" && (
          <div className="mt-3 flex items-center gap-2 text-[13px] text-muted-foreground">
            <span className="size-3.5 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-primary" />
            Running…
          </div>
        )}
      </div>
    </div>
  );
}

function StepRow({ run, step, onSeek }: { run: TestRun; step: StepResult; onSeek: () => void }) {
  const [dims, setDims] = useState<{ w: number; h: number }>();
  return (
    <li className="flex gap-3 rounded-lg border p-2.5" style={{ borderColor: "var(--border)", background: "var(--secondary)" }}>
      {step.screenshot && (
        <button onClick={onSeek} title="Jump to this step in the video" className="relative w-[68px] shrink-0 overflow-hidden rounded-md border bg-[#06090c]" style={{ borderColor: "var(--border-strong)" }}>
          <img
            src={api.testArtifactUrl(run.id, step.screenshot)}
            alt={`step ${step.index + 1}`}
            loading="lazy"
            className="block w-full"
            onLoad={(e) => setDims({ w: e.currentTarget.naturalWidth, h: e.currentTarget.naturalHeight })}
          />
          {dims && step.x !== undefined && step.x > 0 && (
            <span
              className="absolute size-3 -translate-x-1/2 -translate-y-1/2 rounded-full ring-2 ring-white/80"
              style={{ left: `${(step.x / dims.w) * 100}%`, top: `${((step.y ?? 0) / dims.h) * 100}%`, background: "var(--primary)" }}
            />
          )}
          <span className="absolute inset-x-0 top-0 grid place-items-center bg-black/40 opacity-0 transition-opacity hover:opacity-100">
            <Play className="size-4 text-white" fill="currentColor" />
          </span>
        </button>
      )}
      <div className="min-w-0 flex-1">
        <div className="flex items-start gap-2">
          <span className="text-[13px] font-medium leading-snug">
            <span className="text-muted-foreground/60">{step.index + 1}.</span> {step.text}
          </span>
          <span className="ml-auto inline-flex shrink-0 items-center gap-1 rounded-md px-1.5 py-0.5 text-[10.5px] font-semibold uppercase" style={badge(step.pass)}>
            {step.pass ? <Check className="size-3" /> : <X className="size-3" />}
            {step.pass ? "pass" : "fail"}
          </span>
        </div>
        <div className="mt-1 flex flex-wrap gap-x-2.5 gap-y-0.5 font-mono text-[11.5px] text-muted-foreground/80">
          <span>{step.verb}</span>
          {step.target && <span>· {step.target}</span>}
          <span>· {step.durationMs} ms</span>
          {step.battery ? <span>· 🔋{step.battery}%</span> : null}
        </div>
        {step.detail && <div className="mt-1 text-[12px] text-muted-foreground">{step.detail}</div>}
        {step.modelOutput && <div className="mt-1 line-clamp-2 text-[11.5px] italic text-muted-foreground/70">{step.modelOutput.slice(0, 220)}</div>}
      </div>
    </li>
  );
}

function badge(pass: boolean): React.CSSProperties {
  const c = pass ? "var(--running)" : "var(--destructive)";
  return { color: c, background: `color-mix(in srgb, ${c} 12%, transparent)`, border: `1px solid color-mix(in srgb, ${c} 30%, transparent)` };
}
