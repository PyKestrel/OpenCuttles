import { useCallback, useEffect, useRef, useState } from "react";
import { Package, Upload } from "lucide-react";
import { toast } from "sonner";
import { Card, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/api";
import { platformLabel } from "@/lib/platform";
import type { Build, Platform, Principal } from "@/types";
import { can } from "@/lib/permissions";

const PLATFORMS: Platform[] = ["android", "windows", "linux", "macos"];
const EXT: Record<Platform, string> = { android: ".apk", windows: ".exe,.msi", linux: ".deb,.rpm,.AppImage,.run,.sh", macos: ".dmg,.pkg,.zip" };

export function BuildsView({ principal }: { principal: Principal }) {
  const [builds, setBuilds] = useState<Build[]>([]);
  const [platform, setPlatform] = useState<Platform>("windows");
  const [version, setVersion] = useState("");
  const [busy, setBusy] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const canOperate = can(principal, "operate");

  const refresh = useCallback(async () => {
    setBuilds((await api.builds().catch(() => [])) ?? []);
  }, []);
  useEffect(() => {
    refresh();
  }, [refresh]);

  async function onFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    setBusy(true);
    try {
      await api.uploadBuild(platform, file, version.trim() || undefined);
      toast.success(`Uploaded ${file.name}`, { description: "On-new-build cycles for this platform will run automatically." });
      setVersion("");
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Upload failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mx-auto w-full max-w-4xl p-5">
      <h1 className="mb-4 text-[18px] font-semibold tracking-tight">Builds</h1>
      {canOperate && (
        <Card className="mb-4">
          <CardHeader icon={<Upload className="size-[15px]" />} title="Upload a build" />
          <div className="flex flex-wrap items-end gap-3 p-4">
            <label className="block">
              <span className="mb-1 block text-[12px] text-muted-foreground">Platform</span>
              <Select value={platform} onValueChange={(v) => setPlatform(v as Platform)}>
                <SelectTrigger className="w-40"><SelectValue /></SelectTrigger>
                <SelectContent>{PLATFORMS.map((p) => <SelectItem key={p} value={p}>{platformLabel(p)}</SelectItem>)}</SelectContent>
              </Select>
            </label>
            <label className="block">
              <span className="mb-1 block text-[12px] text-muted-foreground">Version (optional)</span>
              <Input value={version} onChange={(e) => setVersion(e.target.value)} placeholder="1.4.0" className="w-40" />
            </label>
            <input ref={fileRef} type="file" accept={EXT[platform]} hidden onChange={onFile} />
            <Button variant="primary" disabled={busy} onClick={() => fileRef.current?.click()}>
              <Upload className="size-3.5" /> {busy ? "Uploading…" : "Choose artifact"}
            </Button>
            <span className="text-[11.5px] text-muted-foreground/70">Accepts {EXT[platform]}</span>
          </div>
        </Card>
      )}

      <Card>
        <CardHeader icon={<Package className="size-[15px]" />} title="Uploaded builds" action={<span className="text-[12px] text-muted-foreground/70">{builds.length}</span>} />
        {builds.length === 0 ? (
          <div className="px-4 py-10 text-center text-[13px] text-muted-foreground/70">No builds uploaded yet.</div>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Filename</TableHead>
                  <TableHead className="w-24">Platform</TableHead>
                  <TableHead className="w-24">Version</TableHead>
                  <TableHead className="w-24 text-right">Size</TableHead>
                  <TableHead className="w-40">Uploaded</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {builds.map((b) => (
                  <TableRow key={b.id}>
                    <TableCell className="font-mono text-[12.5px]">{b.filename}</TableCell>
                    <TableCell><Badge variant="secondary" className="text-[10.5px]">{platformLabel(b.platform)}</Badge></TableCell>
                    <TableCell>{b.version || <span className="text-muted-foreground/50">—</span>}</TableCell>
                    <TableCell className="text-right font-mono tabular-nums">{formatSize(b.sizeBytes)}</TableCell>
                    <TableCell className="text-[12px] text-muted-foreground">{new Date(b.createdAt).toLocaleString()}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </Card>
    </div>
  );
}

function formatSize(bytes: number) {
  if (bytes <= 0) return "—";
  const units = ["B", "KB", "MB", "GB"];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}
