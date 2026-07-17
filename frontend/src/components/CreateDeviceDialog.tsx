import { useEffect, useState, type FormEvent } from "react";
import { Check, Copy, Download, X } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { api } from "@/api";
import type { AndroidVersion, Image, Instance, Platform, RunnerDownload } from "@/types";

const RESOLUTION_PRESETS = [
  { id: "phone", label: "Phone · 720 × 1280 (320 dpi)", width: 720, height: 1280, dpi: 320 },
  { id: "phone-hd", label: "Phone HD · 1080 × 1920 (440 dpi)", width: 1080, height: 1920, dpi: 440 },
  { id: "tablet", label: "Tablet · 1200 × 1920 (240 dpi)", width: 1200, height: 1920, dpi: 240 },
  { id: "compact", label: "Compact · 480 × 800 (240 dpi)", width: 480, height: 800, dpi: 240 },
];

type DesktopOS = Exclude<Platform, "android">;
type Mode = "android" | "desktop";

export function CreateDeviceDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: (instance: Instance) => void;
}) {
  const [mode, setMode] = useState<Mode>("android");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Android
  const [versions, setVersions] = useState<AndroidVersion[]>([]);
  const [images, setImages] = useState<Image[]>([]);
  const [androidVersion, setAndroidVersion] = useState("");
  const [imageId, setImageId] = useState("");
  const [cpuCores, setCpuCores] = useState(2);
  const [memoryMb, setMemoryMb] = useState(4096);
  const [resolution, setResolution] = useState(RESOLUTION_PRESETS[0].id);
  const [showAdvanced, setShowAdvanced] = useState(false);

  // Desktop
  const [desktopOS, setDesktopOS] = useState<DesktopOS>("windows");
  const [enrolled, setEnrolled] = useState<{ instance: Instance; token: string } | null>(null);

  useEffect(() => {
    if (!open) return;
    api.androidVersions().then((v) => {
      setVersions(v ?? []);
      setAndroidVersion((cur) => cur || v?.[0]?.id || "");
    }).catch(() => undefined);
    api.images().then((im) => setImages(im ?? [])).catch(() => undefined);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  if (!open) return null;

  function close() {
    onOpenChange(false);
    // reset transient state after the close animation
    setTimeout(() => {
      setEnrolled(null);
      setError("");
      setName("");
    }, 0);
  }

  const preset = RESOLUTION_PRESETS.find((p) => p.id === resolution) ?? RESOLUTION_PRESETS[0];

  async function deployAndroid(event: FormEvent) {
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
      close();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to deploy device");
    } finally {
      setBusy(false);
    }
  }

  async function onboardDesktop(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await api.onboardDesktop(name.trim(), desktopOS);
      setEnrolled({ instance: res.instance, token: res.enrollmentToken });
      onCreated(res.instance);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to onboard device");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 grid place-items-center p-4" style={{ background: "color-mix(in srgb, #05080b 55%, transparent)" }} onClick={close}>
      <div className="w-full max-w-[480px] overflow-hidden rounded-2xl border bg-card" style={{ boxShadow: "0 20px 60px rgba(0,0,0,0.35)" }} onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center gap-2 border-b px-5 py-3.5" style={{ borderColor: "var(--hairline)" }}>
          <h2 className="text-[15px] font-semibold">{enrolled ? "Device registered" : "Add a device"}</h2>
          <button onClick={close} className="ml-auto grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-accent">
            <X className="size-4" />
          </button>
        </div>

        {enrolled ? (
          <EnrolledView instance={enrolled.instance} token={enrolled.token} onDone={close} />
        ) : (
          <div className="p-5">
            {/* mode toggle */}
            <div className="mb-4 inline-flex rounded-lg border p-0.5" style={{ background: "var(--secondary)" }}>
              {(["android", "desktop"] as Mode[]).map((m) => (
                <button
                  key={m}
                  onClick={() => { setMode(m); setError(""); }}
                  className={cn(
                    "rounded-md px-3.5 py-1.5 text-[13px] font-medium capitalize transition-colors",
                    mode === m ? "text-foreground" : "text-muted-foreground hover:text-foreground",
                  )}
                  style={mode === m ? { background: "var(--card)", boxShadow: "var(--card-shadow)" } : undefined}
                >
                  {m === "android" ? "Android (deploy)" : "Desktop (onboard)"}
                </button>
              ))}
            </div>

            {error && <div className="mb-3 rounded-lg border px-3 py-2 text-[13px]" style={{ borderColor: "color-mix(in srgb, var(--destructive) 35%, transparent)", background: "color-mix(in srgb, var(--destructive) 10%, transparent)", color: "var(--destructive)" }}>{error}</div>}

            {mode === "android" ? (
              <form className="space-y-3.5" onSubmit={deployAndroid}>
                <p className="text-[12px] leading-relaxed text-muted-foreground">
                  Testral fetches the image with <code className="font-mono">cvd fetch</code> and launches a Cuttlefish VM — no manual image registration.
                </p>
                <Field label="Name">
                  <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="android-01" />
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
                    <Input type="number" min={1} max={16} value={cpuCores} onChange={(e) => setCpuCores(Number(e.target.value))} />
                  </Field>
                  <Field label="Memory (MB)">
                    <Input type="number" min={1024} step={512} value={memoryMb} onChange={(e) => setMemoryMb(Number(e.target.value))} />
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
                  <Button type="button" onClick={close}>Cancel</Button>
                  <Button variant="primary" disabled={busy || !name.trim() || (!androidVersion && !imageId)}>
                    {busy ? "Deploying…" : "Deploy device"}
                  </Button>
                </div>
              </form>
            ) : (
              <form className="space-y-3.5" onSubmit={onboardDesktop}>
                <p className="text-[12px] leading-relaxed text-muted-foreground">
                  Onboard a real machine for UI testing. You'll get a one-time token to start the Testral runner on that machine — it dials home, so no inbound ports are needed.
                </p>
                <Field label="Name">
                  <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-workstation" />
                </Field>
                <Field label="Operating system">
                  <select value={desktopOS} onChange={(e) => setDesktopOS(e.target.value as DesktopOS)} className={inputCls}>
                    <option value="windows">Windows</option>
                    <option value="linux">Linux</option>
                    <option value="macos">macOS</option>
                  </select>
                </Field>
                <div className="flex justify-end gap-2 pt-1">
                  <Button type="button" onClick={close}>Cancel</Button>
                  <Button variant="primary" disabled={busy || !name.trim()}>
                    {busy ? "Registering…" : "Register device"}
                  </Button>
                </div>
              </form>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function EnrolledView({ instance, token, onDone }: { instance: Instance; token: string; onDone: () => void }) {
  const origin = window.location.origin;
  const isWindows = instance.platform === "windows";

  const [runners, setRunners] = useState<RunnerDownload[] | null>(null);
  const [showManual, setShowManual] = useState(false);
  useEffect(() => {
    api.runnerDownloads().then(setRunners).catch(() => setRunners([]));
  }, []);
  // Only the builds for this device's platform are relevant (macOS has two archs).
  const forPlatform = (runners ?? []).filter((r) => r.platform === instance.platform);
  const oneLine = oneLineInstall(instance.platform, origin, token);

  async function grab(arch: string) {
    try {
      await api.downloadRunner(instance.platform, arch);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Download failed");
    }
  }

  return (
    <div className="space-y-4 p-5">
      <div className="flex items-center gap-2 text-[13.5px]" style={{ color: "var(--running)" }}>
        <Check className="size-4" /> <span className="font-medium">{instance.name}</span> registered — now start its runner.
      </div>

      {forPlatform.length === 0 && runners !== null ? (
        <p className="rounded-lg border px-3 py-2 text-[11.5px] text-muted-foreground" style={{ borderColor: "var(--border-strong)" }}>
          No prebuilt runner is bundled with this server. Build it from <code className="font-mono">runner/</code> in the repo (<code className="font-mono">go build .</code>) — see its README for the per-OS dependencies — then run it with the token below.
        </p>
      ) : (
        <div>
          <div className="mb-1.5 text-[12px] text-muted-foreground">
            One-line install — paste into an interactive {isWindows ? "PowerShell" : "terminal"} on {instance.name}
          </div>
          <CopyField label={isWindows ? "PowerShell" : "bash / zsh"} value={oneLine} mono />
          <p className="mt-1.5 text-[11px] text-muted-foreground/80">
            Downloads the runner, sets it to <strong>auto-start at login</strong>, and connects it now — the device flips to <strong>online</strong> in a moment. No admin needed. Run in a logged-in desktop session. The token is shown only once. Remove later with <code className="font-mono">opencuttles-runner uninstall</code>.
          </p>
        </div>
      )}

      <button
        onClick={() => setShowManual((v) => !v)}
        className="text-[11.5px] font-medium text-muted-foreground hover:text-foreground"
      >
        {showManual ? "Hide" : "Prefer to download manually?"}
      </button>

      {showManual && (
        <div className="space-y-2.5 rounded-lg border p-3" style={{ borderColor: "var(--border-strong)" }}>
          {forPlatform.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {forPlatform.map((r) => (
                <Button key={r.arch} variant="secondary" size="sm" onClick={() => grab(r.arch)}>
                  <Download className="size-3.5" /> {forPlatform.length > 1 ? archLabel(r.arch) : "Download runner"}
                  <span className="text-[11px] text-muted-foreground/70">{formatMB(r.sizeBytes)}</span>
                </Button>
              ))}
            </div>
          )}
          <CopyField label="Enrollment token" value={token} mono />
          <CopyField
            label="Run command"
            value={isWindows ? `.\\opencuttles-runner.exe --appliance ${origin} --token ${token}` : `./opencuttles-runner --appliance ${origin} --token ${token}`}
            mono
          />
        </div>
      )}

      <div className="flex justify-end pt-1">
        <Button variant="primary" onClick={onDone}>Done</Button>
      </div>
    </div>
  );
}

// oneLineInstall builds a copy-paste command that downloads the runner (via the
// enrollment token, so it works on the target machine with no browser session),
// then installs it to auto-start at login and connects it now. The token goes in
// the Authorization header, never the URL.
function oneLineInstall(platform: string, origin: string, token: string): string {
  const url = (arch: string) => `${origin}/api/v1/runner/download?platform=${platform}&arch=${arch}`;
  if (platform === "windows") {
    return (
      `$t='${token}'; ` +
      `iwr '${url("amd64")}' -Headers @{Authorization="Bearer $t"} -OutFile opencuttles-runner.exe; ` +
      `.\\opencuttles-runner.exe install --appliance '${origin}' --token $t`
    );
  }
  // Linux/macOS: detect the arch (Apple Silicon vs Intel) so one command covers both.
  return (
    `T='${token}'; A=$(uname -m); [ "$A" = x86_64 ] && A=amd64; [ "$A" = aarch64 ] && A=arm64; ` +
    `curl -fsSL -H "Authorization: Bearer $T" '${origin}/api/v1/runner/download?platform=${platform}&arch='"$A" -o opencuttles-runner && ` +
    `chmod +x opencuttles-runner && ./opencuttles-runner install --appliance '${origin}' --token "$T"`
  );
}

function archLabel(arch: string): string {
  if (arch === "arm64") return "Apple Silicon";
  if (arch === "amd64") return "Intel / x64";
  return arch;
}

function formatMB(bytes: number): string {
  if (!bytes) return "";
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function CopyField({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  const [copied, setCopied] = useState(false);
  function copy() {
    void navigator.clipboard?.writeText(value);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  }
  return (
    <div>
      <div className="mb-1 text-[12px] text-muted-foreground">{label}</div>
      <div className="flex items-stretch gap-2">
        <code className={cn("min-w-0 flex-1 overflow-x-auto rounded-lg border bg-secondary px-3 py-2 text-[12px] whitespace-nowrap", mono && "font-mono")} style={{ borderColor: "var(--border-strong)" }}>
          {value}
        </code>
        <button onClick={copy} title="Copy" className="grid w-9 shrink-0 place-items-center rounded-lg border bg-secondary text-muted-foreground hover:bg-accent hover:text-foreground" style={{ borderColor: "var(--border-strong)" }}>
          {copied ? <Check className="size-3.5" style={{ color: "var(--running)" }} /> : <Copy className="size-3.5" />}
        </button>
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
