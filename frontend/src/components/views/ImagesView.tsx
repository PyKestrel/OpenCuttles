import { useCallback, useEffect, useState, type FormEvent } from "react";
import { ImageIcon, PackagePlus, Trash2 } from "lucide-react";
import { Card, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { api } from "@/api";
import type { Image, Principal } from "@/types";
import { can } from "@/lib/permissions";

export function ImagesView({ principal }: { principal: Principal }) {
  const [images, setImages] = useState<Image[]>([]);
  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [deletingId, setDeletingId] = useState("");
  const canOperate = can(principal, "operate");
  // Deletion reclaims tens of GB and is irreversible, so it is admin-only —
  // matching the DELETE route's permission.
  const canDelete = can(principal, "admin");

  const refresh = useCallback(async () => {
    try {
      setImages((await api.images()) ?? []);
      // Clear on success: the first load can race the session cookie and fail
      // with "unauthorized", and without this the stale banner stays up
      // alongside the images that subsequently loaded fine.
      setError("");
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

  async function remove(image: Image) {
    const confirmed = window.confirm(
      `Delete image "${image.name}"?\n\n${image.path}\n\n` +
        "This permanently removes the image files from the host and cannot be undone.",
    );
    if (!confirmed) return;

    setDeletingId(image.id);
    setError("");
    try {
      const result = await api.deleteImage(image.id);
      if (!result.filesRemoved) {
        // The row is gone but the bytes are not, so say so rather than let the
        // operator assume they reclaimed the space.
        setError(
          `Removed "${image.name}" from the catalog, but its files could not be deleted. Check the server log and remove ${image.path} by hand.`,
        );
      }
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to delete image");
    } finally {
      setDeletingId("");
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
                    {image.sizeBytes ? (
                      <span className="shrink-0 text-[11.5px] tabular-nums text-muted-foreground/70">{formatSize(image.sizeBytes)}</span>
                    ) : null}
                    <span className="shrink-0 rounded-md px-1.5 py-0.5 text-[10.5px] font-semibold uppercase" style={statusBadge(image.status)}>
                      {image.status ?? "ready"}
                    </span>
                    {canDelete && (
                      <Button
                        variant="ghost"
                        className="size-7 shrink-0 p-0 text-muted-foreground/70 hover:text-[var(--destructive)]"
                        title={`Delete ${image.name}`}
                        aria-label={`Delete ${image.name}`}
                        disabled={deletingId === image.id}
                        onClick={() => remove(image)}
                      >
                        <Trash2 className="size-[14px]" />
                      </Button>
                    )}
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
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="AOSP main" />
            </label>
            <label className="block">
              <span className="mb-1 block text-[12px] text-muted-foreground">Image path</span>
              <Input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/var/lib/opencuttles/images/aosp" />
            </label>
            <Button variant="primary" disabled={!canOperate || busy || !name || !path}>Register image</Button>
            {!canOperate && <div className="text-[11.5px] text-muted-foreground/70">Your role cannot register images.</div>}
          </form>
        </Card>
      </div>
    </div>
  );
}


function statusBadge(status?: string): React.CSSProperties {
  const c = status === "ready" || !status ? "var(--running)" : status === "error" ? "var(--destructive)" : "var(--warn)";
  return { color: c, background: `color-mix(in srgb, ${c} 12%, transparent)`, border: `1px solid color-mix(in srgb, ${c} 30%, transparent)` };
}

// Images are 10-20 GB, so GiB with one decimal is the useful granularity.
function formatSize(bytes: number): string {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)} GB`;
  if (bytes >= 1 << 20) return `${Math.round(bytes / (1 << 20))} MB`;
  return `${Math.round(bytes / 1024)} KB`;
}
