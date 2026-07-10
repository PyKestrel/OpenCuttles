import { useCallback, useEffect, useState } from "react";
import { BrainCircuit, Check, Plug, X } from "lucide-react";
import { Card, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { api } from "@/api";
import type { AgentModelConfig, AgentModelPreset, AgentModelUpdate } from "@/types";

type Form = { providerId: string; api: string; baseUrl: string; model: string; apiKey: string };

const EMPTY: Form = { providerId: "", api: "openai-completions", baseUrl: "", model: "", apiKey: "" };

// Admin-only agent model configuration. The API key is write-only — the backend
// never returns it, so the field is always blank on load and "leave blank to
// keep" preserves the stored key.
export function AgentModelSettings() {
  const [cfg, setCfg] = useState<AgentModelConfig>();
  const [form, setForm] = useState<Form>(EMPTY);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string }>();
  const [test, setTest] = useState<{ ok: boolean; message: string }>();

  const load = useCallback(async () => {
    const c = await api.agentModel();
    setCfg(c);
    setForm({ providerId: c.providerId || "", api: c.api || "openai-completions", baseUrl: c.baseUrl || "", model: c.model || "", apiKey: "" });
  }, []);

  useEffect(() => {
    load().catch((e) => setMsg({ ok: false, text: e instanceof Error ? e.message : "Failed to load" }));
  }, [load]);

  function set<K extends keyof Form>(k: K, v: Form[K]) {
    setForm((f) => ({ ...f, [k]: v }));
    setTest(undefined);
  }

  function applyPreset(p: AgentModelPreset) {
    setForm((f) => ({ ...f, providerId: p.providerId, api: p.api, baseUrl: p.baseUrl, model: p.model }));
    setTest(undefined);
  }

  function payload(extra?: Partial<AgentModelUpdate>): AgentModelUpdate {
    const body: AgentModelUpdate = {
      providerId: form.providerId.trim(),
      api: form.api,
      baseUrl: form.baseUrl.trim(),
      model: form.model.trim(),
    };
    if (form.apiKey) body.apiKey = form.apiKey; // omit → keep stored key
    return { ...body, ...extra };
  }

  async function run(fn: () => Promise<void>) {
    setBusy(true);
    setMsg(undefined);
    try {
      await fn();
    } catch (e) {
      setMsg({ ok: false, text: e instanceof Error ? e.message : "Action failed" });
    } finally {
      setBusy(false);
    }
  }

  const save = () =>
    run(async () => {
      const c = await api.saveAgentModel(payload());
      setCfg(c);
      setForm((f) => ({ ...f, apiKey: "" }));
      setMsg({ ok: true, text: "Saved. New agent conversations will use this model." });
    });

  const runTest = () =>
    run(async () => {
      setTest(await api.testAgentModel(payload()));
    });

  const clearKey = () =>
    run(async () => {
      const c = await api.saveAgentModel(payload({ apiKey: "" }));
      setCfg(c);
      setForm((f) => ({ ...f, apiKey: "" }));
      setMsg({ ok: true, text: "Stored API key removed." });
    });

  const preset = cfg?.presets.find((p) => p.providerId === form.providerId);
  const needsKey = preset?.needsKey ?? form.api !== "openai-completions";
  const keyMissing = needsKey && !cfg?.keySet && !form.apiKey;

  return (
    <Card>
      <CardHeader
        icon={<BrainCircuit className="size-[15px]" />}
        title="Agent model"
        action={
          cfg?.providerId ? (
            <span className="font-mono text-[11px] text-muted-foreground/70">
              {cfg.providerId}/{cfg.model}
            </span>
          ) : (
            <span className="text-[11px] text-muted-foreground/70">using local default</span>
          )
        }
      />
      <div className="space-y-3.5 p-4">
        <p className="text-[12px] leading-relaxed text-muted-foreground">
          Choose the model that powers the natural-language agent. Any Flue-supported provider works — local Ollama or a hosted OpenAI/Anthropic/Google/Azure endpoint. API keys are encrypted at rest and never shown again.
        </p>

        {cfg && !cfg.secretStorageEnabled && (
          <div className="rounded-lg border px-3 py-2 text-[12px]" style={{ borderColor: "color-mix(in srgb, var(--warn) 40%, transparent)", background: "color-mix(in srgb, var(--warn) 10%, transparent)", color: "var(--warn)" }}>
            Secret storage is disabled — set <code className="font-mono">OPENCUTTLES_SECRET_KEY</code> on the server to store API keys. Keyless providers (local Ollama) still work.
          </div>
        )}

        {/* presets */}
        <div className="flex flex-wrap gap-1.5">
          {cfg?.presets.map((p) => (
            <button
              key={p.label}
              onClick={() => applyPreset(p)}
              className={`rounded-lg border px-2.5 py-1.5 text-[12px] font-medium hover:bg-accent ${form.providerId === p.providerId ? "border-[var(--primary)] text-primary" : "text-muted-foreground"}`}
              style={{ borderColor: form.providerId === p.providerId ? "var(--primary)" : "var(--border-strong)" }}
            >
              {p.label}
            </button>
          ))}
        </div>

        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Provider id">
            <input value={form.providerId} onChange={(e) => set("providerId", e.target.value)} placeholder="openai" className={inputCls} />
          </Field>
          <Field label="API method">
            <select value={form.api} onChange={(e) => set("api", e.target.value)} className={inputCls}>
              {(cfg?.supportedApis ?? [form.api]).map((a) => (
                <option key={a} value={a}>{a}</option>
              ))}
            </select>
          </Field>
          <Field label="Model">
            <input value={form.model} onChange={(e) => set("model", e.target.value)} placeholder="gpt-4o-mini" className={inputCls} />
          </Field>
          <Field label="Base URL">
            <input value={form.baseUrl} onChange={(e) => set("baseUrl", e.target.value)} placeholder="https://api.openai.com/v1" className={inputCls} />
          </Field>
          <Field label="API key">
            <input
              type="password"
              value={form.apiKey}
              onChange={(e) => set("apiKey", e.target.value)}
              placeholder={cfg?.keySet ? "•••••••• (leave blank to keep)" : "API key"}
              className={inputCls}
              autoComplete="new-password"
            />
          </Field>
          <div className="flex items-end">
            {cfg?.keySet && (
              <button onClick={clearKey} disabled={busy} className="text-[12px] font-medium text-muted-foreground hover:text-[var(--destructive)] disabled:opacity-50">
                Remove stored key
              </button>
            )}
          </div>
        </div>

        {keyMissing && (
          <div className="text-[11.5px] text-muted-foreground/80">This provider needs an API key.</div>
        )}

        {test && (
          <div className="flex items-center gap-1.5 text-[12.5px]" style={{ color: test.ok ? "var(--running)" : "var(--destructive)" }}>
            {test.ok ? <Check className="size-3.5" /> : <X className="size-3.5" />}
            {test.message}
          </div>
        )}
        {msg && (
          <div className="text-[12.5px]" style={{ color: msg.ok ? "var(--running)" : "var(--destructive)" }}>
            {msg.text}
          </div>
        )}

        <div className="flex gap-2">
          <Button variant="primary" disabled={busy || !form.providerId.trim() || !form.model.trim()} onClick={save}>
            Save model
          </Button>
          <Button disabled={busy || !form.baseUrl.trim()} onClick={runTest}>
            <Plug className="size-3.5" /> Test connection
          </Button>
        </div>
      </div>
    </Card>
  );
}

const inputCls = "w-full rounded-lg border bg-secondary px-3 py-2 text-[13px] outline-none focus:border-[var(--ring)]";

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1 block text-[12px] text-muted-foreground">{label}</span>
      {children}
    </label>
  );
}
