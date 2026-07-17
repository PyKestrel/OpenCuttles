import { useCallback, useEffect, useState } from "react";
import { Bell } from "lucide-react";
import { Card, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { api } from "@/api";
import type { NotificationConfig } from "@/types";

type Form = { url: string; onlyOnFailure: boolean; secretHeader: string; secret: string };

const EMPTY: Form = { url: "", onlyOnFailure: false, secretHeader: "", secret: "" };

// Admin-only generic-webhook notification config. The secret value is write-only
// — the backend never returns it, so the field is blank on load and left blank
// keeps the stored value.
export function NotificationSettings() {
  const [cfg, setCfg] = useState<NotificationConfig>();
  const [form, setForm] = useState<Form>(EMPTY);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string }>();

  const load = useCallback(async () => {
    const c = await api.notifications();
    setCfg(c);
    setForm({ url: c.url || "", onlyOnFailure: c.onlyOnFailure, secretHeader: c.secretHeader || "", secret: "" });
  }, []);

  useEffect(() => {
    load().catch((e) => setMsg({ ok: false, text: e instanceof Error ? e.message : "Failed to load" }));
  }, [load]);

  function set<K extends keyof Form>(k: K, v: Form[K]) {
    setForm((f) => ({ ...f, [k]: v }));
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
      const c = await api.saveNotifications({
        url: form.url.trim(),
        onlyOnFailure: form.onlyOnFailure,
        secretHeader: form.secretHeader.trim(),
        ...(form.secret ? { secret: form.secret } : {}),
      });
      setCfg(c);
      setForm((f) => ({ ...f, secret: "" }));
      setMsg({ ok: true, text: form.url.trim() ? "Saved. Finished cycle runs will POST to this webhook." : "Saved. Notifications disabled." });
    });

  const clearSecret = () =>
    run(async () => {
      const c = await api.saveNotifications({ url: form.url.trim(), onlyOnFailure: form.onlyOnFailure, secretHeader: form.secretHeader.trim(), secret: "" });
      setCfg(c);
      setForm((f) => ({ ...f, secret: "" }));
      setMsg({ ok: true, text: "Stored secret removed." });
    });

  return (
    <Card>
      <CardHeader
        icon={<Bell className="size-[15px]" />}
        title="Notifications"
        action={
          cfg?.url ? (
            <span className="text-[11px] text-muted-foreground/70">{cfg.onlyOnFailure ? "on failure" : "on completion"}</span>
          ) : (
            <span className="text-[11px] text-muted-foreground/70">disabled</span>
          )
        }
      />
      <div className="space-y-3.5 p-4">
        <p className="text-[12px] leading-relaxed text-muted-foreground">
          POST a JSON summary to a generic webhook when a test cycle finishes — wire it to Slack, Teams, Discord, PagerDuty, or your own service. Leave the URL blank to disable.
        </p>

        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Webhook URL">
            <Input value={form.url} onChange={(e) => set("url", e.target.value)} placeholder="https://hooks.example.com/…" />
          </Field>
          <Field label="Auth header name (optional)">
            <Input value={form.secretHeader} onChange={(e) => set("secretHeader", e.target.value)} placeholder="X-Webhook-Token" />
          </Field>
          <Field label="Auth header value (optional)">
            <Input
              type="password"
              value={form.secret}
              onChange={(e) => set("secret", e.target.value)}
              placeholder={cfg?.secretSet ? "•••••••• (leave blank to keep)" : "secret"}
              autoComplete="new-password"
            />
          </Field>
          <div className="flex items-end gap-4">
            <label className="flex items-center gap-2 text-[12.5px] text-muted-foreground">
              <Checkbox checked={form.onlyOnFailure} onCheckedChange={(v) => set("onlyOnFailure", v === true)} />
              Only notify on failure
            </label>
            {cfg?.secretSet && (
              <button onClick={clearSecret} disabled={busy} className="text-[12px] font-medium text-muted-foreground hover:text-[var(--destructive)] disabled:opacity-50">
                Remove secret
              </button>
            )}
          </div>
        </div>

        {cfg && !cfg.secretStorageEnabled && (
          <div className="rounded-lg border px-3 py-2 text-[12px]" style={{ borderColor: "color-mix(in srgb, var(--warn) 40%, transparent)", background: "color-mix(in srgb, var(--warn) 10%, transparent)", color: "var(--warn)" }}>
            Secret storage is disabled — set <code className="font-mono">OPENCUTTLES_SECRET_KEY</code> on the server to store an auth header value. The webhook still works without one.
          </div>
        )}

        {msg && (
          <div className="text-[12.5px]" style={{ color: msg.ok ? "var(--running)" : "var(--destructive)" }}>
            {msg.text}
          </div>
        )}

        <Button variant="primary" disabled={busy} onClick={save}>
          Save notifications
        </Button>
      </div>
    </Card>
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
