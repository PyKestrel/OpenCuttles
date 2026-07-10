import { useEffect, useState, type ChangeEvent, type FormEvent } from "react";
import { ChevronRight, RotateCw, Terminal, Upload } from "lucide-react";
import { Card, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { api } from "@/api";
import type { Instance } from "@/types";

const CONTROL_KEYS: [string, string][] = [
  ["Back", "BACK"],
  ["Home", "HOME"],
  ["Recents", "APP_SWITCH"],
  ["Enter", "ENTER"],
  ["Vol +", "VOLUME_UP"],
  ["Vol −", "VOLUME_DOWN"],
  ["Power", "POWER"],
];

// The manual controls the WebRTC console doesn't cover: hardware keys, text
// entry, rotation, installed-app management, and an ad-hoc shell.
export function ControlsTab({ instance }: { instance: Instance }) {
  const id = instance.id;
  const running = instance.state === "running";
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [rotation, setRotation] = useState(0);
  const [typeValue, setTypeValue] = useState("");
  const [apps, setApps] = useState<string[]>([]);
  const [thirdPartyOnly, setThirdPartyOnly] = useState(true);
  const [installStatus, setInstallStatus] = useState("");
  const [shellCmd, setShellCmd] = useState("");
  const [shellOut, setShellOut] = useState("");

  useEffect(() => {
    setError("");
    setApps([]);
    setShellOut("");
    setInstallStatus("");
  }, [id]);

  async function run(action: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await action();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Action failed");
    } finally {
      setBusy(false);
    }
  }

  function rotate() {
    const next = (rotation + 1) % 4;
    setRotation(next);
    run(() => api.controlRotate(id, next));
  }

  function sendText(event: FormEvent) {
    event.preventDefault();
    if (!typeValue) return;
    run(() => api.controlText(id, typeValue)).then(() => setTypeValue(""));
  }

  function loadApps() {
    run(async () => {
      const result = await api.controlListApps(id, thirdPartyOnly);
      setApps(result.packages ?? []);
    });
  }

  function onApkSelected(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    setInstallStatus(`Installing ${file.name}…`);
    run(async () => {
      const result = await api.controlInstallApp(id, file);
      setInstallStatus(`Installed ${result.file}`);
    }).catch(() => setInstallStatus(""));
  }

  function runShell(event: FormEvent) {
    event.preventDefault();
    if (!shellCmd) return;
    run(async () => {
      const result = await api.controlShell(id, shellCmd);
      setShellOut(result.output || "(no output)");
    });
  }

  if (!running) {
    return (
      <div className="grid min-h-[240px] place-items-center rounded-xl border border-dashed bg-secondary/40 px-6 text-center text-[13.5px] text-muted-foreground" style={{ borderColor: "var(--border-strong)" }}>
        <p className="max-w-md">Controls become available once the device is running.</p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {error && (
        <div className="rounded-lg border px-3 py-2 text-[13px]" style={{ borderColor: "color-mix(in srgb, var(--destructive) 35%, transparent)", background: "color-mix(in srgb, var(--destructive) 10%, transparent)", color: "var(--destructive)" }}>
          {error}
        </div>
      )}

      <div className="space-y-4">
        <Card>
          <CardHeader icon={<RotateCw className="size-[15px]" />} title="Input" />
          <div className="space-y-3 p-4">
            <div className="flex flex-wrap gap-1.5">
              {CONTROL_KEYS.map(([label, code]) => (
                <Button key={code} size="sm" disabled={busy} onClick={() => run(() => api.controlKey(id, code))}>
                  {label}
                </Button>
              ))}
              <Button size="sm" disabled={busy} onClick={rotate}>
                <RotateCw className="size-3.5" /> Rotate
              </Button>
            </div>
            <form className="flex gap-2" onSubmit={sendText}>
              <input
                value={typeValue}
                onChange={(e) => setTypeValue(e.target.value)}
                placeholder="Type text into the focused field"
                className="min-w-0 flex-1 rounded-lg border bg-secondary px-3 py-2 text-[13px] outline-none focus:border-[var(--ring)]"
              />
              <Button variant="primary" disabled={busy || !typeValue}>Send</Button>
            </form>
          </div>
        </Card>

        <Card>
          <CardHeader
            icon={<Terminal className="size-[15px]" />}
            title="Shell"
            action={<span className="font-mono text-[11px] text-muted-foreground/70">adb shell</span>}
          />
          <div className="space-y-3 p-4">
            <form className="flex gap-2" onSubmit={runShell}>
              <input
                value={shellCmd}
                onChange={(e) => setShellCmd(e.target.value)}
                placeholder="getprop ro.build.version.release"
                className="min-w-0 flex-1 rounded-lg border bg-secondary px-3 py-2 font-mono text-[12.5px] outline-none focus:border-[var(--ring)]"
              />
              <Button variant="primary" disabled={busy || !shellCmd}>Run</Button>
            </form>
            <pre className="max-h-40 overflow-auto rounded-lg border bg-[#06090c] px-3 py-2 font-mono text-[12px] leading-relaxed text-[#c9d6df]" style={{ borderColor: "var(--border-strong)" }}>
              {shellOut || "Command output appears here."}
            </pre>
          </div>
        </Card>
      </div>

      <Card>
        <CardHeader
          icon={<ChevronRight className="size-[15px]" />}
          title="Applications"
          action={
            <div className="flex items-center gap-2.5">
              <label className="flex cursor-pointer items-center gap-1.5 text-[12px] font-normal text-muted-foreground">
                <input type="checkbox" checked={thirdPartyOnly} onChange={(e) => setThirdPartyOnly(e.target.checked)} className="accent-[var(--primary)]" />
                Third-party only
              </label>
              <Button size="sm" disabled={busy} onClick={loadApps}>Load</Button>
              <label className="inline-flex cursor-pointer items-center gap-1.5 rounded-lg border bg-secondary px-2.5 py-1.5 text-[12.5px] font-medium hover:bg-accent">
                <Upload className="size-3.5" /> Install APK
                <input type="file" accept=".apk" onChange={onApkSelected} disabled={busy} hidden />
              </label>
            </div>
          }
        />
        <div className="p-4">
          {installStatus && <div className="mb-2 text-[12px] text-muted-foreground">{installStatus}</div>}
          {apps.length === 0 ? (
            <div className="py-6 text-center text-[13px] text-muted-foreground/70">No apps loaded. Click “Load”.</div>
          ) : (
            <ul className="divide-y" style={{ borderColor: "var(--hairline)" }}>
              {apps.map((pkg) => (
                <li key={pkg} className="flex items-center gap-3 py-1.5">
                  <code className="min-w-0 flex-1 truncate font-mono text-[12.5px]">{pkg}</code>
                  <Button size="sm" disabled={busy} onClick={() => run(() => api.controlLaunchApp(id, pkg))}>Launch</Button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </Card>
    </div>
  );
}
