import { useCallback, useEffect, useState } from "react";
import { Copy, ListChecks, ListPlus, MoreHorizontal, Pencil, Play, Plus, Trash2 } from "lucide-react";
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
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { api } from "@/api";
import { platformLabel } from "@/lib/platform";
import type { Build, Platform, Principal, TestCase, TestCycle } from "@/types";
import { can } from "@/lib/permissions";

const PLATFORMS: Platform[] = ["android", "windows", "linux", "macos"];
const NONE = "__none__";

// The viewer's IANA zone, offered as a one-click default so a cron doesn't
// silently mean UTC when the author meant local time.
const browserTimezone = (() => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "";
  } catch {
    return "";
  }
})();

export function CyclesView({ principal }: { principal: Principal }) {
  const [cycles, setCycles] = useState<TestCycle[]>([]);
  const [cases, setCases] = useState<TestCase[]>([]);
  const [builds, setBuilds] = useState<Build[]>([]);
  const [editing, setEditing] = useState<Partial<TestCycle> | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [addCasesFor, setAddCasesFor] = useState<TestCycle | null>(null);
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
      await api.updateCycleSchedule(saved.id, {
        cron: c.cron ?? "",
        timezone: c.timezone ?? "",
        onNewBuild: !!c.onNewBuild,
        enabled: c.enabled !== false,
      });
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

  // A clone starts disabled so a copied schedule doesn't fire unexpectedly.
  const cloneFields = (c: TestCycle): Partial<TestCycle> => ({
    name: `${c.name} (copy)`,
    platform: c.platform,
    buildId: c.buildId,
    environment: c.environment,
    caseIds: c.caseIds,
    cron: c.cron,
    timezone: c.timezone,
    onNewBuild: c.onNewBuild,
    enabled: false,
  });

  async function clone(c: TestCycle) {
    try {
      await api.createCycle(cloneFields(c));
      toast.success("Cycle cloned (disabled)");
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Clone failed");
    }
  }

  async function remove(id: string) {
    await api.deleteCycle(id);
    setSelected((prev) => {
      const n = new Set(prev);
      n.delete(id);
      return n;
    });
    refresh();
  }

  function toggleSelect(id: string) {
    setSelected((prev) => {
      const n = new Set(prev);
      n.has(id) ? n.delete(id) : n.add(id);
      return n;
    });
  }
  const allSelected = cycles.length > 0 && cycles.every((c) => selected.has(c.id));
  function toggleAll() {
    setSelected(allSelected ? new Set() : new Set(cycles.map((c) => c.id)));
  }
  async function bulkDelete() {
    const ids = Array.from(selected);
    await Promise.all(ids.map((id) => api.deleteCycle(id).catch(() => undefined)));
    setSelected(new Set());
    toast.success(`Deleted ${ids.length} cycle${ids.length === 1 ? "" : "s"}`);
    refresh();
  }
  async function bulkClone() {
    const picked = cycles.filter((c) => selected.has(c.id));
    await Promise.all(picked.map((c) => api.createCycle(cloneFields(c)).catch(() => undefined)));
    setSelected(new Set());
    toast.success(`Cloned ${picked.length} cycle${picked.length === 1 ? "" : "s"}`);
    refresh();
  }

  async function addCasesToCycle(cycle: TestCycle, ids: string[]) {
    const merged = Array.from(new Set([...(cycle.caseIds ?? []), ...ids]));
    try {
      await api.updateCycleCases(cycle.id, merged);
      toast.success(`Added ${ids.length} case${ids.length === 1 ? "" : "s"} to ${cycle.name}`);
      setAddCasesFor(null);
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not add cases");
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
        {selected.size > 0 && canTest && (
          <div className="flex items-center gap-2 border-b bg-secondary/60 px-4 py-2 text-[13px]" style={{ borderColor: "var(--hairline)" }}>
            <span className="font-medium">{selected.size} selected</span>
            <Button size="sm" variant="secondary" onClick={bulkClone}><Copy className="size-3.5" /> Clone</Button>
            <Button size="sm" variant="danger" onClick={bulkDelete}><Trash2 className="size-3.5" /> Delete</Button>
            <button onClick={() => setSelected(new Set())} className="ml-auto text-[12px] text-muted-foreground hover:text-foreground">Clear</button>
          </div>
        )}
        {cycles.length === 0 ? (
          <div className="px-4 py-10 text-center text-[13px] text-muted-foreground/70">No cycles yet. Compose one from your test cases.</div>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-8"><Checkbox checked={allSelected} onCheckedChange={toggleAll} aria-label="Select all" /></TableHead>
                  <TableHead>Name</TableHead>
                  <TableHead className="w-24">Platform</TableHead>
                  <TableHead className="w-16 text-right">Cases</TableHead>
                  <TableHead>Schedule</TableHead>
                  <TableHead className="w-40" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {cycles.map((c) => (
                  <TableRow key={c.id} data-state={selected.has(c.id) ? "selected" : undefined}>
                    <TableCell><Checkbox checked={selected.has(c.id)} onCheckedChange={() => toggleSelect(c.id)} aria-label="Select cycle" /></TableCell>
                    <TableCell className="cursor-pointer font-medium" onClick={() => setEditing(c)}>
                      {c.name}
                      {!c.enabled && <Badge variant="outline" className="ml-2 text-[10px]">disabled</Badge>}
                    </TableCell>
                    <TableCell><Badge variant="secondary" className="text-[10.5px]">{platformLabel(c.platform)}</Badge></TableCell>
                    <TableCell className="text-right font-mono tabular-nums">{c.caseIds.length}</TableCell>
                    <TableCell className="text-[12px] text-muted-foreground">
                      {c.cron ? (
                        <span className="font-mono" title={`Interpreted in ${c.timezone || "UTC"}`}>
                          {c.cron}
                          <span className="ml-1.5 text-[10.5px] text-muted-foreground/70">{c.timezone || "UTC"}</span>
                        </span>
                      ) : (
                        <span className="text-muted-foreground/50">manual</span>
                      )}
                      {c.onNewBuild && <Badge variant="outline" className="ml-2 text-[10px]">on build</Badge>}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center justify-end gap-1">
                        <Button size="sm" variant="primary" disabled={c.caseIds.length === 0} onClick={() => run(c.id)}><Play className="size-3" /> Run</Button>
                        {canTest && (
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <button className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-secondary hover:text-foreground"><MoreHorizontal className="size-4" /></button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem onClick={() => setEditing(c)}><Pencil className="size-3.5" /> Edit</DropdownMenuItem>
                              <DropdownMenuItem onClick={() => setAddCasesFor(c)}><ListPlus className="size-3.5" /> Add cases</DropdownMenuItem>
                              <DropdownMenuItem onClick={() => clone(c)}><Copy className="size-3.5" /> Clone</DropdownMenuItem>
                              <DropdownMenuSeparator />
                              <DropdownMenuItem variant="destructive" onClick={() => remove(c.id)}><Trash2 className="size-3.5" /> Delete</DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
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
      {addCasesFor && <AddCasesDialog cycle={addCasesFor} cases={cases} onClose={() => setAddCasesFor(null)} onSave={(ids) => addCasesToCycle(addCasesFor, ids)} />}
    </div>
  );
}

function AddCasesDialog({ cycle, cases, onClose, onSave }: { cycle: TestCycle; cases: TestCase[]; onClose: () => void; onSave: (ids: string[]) => void }) {
  const existing = new Set(cycle.caseIds ?? []);
  const candidates = cases.filter((c) => !existing.has(c.id));
  const [pick, setPick] = useState<Set<string>>(new Set());
  const toggle = (id: string) =>
    setPick((prev) => {
      const n = new Set(prev);
      n.has(id) ? n.delete(id) : n.add(id);
      return n;
    });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Add cases to {cycle.name}</DialogTitle>
        </DialogHeader>
        {candidates.length === 0 ? (
          <div className="py-6 text-center text-[13px] text-muted-foreground/70">All cases are already in this cycle.</div>
        ) : (
          <div className="max-h-72 space-y-0.5 overflow-y-auto rounded-lg border p-1.5" style={{ borderColor: "var(--border)" }}>
            {candidates.map((tc) => (
              <label key={tc.id} className="flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-1.5 text-[13px] hover:bg-accent">
                <Checkbox checked={pick.has(tc.id)} onCheckedChange={() => toggle(tc.id)} />
                <span className="min-w-0 flex-1 truncate">{tc.summary}</span>
                {tc.folderPath && <span className="truncate text-[11px] text-muted-foreground/60">{tc.folderPath}</span>}
              </label>
            ))}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={pick.size === 0} onClick={() => onSave(Array.from(pick))}>Add {pick.size || ""}</Button>
        </div>
      </DialogContent>
    </Dialog>
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

          <div className="grid gap-3 sm:grid-cols-[1.4fr_1fr]">
            <Field label="Schedule (cron, optional)">
              <Input value={c.cron ?? ""} onChange={(e) => set({ cron: e.target.value })} placeholder="0 */6 * * *  (every 6 hours)" className="font-mono" />
            </Field>
            <Field label="Timezone">
              <div className="flex gap-1.5">
                <Input value={c.timezone ?? ""} onChange={(e) => set({ timezone: e.target.value })} placeholder="UTC" className="font-mono" />
                {browserTimezone && c.timezone !== browserTimezone && (
                  <Button type="button" variant="secondary" className="shrink-0" title={`Use ${browserTimezone}`} onClick={() => set({ timezone: browserTimezone })}>
                    Use mine
                  </Button>
                )}
              </div>
            </Field>
          </div>
          {c.cron && (
            <p className="text-[11.5px] text-muted-foreground/80">
              Runs at this cron in <span className="font-mono">{c.timezone || "UTC"}</span>
              {!c.timezone && browserTimezone !== "UTC" && <> — not your local {browserTimezone}</>}.
            </p>
          )}
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
