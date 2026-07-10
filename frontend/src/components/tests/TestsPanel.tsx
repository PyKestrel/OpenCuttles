import { useCallback, useEffect, useState, type FormEvent } from "react";
import { FlaskConical, History, Plus, Trash2 } from "lucide-react";
import { Card, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { TestReport } from "@/components/tests/TestReport";
import { api } from "@/api";
import type { DeviceTest, Instance, TestRun } from "@/types";

// Author natural-language tests, run them against a device, and open replayable
// reports. Shared by the global Tests view and the per-device Tests tab; when
// `scoped` is set the run history is filtered to `instance`.
export function TestsPanel({
  instance,
  instances,
  scoped = false,
}: {
  instance?: Instance;
  instances: Instance[];
  scoped?: boolean;
}) {
  const [tests, setTests] = useState<DeviceTest[]>([]);
  const [runs, setRuns] = useState<TestRun[]>([]);
  const [name, setName] = useState("");
  const [stepsText, setStepsText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [selectedRunId, setSelectedRunId] = useState<string>(() =>
    window.location.hash.startsWith("#run-") ? window.location.hash.slice(5) : "",
  );

  const visibleRuns = scoped && instance ? runs.filter((r) => r.instanceId === instance.id) : runs;
  const selectedRun = runs.find((run) => run.id === selectedRunId);
  const hasActiveRun = runs.some((run) => run.status === "running");
  const canRun = instance?.state === "running";

  const refresh = useCallback(async () => {
    try {
      const [testList, runList] = await Promise.all([api.tests(), api.testRuns()]);
      setTests(testList ?? []);
      setRuns(runList ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load tests");
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    if (!hasActiveRun) return;
    const timer = window.setInterval(() => {
      if (!document.hidden) refresh();
    }, 2500);
    return () => window.clearInterval(timer);
  }, [hasActiveRun, refresh]);

  async function act(action: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await action();
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Action failed");
    } finally {
      setBusy(false);
    }
  }

  function submitTest(event: FormEvent) {
    event.preventDefault();
    const steps = stepsText.split("\n").map((s) => s.trim()).filter(Boolean);
    if (!name.trim() || steps.length === 0) return;
    act(async () => {
      await api.createTest(name.trim(), steps);
      setName("");
      setStepsText("");
    });
  }

  function runTest(testId: string) {
    if (!instance) {
      setError("Select a running device first.");
      return;
    }
    act(async () => {
      const run = await api.runTest(testId, instance.id);
      setSelectedRunId(run.id);
      window.location.hash = `run-${run.id}`;
    });
  }

  function openRun(id: string) {
    setSelectedRunId(id);
    window.location.hash = `run-${id}`;
  }

  return (
    <div className="space-y-4">
      {error && (
        <div className="rounded-lg border px-3 py-2 text-[13px]" style={{ borderColor: "color-mix(in srgb, var(--destructive) 35%, transparent)", background: "color-mix(in srgb, var(--destructive) 10%, transparent)", color: "var(--destructive)" }}>
          {error}
        </div>
      )}

      <div className="grid items-start gap-4 lg:grid-cols-2">
        {/* library */}
        <Card>
          <CardHeader
            icon={<FlaskConical className="size-[15px]" />}
            title="Test library"
            action={<span className="text-[12px] text-muted-foreground/70">{tests.length} total</span>}
          />
          <div className="p-2">
            {tests.length === 0 ? (
              <div className="px-3 py-8 text-center text-[13px] text-muted-foreground/70">No tests yet. Author one on the right — one step per line.</div>
            ) : (
              <ul className="space-y-1">
                {tests.map((test) => (
                  <li key={test.id} className="rounded-lg px-2.5 py-2 hover:bg-accent">
                    <div className="flex items-center gap-2">
                      <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{test.name}</span>
                      <Button size="sm" variant="primary" disabled={busy || !canRun} onClick={() => runTest(test.id)}>
                        Run{instance ? ` · ${instance.name}` : ""}
                      </Button>
                      <button disabled={busy} onClick={() => act(() => api.deleteTest(test.id))} title="Delete" className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-secondary hover:text-[var(--destructive)] disabled:opacity-40">
                        <Trash2 className="size-3.5" />
                      </button>
                    </div>
                    <div className="mt-0.5 truncate text-[11.5px] text-muted-foreground/70">{test.steps.join(" → ")}</div>
                  </li>
                ))}
              </ul>
            )}
            {!canRun && tests.length > 0 && (
              <div className="px-3 pt-2 text-[11.5px] text-muted-foreground/70">
                {instance ? "Start the device to run tests." : "Select a running device to run tests."}
              </div>
            )}
          </div>
        </Card>

        {/* author */}
        <Card>
          <CardHeader icon={<Plus className="size-[15px]" />} title="New test" />
          <form className="space-y-3 p-4" onSubmit={submitTest}>
            <p className="text-[12px] leading-relaxed text-muted-foreground">
              One step per line, in plain language. Steps are grounded visually at run time, so tests self-heal across layout changes. Verbs: open / tap / type … into … / swipe / wait / assert … is visible.
            </p>
            <label className="block">
              <span className="mb-1 block text-[12px] text-muted-foreground">Name</span>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Wi-Fi settings smoke test" className="w-full rounded-lg border bg-secondary px-3 py-2 text-[13px] outline-none focus:border-[var(--ring)]" />
            </label>
            <label className="block">
              <span className="mb-1 block text-[12px] text-muted-foreground">Steps</span>
              <textarea
                rows={7}
                value={stepsText}
                onChange={(e) => setStepsText(e.target.value)}
                placeholder={"open Settings\ntap Network & internet\nassert Airplane mode is visible"}
                className="w-full resize-y rounded-lg border bg-secondary px-3 py-2 font-mono text-[12.5px] leading-relaxed outline-none focus:border-[var(--ring)]"
              />
            </label>
            <Button variant="primary" disabled={busy || !name.trim() || !stepsText.trim()}>Save test</Button>
          </form>
        </Card>
      </div>

      <div className="grid items-start gap-4 lg:grid-cols-[0.85fr_1.15fr]">
        {/* runs */}
        <Card>
          <CardHeader icon={<History className="size-[15px]" />} title={scoped ? "Runs on this device" : "Run history"} />
          <div className="p-2">
            {visibleRuns.length === 0 ? (
              <div className="px-3 py-8 text-center text-[13px] text-muted-foreground/70">No runs yet.</div>
            ) : (
              <ul className="space-y-0.5">
                {visibleRuns.map((run) => (
                  <li key={run.id}>
                    <button onClick={() => openRun(run.id)} className={`flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left hover:bg-accent ${selectedRunId === run.id ? "bg-accent" : ""}`}>
                      <span className="size-2 shrink-0 rounded-full" style={{ background: run.status === "passed" ? "var(--running)" : run.status === "running" ? "var(--warn)" : "var(--destructive)" }} />
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-[13px] font-medium">{run.testName || run.testId}</div>
                        <div className="truncate text-[11.5px] text-muted-foreground/70">
                          {run.steps.filter((s) => s.pass).length}/{run.steps.length} steps · {instances.find((i) => i.id === run.instanceId)?.name ?? run.instanceId}
                        </div>
                      </div>
                      <time className="shrink-0 font-mono text-[10.5px] text-muted-foreground/60">{new Date(run.startedAt).toLocaleTimeString()}</time>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </Card>

        {selectedRun ? (
          <TestReport run={selectedRun} />
        ) : (
          <div className="grid min-h-[240px] place-items-center rounded-xl border border-dashed bg-secondary/40 px-6 text-center text-[13.5px] text-muted-foreground" style={{ borderColor: "var(--border-strong)" }}>
            <p className="max-w-xs">Select a run to open its report — video, step timeline, and grounding dots.</p>
          </div>
        )}
      </div>
    </div>
  );
}
