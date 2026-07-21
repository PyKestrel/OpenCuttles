import { useEffect, useRef, useState, type FormEvent, type MouseEvent } from "react";
import { cn } from "@/lib/utils";
import { AgentTab } from "@/components/device/AgentTab";
import { api } from "@/api";
import { isLive, usesRunnerTunnel } from "@/lib/platform";
import type { Instance } from "@/types";

type Pane = "controls" | "agent";

const KEYS = ["ENTER", "TAB", "ESC", "BACKSPACE", "DELETE", "UP", "DOWN", "LEFT", "RIGHT"];

// A screenshot-based operation workspace: a live, clickable screenshot on the
// left with a Controls / Agent switcher on the right.
//
// Used by every device without a WebRTC stream — desktops driven over the runner
// tunnel, and physical Android handsets driven over ADB. The component was
// already transport-agnostic (it goes through the same control routes either
// way); only its name and its offline message assumed a desktop.
export function ScreenshotConsole({
  instance,
  canControl,
  pane,
  onPane,
}: {
  instance: Instance;
  canControl: boolean;
  pane: Pane;
  onPane: (p: Pane) => void;
}) {
  const online = isLive(instance);
  // Why a device is offline depends on how it is reached, and the fix is
  // completely different: start a runner, or plug in a phone and accept the
  // debugging prompt.
  const offlineHelp = usesRunnerTunnel(instance)
    ? "This device is offline. Start its runner — the Configure tab shows the install command — to bring it online."
    : "This device is offline. Check that it is connected and that USB debugging is authorized on the device itself; the appliance polls for it every few seconds.";

  return (
    <div className="flex flex-col gap-4 xl:h-[720px] xl:flex-row">
      <div
        className={cn(
          "min-h-[440px] overflow-hidden rounded-xl border bg-black xl:min-h-0",
          canControl ? "xl:flex-[1.3]" : "xl:flex-1",
        )}
        style={{ borderColor: "var(--border-strong)" }}
      >
        {online ? (
          <ScreenView instance={instance} />
        ) : (
          <div className="grid size-full place-items-center bg-secondary/40 px-6 text-center text-[13.5px] text-muted-foreground">
            <div className="max-w-xs space-y-2">
              <p>{offlineHelp}</p>
              {instance.lastError ? (
                // The poller records exactly why — an unaccepted debugging
                // prompt reads very differently from a missing cable.
                <p className="text-[12.5px] text-muted-foreground/80">{instance.lastError}</p>
              ) : null}
            </div>
          </div>
        )}
      </div>

      {canControl && (
        <div className="flex min-h-0 flex-col xl:flex-1">
          <div className="mb-3 inline-flex self-start rounded-lg border p-0.5" style={{ background: "var(--secondary)" }}>
            {(["controls", "agent"] as Pane[]).map((p) => (
              <button
                key={p}
                onClick={() => onPane(p)}
                className={cn(
                  "rounded-md px-4 py-1.5 text-[13px] font-medium capitalize transition-colors",
                  pane === p ? "text-foreground" : "text-muted-foreground hover:text-foreground",
                )}
                style={pane === p ? { background: "var(--card)", boxShadow: "var(--card-shadow)" } : undefined}
              >
                {p}
              </button>
            ))}
          </div>
          <div className="min-h-0 flex-1 xl:overflow-auto">
            {pane === "controls" ? <DesktopControls instance={instance} /> : <AgentTab instance={instance} />}
          </div>
        </div>
      )}
    </div>
  );
}

function ScreenView({ instance }: { instance: Instance }) {
  const [token, setToken] = useState(() => Date.now());
  const [failed, setFailed] = useState(false);
  const imgRef = useRef<HTMLImageElement>(null);

  useEffect(() => {
    const t = window.setInterval(() => {
      if (!document.hidden) setToken(Date.now());
    }, 1500);
    return () => window.clearInterval(t);
  }, []);

  function onClick(e: MouseEvent<HTMLImageElement>) {
    const img = imgRef.current;
    if (!img || !img.naturalWidth || !img.naturalHeight) return;
    const rect = img.getBoundingClientRect();
    // object-contain letterboxes the image: it is scaled to fit and centered, so
    // the rendered image is smaller than the element box. Map clicks against the
    // actual image rect, not the element box, or they land offset.
    const scale = Math.min(rect.width / img.naturalWidth, rect.height / img.naturalHeight);
    const dispW = img.naturalWidth * scale;
    const dispH = img.naturalHeight * scale;
    const lx = e.clientX - rect.left - (rect.width - dispW) / 2;
    const ly = e.clientY - rect.top - (rect.height - dispH) / 2;
    if (lx < 0 || ly < 0 || lx > dispW || ly > dispH) return; // clicked the letterbox
    const x = Math.round(lx / scale);
    const y = Math.round(ly / scale);
    api.controlTap(instance.id, x, y).catch(() => undefined);
  }

  if (failed) {
    return (
      <div className="grid size-full place-items-center bg-secondary/40 px-6 text-center text-[13px] text-muted-foreground">
        <p className="max-w-xs">Waiting for the first frame from the runner…</p>
      </div>
    );
  }
  return (
    <img
      ref={imgRef}
      src={api.controlScreenshotSrc(instance.id, token)}
      alt={`${instance.name} screen`}
      className="size-full cursor-crosshair object-contain"
      draggable={false}
      onClick={onClick}
      onError={() => setFailed(true)}
      title="Click to interact"
    />
  );
}

function DesktopControls({ instance }: { instance: Instance }) {
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function run(fn: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Action failed");
    } finally {
      setBusy(false);
    }
  }

  function sendText(e: FormEvent) {
    e.preventDefault();
    if (!text) return;
    run(() => api.controlText(instance.id, text)).then(() => setText(""));
  }

  return (
    <div className="space-y-3 rounded-xl border bg-card p-4" style={{ boxShadow: "var(--card-shadow)" }}>
      <div className="text-[13px] font-semibold">Keyboard</div>
      {error && <div className="text-[12px]" style={{ color: "var(--destructive)" }}>{error}</div>}
      <form className="flex gap-2" onSubmit={sendText}>
        <input
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Type into the focused field…"
          className="min-w-0 flex-1 rounded-lg border bg-secondary px-3 py-2 text-[13px] outline-none focus:border-[var(--ring)]"
        />
        <button disabled={busy || !text} className="rounded-lg px-3.5 py-2 text-[13px] font-medium text-primary-foreground disabled:opacity-50" style={{ background: "var(--primary-strong)" }}>
          Send
        </button>
      </form>
      <div className="flex flex-wrap gap-1.5">
        {KEYS.map((k) => (
          <button
            key={k}
            disabled={busy}
            onClick={() => run(() => api.controlKey(instance.id, k))}
            className="rounded-lg border bg-secondary px-2.5 py-1.5 text-[12px] font-medium hover:bg-accent disabled:opacity-50"
          >
            {k}
          </button>
        ))}
      </div>
      <p className="text-[11.5px] text-muted-foreground/70">
        Click anywhere on the screen to move and click the mouse. Vision-grounded agent actions work here too.
      </p>
    </div>
  );
}
