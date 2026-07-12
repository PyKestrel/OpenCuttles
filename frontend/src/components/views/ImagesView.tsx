import { useCallback, useEffect, useState, type FormEvent } from "react";
import { ImageIcon, PackagePlus } from "lucide-react";
import { Card, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { api } from "@/api";
import type { Image, Principal } from "@/types";
import { can } from "@/lib/permissions";

export function ImagesView({ principal }: { principal: Principal }) {
  const [images, setImages] = useState<Image[]>([]);
  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const canOperate = can(principal, "operate");

  const refresh = useCallback(async () => {
    try {
      setImages((await api.images()) ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load images");
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.createImage({ name, path });
      setName("");
      setPath("");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to register image");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mx-auto max-w-5xl space-y-4 p-5">
      {error && <div className="rounded-lg border px-3 py-2 text-[13px]" style={{ borderColor: "color-mix(in srgb, var(--destructive) 35%, transparent)", background: "color-mix(in srgb, var(--destructive) 10%, transparent)", color: "var(--destructive)" }}>{error}</div>}

      <div className="grid items-start gap-4 lg:grid-cols-[1.4fr_1fr]">
        <Card>
          <CardHeader icon={<ImageIcon className="size-[15px]" />} title="Image catalog" action={<span className="text-[12px] text-muted-foreground/70">{images.length} total</span>} />
          <div className="p-2">
            {images.length === 0 ? (
              <div className="px-3 py-10 text-center text-[13px] text-muted-foreground/70">No images yet. Deploying a device auto-fetches the chosen Android version.</div>
            ) : (
              <ul className="space-y-1">
                {images.map((image) => (
                  <li key={image.id} className="flex items-center gap-3 rounded-lg px-2.5 py-2 hover:bg-accent">
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[13px] font-medium">{image.name}</div>
                      <div className="truncate font-mono text-[11px] text-muted-foreground/70">{image.path}</div>
                    </div>
                    <span className="shrink-0 rounded-md px-1.5 py-0.5 text-[10.5px] font-semibold uppercase" style={statusBadge(image.status)}>
                      {image.status ?? "ready"}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </Card>

        <Card>
          <CardHeader icon={<PackagePlus className="size-[15px]" />} title="Register image" />
          <form className="space-y-3 p-4" onSubmit={submit}>
            <p className="text-[12px] leading-relaxed text-muted-foreground">
              Register an image directory already present on the host. For most workflows just deploy a device — Testral fetches the image automatically.
            </p>
            <label className="block">
              <span className="mb-1 block text-[12px] text-muted-foreground">Name</span>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="AOSP main" className={inputCls} />
            </label>
            <label className="block">
              <span className="mb-1 block text-[12px] text-muted-foreground">Image path</span>
              <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/var/lib/opencuttles/images/aosp" className={inputCls} />
            </label>
            <Button variant="primary" disabled={!canOperate || busy || !name || !path}>Register image</Button>
            {!canOperate && <div className="text-[11.5px] text-muted-foreground/70">Your role cannot register images.</div>}
          </form>
        </Card>
      </div>
    </div>
  );
}

const inputCls = "w-full rounded-lg border bg-secondary px-3 py-2 text-[13px] outline-none focus:border-[var(--ring)]";

function statusBadge(status?: string): React.CSSProperties {
  const c = status === "ready" || !status ? "var(--running)" : status === "error" ? "var(--destructive)" : "var(--warn)";
  return { color: c, background: `color-mix(in srgb, ${c} 12%, transparent)`, border: `1px solid color-mix(in srgb, ${c} 30%, transparent)` };
}
