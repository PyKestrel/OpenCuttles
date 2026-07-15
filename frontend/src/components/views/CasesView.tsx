import { useCallback, useEffect, useRef, useState } from "react";
import { BookMarked, FolderTree, Plus, Trash2, Upload, X } from "lucide-react";
import { toast } from "sonner";
import { Card, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { api } from "@/api";
import type { Principal, TestCase, TestStep } from "@/types";
import { can } from "@/lib/permissions";

const emptyCase: Partial<TestCase> = { summary: "", labels: [], components: [], steps: [] };

export function CasesView({ principal }: { principal: Principal }) {
  const [cases, setCases] = useState<TestCase[]>([]);
  const [folder, setFolder] = useState<string>("");
  const [editing, setEditing] = useState<Partial<TestCase> | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const canTest = can(principal, "test");

  const refresh = useCallback(async () => {
    try {
      setCases((await api.cases()) ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to load cases");
    }
  }, []);
  useEffect(() => {
    refresh();
  }, [refresh]);

  const folders = Array.from(new Set(cases.map((c) => c.folderPath).filter(Boolean))) as string[];
  const visible = folder ? cases.filter((c) => c.folderPath === folder) : cases;

  async function onImport(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    try {
      const res = await api.importCases(file);
      toast.success(`Imported ${res.casesParsed} cases · ${res.stepsParsed} steps`, {
        description: res.warnings.length ? res.warnings.slice(0, 3).join("; ") : undefined,
      });
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Import failed");
    }
  }

  async function save(c: Partial<TestCase>) {
    try {
      if (c.id) await api.updateCase(c.id, c);
      else await api.createCase(c);
      setEditing(null);
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Save failed");
    }
  }

  async function remove(id: string) {
    await api.deleteCase(id);
    refresh();
  }

  return (
    <div className="mx-auto w-full max-w-6xl p-5">
      <div className="mb-4 flex items-center gap-3">
        <div>
          <h1 className="text-[18px] font-semibold tracking-tight">Test cases</h1>
          <p className="text-[13px] text-muted-foreground">Reusable, QMetry-compatible test definitions executed by the agent.</p>
        </div>
        {canTest && (
          <div className="ml-auto flex gap-2">
            <input ref={fileRef} type="file" accept=".csv,.xlsx,.xlsm,.tsv" hidden onChange={onImport} />
            <Button variant="secondary" onClick={() => fileRef.current?.click()}>
              <Upload className="size-3.5" /> Import QMetry
            </Button>
            <Button variant="primary" onClick={() => setEditing({ ...emptyCase })}>
              <Plus className="size-3.5" /> New case
            </Button>
          </div>
        )}
      </div>

      <div className="grid gap-4 lg:grid-cols-[200px_1fr]">
        <Card>
          <CardHeader icon={<FolderTree className="size-[15px]" />} title="Folders" />
          <div className="p-1.5 text-[13px]">
            <FolderRow label="All cases" count={cases.length} active={folder === ""} onClick={() => setFolder("")} />
            {folders.map((f) => (
              <FolderRow key={f} label={f} count={cases.filter((c) => c.folderPath === f).length} active={folder === f} onClick={() => setFolder(f)} />
            ))}
          </div>
        </Card>

        <Card>
          <CardHeader icon={<BookMarked className="size-[15px]" />} title={folder || "All cases"} action={<span className="text-[12px] text-muted-foreground/70">{visible.length}</span>} />
          {visible.length === 0 ? (
            <div className="px-4 py-10 text-center text-[13px] text-muted-foreground/70">No cases here yet. Import a QMetry export or author one.</div>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Summary</TableHead>
                    <TableHead className="w-24">Priority</TableHead>
                    <TableHead className="w-16 text-right">Steps</TableHead>
                    <TableHead>Labels</TableHead>
                    <TableHead className="w-10" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visible.map((c) => (
                    <TableRow key={c.id} className="cursor-pointer" onClick={() => setEditing(c)}>
                      <TableCell className="font-medium">
                        {c.summary}
                        {c.externalKey && <span className="ml-2 font-mono text-[10.5px] text-muted-foreground/60">{c.externalKey}</span>}
                      </TableCell>
                      <TableCell>{c.priority ? <Badge variant="outline" className="text-[10.5px] capitalize">{c.priority}</Badge> : <span className="text-muted-foreground/50">—</span>}</TableCell>
                      <TableCell className="text-right font-mono tabular-nums">{c.steps.length}</TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-1">
                          {c.labels.slice(0, 3).map((l) => (
                            <Badge key={l} variant="secondary" className="text-[10px]">{l}</Badge>
                          ))}
                        </div>
                      </TableCell>
                      <TableCell onClick={(e) => e.stopPropagation()}>
                        {canTest && (
                          <button onClick={() => remove(c.id)} title="Delete" className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-secondary hover:text-[var(--destructive)]">
                            <Trash2 className="size-3.5" />
                          </button>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </Card>
      </div>

      {editing && <CaseEditor initial={editing} onClose={() => setEditing(null)} onSave={save} />}
    </div>
  );
}

function FolderRow({ label, count, active, onClick }: { label: string; count: number; active: boolean; onClick: () => void }) {
  return (
    <button onClick={onClick} className={cn("flex w-full items-center gap-2 truncate rounded-md px-2.5 py-1.5 text-left hover:bg-accent", active && "bg-accent text-foreground")}>
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <span className="font-mono text-[10.5px] text-muted-foreground/70">{count}</span>
    </button>
  );
}

function CaseEditor({ initial, onClose, onSave }: { initial: Partial<TestCase>; onClose: () => void; onSave: (c: Partial<TestCase>) => void }) {
  const [c, setC] = useState<Partial<TestCase>>({ ...initial, labels: initial.labels ?? [], steps: initial.steps ?? [] });
  const set = (patch: Partial<TestCase>) => setC((prev) => ({ ...prev, ...patch }));
  const steps = c.steps ?? [];
  const setStep = (i: number, patch: Partial<TestStep>) => set({ steps: steps.map((s, idx) => (idx === i ? { ...s, ...patch } : s)) });
  const addStep = () => set({ steps: [...steps, { index: steps.length, action: "", testData: "", expected: "" }] });
  const removeStep = (i: number) => set({ steps: steps.filter((_, idx) => idx !== i) });

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{c.id ? "Edit case" : "New case"}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <Field label="Summary"><Input value={c.summary ?? ""} onChange={(e) => set({ summary: e.target.value })} placeholder="Login succeeds with valid credentials" /></Field>
          <div className="grid grid-cols-3 gap-3">
            <Field label="Priority"><Input value={c.priority ?? ""} onChange={(e) => set({ priority: e.target.value })} placeholder="high" /></Field>
            <Field label="Folder"><Input value={c.folderPath ?? ""} onChange={(e) => set({ folderPath: e.target.value })} placeholder="Auth/Login" /></Field>
            <Field label="Labels (comma)"><Input value={(c.labels ?? []).join(", ")} onChange={(e) => set({ labels: e.target.value.split(",").map((l) => l.trim()).filter(Boolean) })} /></Field>
          </div>
          <Field label="Precondition"><Textarea rows={2} value={c.precondition ?? ""} onChange={(e) => set({ precondition: e.target.value })} /></Field>

          <div>
            <div className="mb-1 flex items-center">
              <span className="text-[12px] text-muted-foreground">Steps</span>
              <Button size="sm" variant="ghost" className="ml-auto" onClick={addStep}><Plus className="size-3.5" /> Add step</Button>
            </div>
            <div className="space-y-2">
              {steps.map((s, i) => (
                <div key={i} className="grid grid-cols-[1.5rem_1fr_1fr_1fr_1.5rem] items-start gap-2 rounded-lg border p-2" style={{ borderColor: "var(--border)" }}>
                  <span className="pt-2 text-center font-mono text-[11px] text-muted-foreground/70">{i + 1}</span>
                  <Textarea rows={1} value={s.action} onChange={(e) => setStep(i, { action: e.target.value })} placeholder="Action" className="min-h-0" />
                  <Textarea rows={1} value={s.testData ?? ""} onChange={(e) => setStep(i, { testData: e.target.value })} placeholder="Test data" className="min-h-0" />
                  <Textarea rows={1} value={s.expected ?? ""} onChange={(e) => setStep(i, { expected: e.target.value })} placeholder="Expected result" className="min-h-0" />
                  <button onClick={() => removeStep(i)} className="grid size-7 place-items-center rounded-md text-muted-foreground hover:text-[var(--destructive)]"><X className="size-3.5" /></button>
                </div>
              ))}
              {steps.length === 0 && <div className="rounded-lg border border-dashed p-4 text-center text-[12px] text-muted-foreground/70">No steps — add the actions and expected results.</div>}
            </div>
          </div>

          <div className="flex justify-end gap-2 pt-1">
            <Button onClick={onClose}>Cancel</Button>
            <Button variant="primary" disabled={!c.summary?.trim()} onClick={() => onSave(c)}>Save case</Button>
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
