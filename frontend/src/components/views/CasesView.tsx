import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { BookMarked, ChevronDown, ChevronRight, Copy, FolderTree, MoreHorizontal, Pencil, Plus, Search, Trash2, Upload, X } from "lucide-react";
import { toast } from "sonner";
import { Card, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { api } from "@/api";
import type { Principal, TestCase, TestStep } from "@/types";
import { can } from "@/lib/permissions";

const emptyCase: Partial<TestCase> = { summary: "", labels: [], components: [], steps: [] };

type FolderNode = { name: string; path: string; children: FolderNode[] };

function buildFolderTree(paths: string[]): FolderNode[] {
  const root: FolderNode = { name: "", path: "", children: [] };
  for (const p of paths) {
    if (!p) continue;
    let node = root;
    let acc = "";
    for (const seg of p.split("/").filter(Boolean)) {
      acc = acc ? `${acc}/${seg}` : seg;
      let child = node.children.find((c) => c.name === seg);
      if (!child) {
        child = { name: seg, path: acc, children: [] };
        node.children.push(child);
      }
      node = child;
    }
  }
  const sortRec = (n: FolderNode) => {
    n.children.sort((a, b) => a.name.localeCompare(b.name));
    n.children.forEach(sortRec);
  };
  sortRec(root);
  return root.children;
}

const inFolder = (casePath: string | undefined, folder: string) =>
  !folder || casePath === folder || (casePath ?? "").startsWith(folder + "/");

export function CasesView({ principal }: { principal: Principal }) {
  const [cases, setCases] = useState<TestCase[]>([]);
  const [folder, setFolder] = useState<string>("");
  const [query, setQuery] = useState("");
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<Set<string>>(new Set());
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

  const tree = useMemo(() => buildFolderTree(cases.map((c) => c.folderPath || "").filter(Boolean)), [cases]);
  const folderCount = useCallback((path: string) => cases.filter((c) => inFolder(c.folderPath, path)).length, [cases]);

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    return cases.filter((c) => {
      if (!inFolder(c.folderPath, folder)) return false;
      if (!q) return true;
      const hay = [c.summary, c.folderPath, c.priority, c.externalKey, ...(c.labels ?? []), ...c.steps.flatMap((s) => [s.action, s.expected])]
        .join(" ")
        .toLowerCase();
      return hay.includes(q);
    });
  }, [cases, folder, query]);

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

  async function clone(c: TestCase) {
    try {
      await api.createCase(cloneFields(c));
      toast.success("Case cloned");
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Clone failed");
    }
  }

  async function remove(c: TestCase) {
    await api.deleteCase(c.id);
    setSelected((prev) => {
      const n = new Set(prev);
      n.delete(c.id);
      return n;
    });
    refresh();
  }

  const cloneFields = (c: TestCase): Partial<TestCase> => ({
    summary: `${c.summary} (copy)`,
    description: c.description,
    precondition: c.precondition,
    priority: c.priority,
    status: c.status,
    labels: c.labels,
    components: c.components,
    folderPath: c.folderPath,
    steps: c.steps.map((s) => ({ ...s })),
  });

  function toggleSelect(id: string) {
    setSelected((prev) => {
      const n = new Set(prev);
      n.has(id) ? n.delete(id) : n.add(id);
      return n;
    });
  }
  const allVisibleSelected = visible.length > 0 && visible.every((c) => selected.has(c.id));
  function toggleAll() {
    setSelected((prev) => {
      const n = new Set(prev);
      if (allVisibleSelected) visible.forEach((c) => n.delete(c.id));
      else visible.forEach((c) => n.add(c.id));
      return n;
    });
  }

  async function bulkDelete() {
    const ids = Array.from(selected);
    await Promise.all(ids.map((id) => api.deleteCase(id).catch(() => undefined)));
    setSelected(new Set());
    toast.success(`Deleted ${ids.length} case${ids.length === 1 ? "" : "s"}`);
    refresh();
  }
  async function bulkClone() {
    const picked = cases.filter((c) => selected.has(c.id));
    await Promise.all(picked.map((c) => api.createCase(cloneFields(c)).catch(() => undefined)));
    setSelected(new Set());
    toast.success(`Cloned ${picked.length} case${picked.length === 1 ? "" : "s"}`);
    refresh();
  }

  function toggleExpand(path: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      next.has(path) ? next.delete(path) : next.add(path);
      return next;
    });
  }

  function renderFolder(node: FolderNode, depth: number): React.ReactNode {
    const hasChildren = node.children.length > 0;
    const open = expanded.has(node.path);
    return (
      <div key={node.path}>
        <div className={cn("flex items-center gap-1 rounded-md pr-2 hover:bg-accent", folder === node.path && "bg-accent")} style={{ paddingLeft: 4 + depth * 12 }}>
          <button onClick={() => hasChildren && toggleExpand(node.path)} className="grid size-5 shrink-0 place-items-center text-muted-foreground">
            {hasChildren ? open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" /> : null}
          </button>
          <button onClick={() => setFolder(node.path)} className="flex min-w-0 flex-1 items-center gap-2 py-1.5 text-left">
            <span className="min-w-0 flex-1 truncate">{node.name}</span>
            <span className="font-mono text-[10.5px] text-muted-foreground/70">{folderCount(node.path)}</span>
          </button>
        </div>
        {open && node.children.map((c) => renderFolder(c, depth + 1))}
      </div>
    );
  }

  return (
    <div className="mx-auto w-full max-w-6xl p-5">
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <div>
          <h1 className="text-[18px] font-semibold tracking-tight">Test cases</h1>
          <p className="text-[13px] text-muted-foreground">Reusable, QMetry-compatible test definitions executed by the agent.</p>
        </div>
        <div className="relative ml-auto w-64">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search cases…" className="pl-8" />
        </div>
        {canTest && (
          <div className="flex gap-2">
            <input ref={fileRef} type="file" accept=".csv,.xlsx,.xlsm,.tsv" hidden onChange={onImport} />
            <Button variant="secondary" onClick={() => fileRef.current?.click()}>
              <Upload className="size-3.5" /> Import QMetry
            </Button>
            <Button variant="primary" onClick={() => setEditing({ ...emptyCase, folderPath: folder })}>
              <Plus className="size-3.5" /> New case
            </Button>
          </div>
        )}
      </div>

      <div className="grid gap-4 lg:grid-cols-[220px_1fr]">
        <Card>
          <CardHeader icon={<FolderTree className="size-[15px]" />} title="Folders" />
          <div className="p-1.5 text-[13px]">
            <button onClick={() => setFolder("")} className={cn("flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-accent", folder === "" && "bg-accent text-foreground")}>
              <span className="min-w-0 flex-1 truncate">All cases</span>
              <span className="font-mono text-[10.5px] text-muted-foreground/70">{cases.length}</span>
            </button>
            {tree.map((n) => renderFolder(n, 0))}
          </div>
        </Card>

        <Card>
          <CardHeader icon={<BookMarked className="size-[15px]" />} title={folder || "All cases"} action={<span className="text-[12px] text-muted-foreground/70">{visible.length}{query || folder ? ` / ${cases.length}` : ""}</span>} />
          {selected.size > 0 && canTest && (
            <div className="flex items-center gap-2 border-b bg-secondary/60 px-4 py-2 text-[13px]" style={{ borderColor: "var(--hairline)" }}>
              <span className="font-medium">{selected.size} selected</span>
              <Button size="sm" variant="secondary" onClick={bulkClone}><Copy className="size-3.5" /> Clone</Button>
              <Button size="sm" variant="danger" onClick={bulkDelete}><Trash2 className="size-3.5" /> Delete</Button>
              <button onClick={() => setSelected(new Set())} className="ml-auto text-[12px] text-muted-foreground hover:text-foreground">Clear</button>
            </div>
          )}
          {visible.length === 0 ? (
            <div className="px-4 py-10 text-center text-[13px] text-muted-foreground/70">{query ? "No cases match your search." : "No cases here yet. Import a QMetry export or author one."}</div>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-8"><Checkbox checked={allVisibleSelected} onCheckedChange={toggleAll} aria-label="Select all" /></TableHead>
                    <TableHead>Summary</TableHead>
                    <TableHead className="w-40">Folder</TableHead>
                    <TableHead className="w-20">Priority</TableHead>
                    <TableHead className="w-14 text-right">Steps</TableHead>
                    <TableHead className="w-10" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visible.map((c) => (
                    <TableRow key={c.id} className="cursor-pointer" data-state={selected.has(c.id) ? "selected" : undefined} onClick={() => setEditing(c)}>
                      <TableCell onClick={(e) => e.stopPropagation()}>
                        <Checkbox checked={selected.has(c.id)} onCheckedChange={() => toggleSelect(c.id)} aria-label="Select case" />
                      </TableCell>
                      <TableCell className="font-medium">
                        {c.summary}
                        {c.externalKey && <span className="ml-2 font-mono text-[10.5px] text-muted-foreground/60">{c.externalKey}</span>}
                        {(c.labels?.length ?? 0) > 0 && (
                          <span className="ml-2 inline-flex gap-1">{c.labels.slice(0, 3).map((l) => <Badge key={l} variant="secondary" className="text-[10px]">{l}</Badge>)}</span>
                        )}
                      </TableCell>
                      <TableCell className="truncate text-[12px] text-muted-foreground">{c.folderPath || "—"}</TableCell>
                      <TableCell>{c.priority ? <Badge variant="outline" className="text-[10.5px] capitalize">{c.priority}</Badge> : <span className="text-muted-foreground/50">—</span>}</TableCell>
                      <TableCell className="text-right font-mono tabular-nums">{c.steps.length}</TableCell>
                      <TableCell onClick={(e) => e.stopPropagation()}>
                        {canTest && (
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <button className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-secondary hover:text-foreground"><MoreHorizontal className="size-4" /></button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem onClick={() => setEditing(c)}><Pencil className="size-3.5" /> Edit</DropdownMenuItem>
                              <DropdownMenuItem onClick={() => clone(c)}><Copy className="size-3.5" /> Clone</DropdownMenuItem>
                              <DropdownMenuSeparator />
                              <DropdownMenuItem variant="destructive" onClick={() => remove(c)}><Trash2 className="size-3.5" /> Delete</DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
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
