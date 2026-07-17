import { useCallback, useEffect, useState } from "react";
import { Download, History, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Card, CardHeader } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { TestReport } from "@/components/tests/TestReport";
import { cn } from "@/lib/utils";
import { api } from "@/api";
import type { CycleRun, CycleTotals, TestRun } from "@/types";

async function exportRun(id: string, format: "junit" | "csv" | "xlsx") {
  try {
    await api.exportCycleRun(id, format);
  } catch (err) {
    toast.error(err instanceof Error ? err.message : "Export failed");
  }
}

export function CycleRunsView() {
  const [runs, setRuns] = useState<CycleRun[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [detail, setDetail] = useState<{ run: CycleRun; cases: TestRun[] } | null>(null);

  const refresh = useCallback(async () => {
    try {
      setRuns((await api.cycleRuns()) ?? []);
    } catch {
      /* surfaced elsewhere */
    }
  }, []);
  useEffect(() => {
    refresh();
  }, [refresh]);

  const hasActive = runs.some((r) => r.status === "running");
  useEffect(() => {
    if (!hasActive && !selectedId) return;
    const t = window.setInterval(() => {
      if (document.hidden) return;
      refresh();
      if (selectedId) api.cycleRun(selectedId).then(setDetail).catch(() => undefined);
    }, 2500);
    return () => window.clearInterval(t);
  }, [hasActive, selectedId, refresh]);

  function open(id: string) {
    setSelectedId(id);
    api.cycleRun(id).then(setDetail).catch(() => setDetail(null));
  }

  async function remove(id: string) {
    if (!window.confirm("Delete this cycle run and its stored screenshots/video? This can't be undone.")) return;
    try {
      await api.deleteCycleRun(id);
      setSelectedId("");
      setDetail(null);
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Delete failed");
    }
  }

  return (
    <div className="mx-auto w-full max-w-6xl p-5">
      <h1 className="mb-4 text-[18px] font-semibold tracking-tight">Cycle runs</h1>
      <div className="grid items-start gap-4 lg:grid-cols-[0.8fr_1.2fr]">
        <Card>
          <CardHeader icon={<History className="size-[15px]" />} title="History" />
          <div className="p-2">
            {runs.length === 0 ? (
              <div className="px-3 py-8 text-center text-[13px] text-muted-foreground/70">No cycle runs yet.</div>
            ) : (
              <ul className="space-y-0.5">
                {runs.map((r) => (
                  <li key={r.id}>
                    <button onClick={() => open(r.id)} className={cn("flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left hover:bg-accent", selectedId === r.id && "bg-accent")}>
                      <span className="size-2 shrink-0 rounded-full" style={{ background: statusColor(r.status) }} />
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-[13px] font-medium">{r.cycleName || r.cycleId}</div>
                        <div className="truncate text-[11.5px] text-muted-foreground/70">{r.trigger} · {rollupText(r.totals)}</div>
                      </div>
                      <time className="shrink-0 font-mono text-[10.5px] text-muted-foreground/60">{new Date(r.startedAt).toLocaleTimeString()}</time>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </Card>

        {detail ? (
          <div className="space-y-4">
            <Card>
              <CardHeader
                title={detail.run.cycleName || "Cycle run"}
                action={
                  <div className="flex items-center gap-2">
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button size="sm" variant="secondary"><Download className="size-3.5" /> Export</Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onClick={() => exportRun(detail.run.id, "junit")}>JUnit XML (CI)</DropdownMenuItem>
                        <DropdownMenuItem onClick={() => exportRun(detail.run.id, "csv")}>Results CSV</DropdownMenuItem>
                        <DropdownMenuItem onClick={() => exportRun(detail.run.id, "xlsx")}>Results XLSX</DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                    {detail.run.status !== "running" && (
                      <Button size="sm" variant="danger" onClick={() => remove(detail.run.id)}><Trash2 className="size-3.5" /> Delete</Button>
                    )}
                    <Badge variant="outline" className="uppercase" style={badgeStyle(statusColor(detail.run.status))}>{detail.run.status}</Badge>
                  </div>
                }
              />
              <div className="flex flex-wrap gap-2 p-4">
                <Totals totals={detail.run.totals} />
              </div>
            </Card>
            {detail.cases.map((run) => (
              <TestReport key={run.id} run={run} />
            ))}
            {detail.cases.length === 0 && <div className="rounded-xl border border-dashed p-6 text-center text-[13px] text-muted-foreground" style={{ borderColor: "var(--border-strong)" }}>No case results yet — the run is starting.</div>}
          </div>
        ) : (
          <div className="grid min-h-[240px] place-items-center rounded-xl border border-dashed bg-secondary/40 px-6 text-center text-[13.5px] text-muted-foreground" style={{ borderColor: "var(--border-strong)" }}>
            <p className="max-w-xs">Select a cycle run to open its report — per-case results, step timelines, and screenshots.</p>
          </div>
        )}
      </div>
    </div>
  );
}

function Totals({ totals }: { totals: CycleTotals }) {
  const items: { label: string; value: number; color: string }[] = [
    { label: "Passed", value: totals.pass, color: "var(--running)" },
    { label: "Failed", value: totals.fail, color: "var(--destructive)" },
    { label: "Blocked", value: totals.blocked, color: "var(--warn)" },
    { label: "Not run", value: totals.notRun, color: "var(--stopped)" },
  ];
  return (
    <>
      {items.map((it) => (
        <div key={it.label} className="rounded-lg border px-3 py-2" style={{ borderColor: "var(--border)" }}>
          <div className="font-mono text-[20px] font-bold tabular-nums" style={{ color: it.value > 0 ? it.color : undefined }}>{it.value}</div>
          <div className="text-[11px] text-muted-foreground">{it.label}</div>
        </div>
      ))}
      <div className="rounded-lg border px-3 py-2" style={{ borderColor: "var(--border)" }}>
        <div className="font-mono text-[20px] font-bold tabular-nums">{totals.cases}</div>
        <div className="text-[11px] text-muted-foreground">Cases</div>
      </div>
    </>
  );
}

function rollupText(t: CycleTotals) {
  return `${t.pass}/${t.cases} passed`;
}
function statusColor(status: string) {
  return status === "passed" ? "var(--running)" : status === "running" ? "var(--warn)" : "var(--destructive)";
}
function badgeStyle(c: string): React.CSSProperties {
  return { color: c, background: `color-mix(in srgb, ${c} 12%, transparent)`, borderColor: `color-mix(in srgb, ${c} 30%, transparent)` };
}
