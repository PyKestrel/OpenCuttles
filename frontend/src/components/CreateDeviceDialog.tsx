import { useEffect, useState, type FormEvent } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { api } from "@/api";
import type { AndroidVersion, Image, Instance } from "@/types";

const RESOLUTION_PRESETS = [
  { id: "phone", label: "Phone · 720 × 1280 (320 dpi)", width: 720, height: 1280, dpi: 320 },
  { id: "phone-hd", label: "Phone HD · 1080 × 1920 (440 dpi)", width: 1080, height: 1920, dpi: 440 },
  { id: "tablet", label: "Tablet · 1200 × 1920 (240 dpi)", width: 1200, height: 1920, dpi: 240 },
  { id: "compact", label: "Compact · 480 × 800 (240 dpi)", width: 480, height: 800, dpi: 240 },
];

export function CreateDeviceDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: (instance: Instance) => void;
}) {
  const [versions, setVersions] = useState<AndroidVersion[]>([]);
  const [images, setImages] = useState<Image[]>([]);
  const [name, setName] = useState("");
  const [androidVersion, setAndroidVersion] = useState("");
  const [imageId, setImageId] = useState("");
  const [cpuCores, setCpuCores] = useState(2);
  const [memoryMb, setMemoryMb] = useState(4096);
  const [resolution, setResolution] = useState(RESOLUTION_PRESETS[0].id);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    api.androidVersions().then((v) => {
      setVersions(v ?? []);
      setAndroidVersion((cur) => cur || v?.[0]?.id || "");
    }).catch(() => undefined);
    api.images().then((im) => setImages(im ?? [])).catch(() => undefined);
  }, [open]);

  // Close on Escape.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onOpenChange(false);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onOpenChange]);

  if (!open) return null;

  const preset = RESOLUTION_PRESETS.find((p) => p.id === resolution) ?? RESOLUTION_PRESETS[0];

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const instance = await api.createInstance({
        name: name.trim(),
        androidVersion: imageId ? undefined : androidVersion,
        imageId: imageId || undefined,
        cpuCores,
        memoryMb,
        displayWidth: preset.width,
        displayHeight: preset.height,
        dpi: preset.dpi,
      });
      onCreated(instance);
      onOpenChange(false);
      setName("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to deploy device");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 grid place-items-center p-4" style={{ background: "color-mix(in srgb, #05080b 55%, transparent)" }} onClick={() => onOpenChange(false)}>
      <div className="w-full max-w-[460px] overflow-hidden rounded-2xl border bg-card" style={{ boxShadow: "0 20px 60px rgba(0,0,0,0.35)" }} onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center gap-2 border-b px-5 py-3.5" style={{ borderColor: "var(--hairline)" }}>
          <h2 className="text-[15px] font-semibold">Deploy Android device</h2>
          <button onClick={() => onOpenChange(false)} className="ml-auto grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-accent">
            <X className="size-4" />
          </button>
        </div>

        <form className="space-y-3.5 p-5" onSubmit={submit}>
          <p className="text-[12px] leading-relaxed text-muted-foreground">
            OpenCuttles fetches the image automatically with <code className="font-mono">cvd fetch</code> and launches the device — no manual image registration required.
          </p>

          {error && <div className="rounded-lg border px-3 py-2 text-[13px]" style={{ borderColor: "color-mix(in srgb, var(--destructive) 35%, transparent)", background: "color-mix(in srgb, var(--destructive) 10%, transparent)", color: "var(--destructive)" }}>{error}</div>}

          <Field label="Name">
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="android-01" className={inputCls} />
          </Field>

          <Field label="Android version">
            <select value={androidVersion} onChange={(e) => setAndroidVersion(e.target.value)} disabled={Boolean(imageId)} className={inputCls}>
              {versions.length === 0 && <option value="">Loading versions…</option>}
              {versions.map((v) => (
                <option value={v.id} key={v.id}>{v.label}</option>
              ))}
            </select>
          </Field>

          <Field label="Resolution">
            <select value={resolution} onChange={(e) => setResolution(e.target.value)} className={inputCls}>
              {RESOLUTION_PRESETS.map((p) => (
                <option value={p.id} key={p.id}>{p.label}</option>
              ))}
            </select>
          </Field>

          <div className="grid grid-cols-2 gap-3">
            <Field label="CPU cores">
              <input type="number" min={1} max={16} value={cpuCores} onChange={(e) => setCpuCores(Number(e.target.value))} className={inputCls} />
            </Field>
            <Field label="Memory (MB)">
              <input type="number" min={1024} step={512} value={memoryMb} onChange={(e) => setMemoryMb(Number(e.target.value))} className={inputCls} />
            </Field>
          </div>

          <button type="button" onClick={() => setShowAdvanced((v) => !v)} className="text-[12.5px] font-medium text-primary">
            {showAdvanced ? "Hide advanced" : "Advanced: use a registered image"}
          </button>
          {showAdvanced && (
            <Field label="Registered image (overrides version)">
              <select value={imageId} onChange={(e) => setImageId(e.target.value)} className={inputCls}>
                <option value="">Auto-fetch selected Android version</option>
                {images.map((image) => (
                  <option value={image.id} key={image.id}>
                    {image.name}{image.status && image.status !== "ready" ? ` (${image.status})` : ""}
                  </option>
                ))}
              </select>
            </Field>
          )}

          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button variant="primary" disabled={busy || !name.trim() || (!androidVersion && !imageId)}>
              {busy ? "Deploying…" : "Deploy device"}
            </Button>
          </div>
        </form>
      </div>
    </div>
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
