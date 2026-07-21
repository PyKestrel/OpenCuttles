import { useRef, useState } from "react";
import { AlertTriangle, ChevronDown, ChevronRight, FileText, Sparkles, Upload, X } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { api } from "@/api";
import type { DraftCase, DraftResult, TestStep } from "@/types";

// SpecDraftDialog turns a requirements document into proposed test cases, then
// makes the reviewer decide on each one before anything is saved.
//
// The review step is the point of the feature, not friction in front of it.
// These cases become the pass/fail source of truth for automated runs, so a
// case that reads plausibly but describes behavior the spec never promised
// doesn't just waste a tester's time — it reports failures that never happened.
// Everything here is built to make that judgment easy: warnings up top, steps
// visible without a second click, and nothing selected by accident.
export function SpecDraftDialog({ folder, onClose, onSaved }: { folder: string; onClose: () => void; onSaved: (n: number) => void }) {
  const [text, setText] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [folderPath, setFolderPath] = useState(folder);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<DraftResult | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  async function generate() {
    setBusy(true);
    try {
      const res = file
        ? await api.draftCasesFromFile(file, folderPath)
        : await api.draftCasesFromText(text, folderPath);
      setResult(res);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not read that specification");
    } finally {
      setBusy(false);
    }
  }

  const canGenerate = !busy && (file !== null || text.trim().length > 0);

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{result ? "Review drafted cases" : "Draft cases from a specification"}</DialogTitle>
        </DialogHeader>

        {result ? (
          <DraftReview
            result={result}
            onBack={() => setResult(null)}
            onClose={onClose}
            onSaved={onSaved}
          />
        ) : (
          <div className="space-y-3">
            <p className="text-[13px] text-muted-foreground">
              Upload a requirements document or paste the relevant section. Nothing is saved until you review and accept.
            </p>

            <input
              ref={fileRef}
              type="file"
              accept=".md,.markdown,.txt,.text,.docx"
              hidden
              onChange={(e) => {
                const f = e.target.files?.[0] ?? null;
                e.target.value = "";
                setFile(f);
                if (f) setText("");
              }}
            />

            {file ? (
              <div className="flex items-center gap-2 rounded-lg border p-2.5" style={{ borderColor: "var(--border)" }}>
                <FileText className="size-4 shrink-0 text-muted-foreground" />
                <span className="truncate text-[13px]">{file.name}</span>
                <span className="ml-auto shrink-0 text-[11.5px] text-muted-foreground/70">{Math.max(1, Math.round(file.size / 1024))} KB</span>
                <button onClick={() => setFile(null)} title="Remove" className="grid size-6 shrink-0 place-items-center rounded-md text-muted-foreground hover:text-[var(--destructive)]">
                  <X className="size-3.5" />
                </button>
              </div>
            ) : (
              <>
                <Button variant="secondary" onClick={() => fileRef.current?.click()} className="w-full">
                  <Upload className="size-3.5" /> Choose a document — Markdown, text, or Word
                </Button>
                <div className="flex items-center gap-3 text-[11.5px] uppercase tracking-wide text-muted-foreground/60">
                  <span className="h-px flex-1" style={{ background: "var(--border)" }} />or paste<span className="h-px flex-1" style={{ background: "var(--border)" }} />
                </div>
                <Textarea
                  rows={10}
                  value={text}
                  onChange={(e) => setText(e.target.value)}
                  placeholder={"Paste the requirements here.\n\nThe user must be able to sign in with a valid password. Signing in with a wrong password shows an error and does not open the home screen."}
                />
              </>
            )}

            <Field label="Folder for the drafted cases">
              <Input value={folderPath} onChange={(e) => setFolderPath(e.target.value)} placeholder="Auth/Login" />
            </Field>

            <div className="flex items-center justify-end gap-2 pt-1">
              <Button onClick={onClose}>Cancel</Button>
              <Button variant="primary" disabled={!canGenerate} onClick={generate}>
                <Sparkles className="size-3.5" /> {busy ? "Reading the specification…" : "Draft cases"}
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function DraftReview({ result, onBack, onClose, onSaved }: { result: DraftResult; onBack: () => void; onClose: () => void; onSaved: (n: number) => void }) {
  // Drafts start accepted — the reviewer is confirming a set, not assembling one
  // from nothing — but each is visible and rejectable before anything is written.
  const [cases, setCases] = useState<DraftCase[]>(result.cases);
  const [rejected, setRejected] = useState<Set<number>>(new Set());
  const [editing, setEditing] = useState<number | null>(null);
  const [saving, setSaving] = useState(false);

  const accepted = cases.filter((_, i) => !rejected.has(i));

  function toggle(i: number) {
    setRejected((prev) => {
      const next = new Set(prev);
      if (next.has(i)) next.delete(i);
      else next.add(i);
      return next;
    });
  }

  async function save() {
    setSaving(true);
    try {
      // Written one at a time through the ordinary create path, so drafts get the
      // same validation and audit trail as a hand-authored case.
      let written = 0;
      for (const c of accepted) {
        await api.createCase(c);
        written++;
      }
      onSaved(written);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save the accepted cases");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-3">
      {(result.warnings.length > 0 || result.dropped > 0) && (
        <div className="space-y-1.5 rounded-lg border p-3" style={{ borderColor: "var(--warn)", background: "color-mix(in oklab, var(--warn) 8%, transparent)" }}>
          <div className="flex items-center gap-2 text-[12.5px] font-medium">
            <AlertTriangle className="size-3.5" style={{ color: "var(--warn)" }} />
            Check these before accepting
          </div>
          <ul className="space-y-1 pl-5 text-[12.5px] text-muted-foreground">
            {result.warnings.map((w, i) => (
              <li key={i} className="list-disc">{w}</li>
            ))}
          </ul>
        </div>
      )}

      <p className="text-[13px] text-muted-foreground">
        {cases.length} case{cases.length === 1 ? "" : "s"} drafted from the specification. Uncheck anything the specification doesn&apos;t actually promise.
      </p>

      <div className="space-y-2">
        {cases.map((c, i) => (
          <DraftRow
            key={i}
            draft={c}
            rejected={rejected.has(i)}
            expanded={editing === i}
            onToggle={() => toggle(i)}
            onExpand={() => setEditing(editing === i ? null : i)}
            onChange={(patch) => setCases((prev) => prev.map((x, idx) => (idx === i ? { ...x, ...patch } : x)))}
          />
        ))}
      </div>

      <div className="flex items-center gap-2 pt-1">
        <Button onClick={onBack}>Back</Button>
        <span className="text-[12.5px] text-muted-foreground">
          {accepted.length} of {cases.length} will be saved
        </span>
        <div className="ml-auto flex gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={saving || accepted.length === 0} onClick={save}>
            {saving ? "Saving…" : `Save ${accepted.length} case${accepted.length === 1 ? "" : "s"}`}
          </Button>
        </div>
      </div>
    </div>
  );
}

function DraftRow({
  draft,
  rejected,
  expanded,
  onToggle,
  onExpand,
  onChange,
}: {
  draft: DraftCase;
  rejected: boolean;
  expanded: boolean;
  onToggle: () => void;
  onExpand: () => void;
  onChange: (patch: Partial<DraftCase>) => void;
}) {
  const steps = draft.steps ?? [];
  const setStep = (i: number, patch: Partial<TestStep>) =>
    onChange({ steps: steps.map((s, idx) => (idx === i ? { ...s, ...patch } : s)) });

  return (
    <div className="rounded-lg border" style={{ borderColor: "var(--border)", opacity: rejected ? 0.5 : 1 }}>
      <div className="flex items-start gap-2.5 p-2.5">
        <Checkbox checked={!rejected} onCheckedChange={onToggle} className="mt-0.5" />
        <button onClick={onExpand} className="min-w-0 flex-1 text-left">
          <div className="flex items-center gap-2">
            <span className="truncate text-[13px] font-medium">{draft.summary}</span>
            {draft.priority && <Badge>{draft.priority}</Badge>}
          </div>
          <span className="text-[11.5px] text-muted-foreground/70">
            {steps.length} step{steps.length === 1 ? "" : "s"}
            {draft.folderPath ? ` · ${draft.folderPath}` : ""}
          </span>
        </button>
        <button onClick={onExpand} className="grid size-6 shrink-0 place-items-center rounded-md text-muted-foreground hover:text-primary">
          {expanded ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
        </button>
      </div>

      {expanded && (
        <div className="space-y-2.5 border-t p-2.5" style={{ borderColor: "var(--border)" }}>
          <Field label="Summary">
            <Input value={draft.summary} onChange={(e) => onChange({ summary: e.target.value })} />
          </Field>
          {draft.precondition !== undefined && (
            <Field label="Precondition">
              <Textarea rows={2} value={draft.precondition ?? ""} onChange={(e) => onChange({ precondition: e.target.value })} />
            </Field>
          )}
          <div>
            <span className="text-[12px] text-muted-foreground">Steps</span>
            <div className="mt-1 space-y-2">
              {steps.map((s, i) => (
                <div key={i} className="grid grid-cols-[1.5rem_1fr_1fr] items-start gap-2 rounded-lg border p-2" style={{ borderColor: "var(--border)" }}>
                  <span className="pt-2 text-center font-mono text-[11px] text-muted-foreground/70">{i + 1}</span>
                  <Textarea rows={2} value={s.action} onChange={(e) => setStep(i, { action: e.target.value })} placeholder="Action" className="min-h-0" />
                  <Textarea rows={2} value={s.expected ?? ""} onChange={(e) => setStep(i, { expected: e.target.value })} placeholder="Expected result" className="min-h-0" />
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
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
