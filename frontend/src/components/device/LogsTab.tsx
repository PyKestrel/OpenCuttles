import { useEffect, useRef, useState } from "react";
import { ScrollText } from "lucide-react";
import { Card, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { api } from "@/api";
import type { Instance } from "@/types";

// Recent logcat for the selected device, with optional live tailing.
export function LogsTab({ instance }: { instance: Instance }) {
  const id = instance.id;
  const running = instance.state === "running";
  const [logcat, setLogcat] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [follow, setFollow] = useState(false);
  const preRef = useRef<HTMLPreElement>(null);

  async function fetchLogcat() {
    setBusy(true);
    setError("");
    try {
      const result = await api.controlLogcat(id, 400);
      setLogcat(result.logcat || "(empty)");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch logcat");
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    setLogcat("");
    setFollow(false);
  }, [id]);

  // Live tail: re-fetch on an interval while following and the tab is visible.
  useEffect(() => {
    if (!follow || !running) return;
    const t = window.setInterval(() => {
      if (!document.hidden) fetchLogcat();
    }, 3000);
    return () => window.clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [follow, running, id]);

  // Keep the newest lines in view when following.
  useEffect(() => {
    if (follow) preRef.current?.scrollTo({ top: preRef.current.scrollHeight });
  }, [logcat, follow]);

  if (!running) {
    return (
      <div className="grid min-h-[240px] place-items-center rounded-xl border border-dashed bg-secondary/40 px-6 text-center text-[13.5px] text-muted-foreground" style={{ borderColor: "var(--border-strong)" }}>
        <p className="max-w-md">Start the device to stream its logcat.</p>
      </div>
    );
  }

  return (
    <Card>
      <CardHeader
        icon={<ScrollText className="size-[15px]" />}
        title="Logcat"
        action={
          <div className="flex items-center gap-2.5">
            <label className="flex cursor-pointer items-center gap-1.5 text-[12px] font-normal text-muted-foreground">
              <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} className="accent-[var(--primary)]" />
              Follow
            </label>
            <Button size="sm" disabled={busy} onClick={fetchLogcat}>{busy ? "Fetching…" : "Refresh"}</Button>
          </div>
        }
      />
      <div className="p-4">
        {error && <div className="mb-2 text-[12px]" style={{ color: "var(--destructive)" }}>{error}</div>}
        <pre ref={preRef} className="h-[560px] overflow-auto rounded-lg border bg-[#06090c] px-3 py-2.5 font-mono text-[11.5px] leading-relaxed text-[#c9d6df]" style={{ borderColor: "var(--border-strong)" }}>
          {logcat || "Click “Refresh” to load the last 400 lines, or enable Follow to tail."}
        </pre>
      </div>
    </Card>
  );
}
