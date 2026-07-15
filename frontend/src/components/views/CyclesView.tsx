import { useCallback, useEffect, useState } from "react";
import { ListChecks, Play, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Card, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Checkbox } from "@/components/ui/checkbox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/api";
import { platformLabel } from "@/lib/platform";
import type { Build, Platform, Principal, TestCase, TestCycle } from "@/types";
import { can } from "@/lib/permissions";

const PLATFORMS: Platform[] = ["android", "windows", "linux", "macos"];
const NONE = "__none__";

export function CyclesView({ principal }: { principal: Principal }) {
  const [cycles, setCycles] = useState<TestCycle[]>([]);
  const [cases, setCases] = useState<TestCase[]>([]);
  const [builds, setBuilds] = useState<Build[]>([]);
  const [editing, setEditing] = useState<Partial<TestCycle> | null>(null);
  const canTest = can(principal, "test");

  const refresh = useCallback(async () => {
    const [cy, ca, bu] = await Promise.all([api.cycles().catch(() => []), api.cases().catch(() => []), api.builds().catch(() => [])]);
    setCycles(cy ?? []);
    setCases(ca ?? []);
    setBuilds(bu ?? []);
  }, []);
  useEffect(() => {
    refresh();
  }, [refresh]);

  async function save(c: Partial<TestCycle>) {
    try {
      const saved = c.id ? await api.updateCycle(c.id, c) : await api.createCycle(c);
      // Schedule fields go through the dedicated endpoint so next-run recomputes.
      await api.updateCycleSchedule(saved.id, { cron: c.cron ?? "", onNewBuild: !!c.onNewBuild, enabled: c.enabled !== false });
      setEditing(null);
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Save failed");
    }
  }

  async function run(id: string) {
    try {
      await api.runCycle(id);
      toast.success("Cycle run started — watch it under Cycle runs.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not start the cycle");
    }
  }

  return (
    <div className="mx-auto w-full max-w-5xl p-5">
      <div className="mb-4 flex items-center">
        <div>
          <h1 className="text-[18px] font-semibold tracking-tight">Test cycles</h1>
          <p className="text-[13px] text-muted-foreground">Select cases into a cycle, schedule it, and run it on a device.</p>
        </div>
        {canTest && (
          <Button variant="primary" className="ml-auto" onClick={() => setEditing({ platform: "windows", caseIds: [], enabled: true })}>
            <Plus className="size-3.5" /> New cycle
          </Button>
        )}
      </div>

      <Card>
        <CardHeader icon={<ListChecks className="size-[15px]" />} title="Cycles" action={<span className="text-[12px] text-muted-foreground/70">{cycles.length}</span>} />
        {cycles.length === 0 ? (
          <div className="px-4 py-10 text-center text-[13px] text-muted-foreground/70">No cycles yet. Compose one from your test cases.</div>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead className="w-24">Platform</TableHead>
                  <TableHead className="w-16 text-right">Cases</TableHead>
                  <TableHead>Schedule</TableHead>
                  <TableHead className="w-40" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {cycles.map((c) => (
                  <TableRow key={c.id}>
                    <TableCell className="cursor-pointer font-medium" onClick={() => setEditing(c)}>
                      {c.name}
                      {!c.enabled && <Badge variant="outline" className="ml-2 text-[10px]">disabled</Badge>}
                    </TableCell>
                    <TableCell><Badge variant="secondary" className="text-[10.5px]">{platformLabel(c.platform)}</Badge></TableCell>
                    <TableCell className="text-right font-mono tabular-nums">{c.caseIds.length}</TableCell>
                    <TableCell className="text-[12px] text-muted-foreground">
                      {c.cron ? <span className="font-mono">{c.cron}</span> : <span className="text-muted-foreground/50">manual</span>}
                      {c.onNewBuild && <Badge variant="outline" className="ml-2 text-[10px]">on build</Badge>}
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-1">
                        <Button size="sm" variant="primary" disabled={c.caseIds.length === 0} onClick={() => run(c.id)}><Play className="size-3" /> Run</Button>
                        {canTest && (
                          <button onClick={() => api.deleteCycle(c.id).then(refresh)} title="Delete" className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-secondary hover:text-[var(--destructive)]">
                            <Trash2 className="size-3.5" />
                          </button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </Card>

      {editing && <CycleEditor initial={editing} cases={cases} builds={builds} onClose={() => setEditing(null)} onSave={save} />}
    </div>
  );
}

function CycleEditor({ initial, cases, builds, onClose, onSave }: { initial: Partial<TestCycle>; cases: TestCase[]; builds: Build[]; onClose: () => void; onSave: (c: Partial<TestCycle>) => void }) {
  const [c, setC] = useState<Partial<TestCycle>>({ caseIds: [], enabled: true, ...initial });
  const set = (patch: Partial<TestCycle>) => setC((prev) => ({ ...prev, ...patch }));
  const selected = new Set(c.caseIds ?? []);
  const platformBuilds = builds.filter((b) => b.platform === c.platform);

  function toggleCase(id: string) {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    set({ caseIds: Array.from(next) });
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader><DialogTitle>{c.id ? "Edit cycle" : "New cycle"}</DialogTitle></DialogHeader>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Name"><Input value={c.name ?? ""} onChange={(e) => set({ name: e.target.value })} placeholder="Windows regression" /></Field>
            <Field label="Platform">
              <Select value={c.platform ?? "windows"} onValueChange={(v) => set({ platform: v as Platform })}>
                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>{PLATFORMS.map((p) => <SelectItem key={p} value={p}>{platformLabel(p)}</SelectItem>)}</SelectContent>
              </Select>
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Environment"><Input value={c.environment ?? ""} onChange={(e) => set({ environment: e.target.value })} placeholder="staging" /></Field>
            <Field label="Bound build (optional)">
              <Select value={c.buildId || NONE} onValueChange={(v) => set({ buildId: v === NONE ? "" : v })}>
                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>None (current state)</SelectItem>
                  {platformBuilds.map((b) => <SelectItem key={b.id} value={b.id}>{b.filename}{b.version ? ` · ${b.version}` : ""}</SelectItem>)}
                </SelectContent>
              </Select>
            </Field>
          </div>

          <Field label="Schedule (cron, optional)"><Input value={c.cron ?? ""} onChange={(e) => set({ cron: e.target.value })} placeholder="0 */6 * * *  (every 6 hours)" className="font-mono" /></Field>
          <div className="flex items-center gap-6">
            <label className="flex items-center gap-2 text-[13px]"><Switch checked={!!c.onNewBuild} onCheckedChange={(v) => set({ onNewBuild: v })} /> Run automatically on a new build</label>
            <label className="flex items-center gap-2 text-[13px]"><Switch checked={c.enabled !== false} onCheckedChange={(v) => set({ enabled: v })} /> Enabled</label>
          </div>

          <div>
            <div className="mb-1 text-[12px] text-muted-foreground">Cases ({(c.caseIds ?? []).length} selected)</div>
            <div className="max-h-56 space-y-0.5 overflow-y-auto rounded-lg border p-1.5" style={{ borderColor: "var(--border)" }}>
              {cases.length === 0 && <div className="p-3 text-center text-[12px] text-muted-foreground/70">No cases yet — create some first.</div>}
              {cases.map((tc) => (
                <label key={tc.id} className="flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-1.5 text-[13px] hover:bg-accent">
                  <Checkbox checked={selected.has(tc.id)} onCheckedChange={() => toggleCase(tc.id)} />
                  <span className="min-w-0 flex-1 truncate">{tc.summary}</span>
                  {tc.folderPath && <span className="truncate text-[11px] text-muted-foreground/60">{tc.folderPath}</span>}
                </label>
              ))}
            </div>
          </div>

          <div className="flex justify-end gap-2 pt-1">
            <Button onClick={onClose}>Cancel</Button>
            <Button variant="primary" disabled={!c.name?.trim()} onClick={() => onSave(c)}>Save cycle</Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1 block text-[12px] text-muted-foreground">{label}</span>
      {children}
    </label>
  );
}
