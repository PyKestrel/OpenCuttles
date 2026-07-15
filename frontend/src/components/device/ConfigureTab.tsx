import { useState } from "react";
import { Settings2, Trash2 } from "lucide-react";
import { Card, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { isDesktopPlatform, platformLabel } from "@/lib/platform";
import type { Instance } from "@/types";

// Read-only device configuration plus a guarded delete.
export function ConfigureTab({
  instance,
  busy,
  canOperate,
  onDelete,
}: {
  instance: Instance;
  busy: boolean;
  canOperate: boolean;
  onDelete: (id: string) => void;
}) {
  const [confirming, setConfirming] = useState(false);
  const platform = instance.platform || "android";
  const isDesktop = isDesktopPlatform(instance.platform);

  return (
    <div className="grid items-start gap-4 lg:grid-cols-2">
      <Card>
        <CardHeader icon={<Settings2 className="size-[15px]" />} title="Configuration" />
        <div className="px-4 pb-3 pt-1">
          <Row k="Name">{instance.name}</Row>
          <Row k="Instance ID" mono>{instance.id}</Row>
          <Row k="Host" mono>{instance.hostId}</Row>
          {isDesktop ? (
            <>
              <Row k="Platform">{platformLabel(platform)}</Row>
              <Row k="Control endpoint" mono>{instance.controlEndpoint || "dial-home tunnel"}</Row>
              {instance.displayWidth ? (
                <Row k="Display">
                  {instance.displayWidth} × {instance.displayHeight}
                  {instance.dpi ? ` · ${instance.dpi} dpi` : ""}
                </Row>
              ) : null}
            </>
          ) : (
            <>
              <Row k="Image" mono>{instance.imageId || "—"}</Row>
              <Row k="Android">{instance.androidVersion || "—"}</Row>
              <Row k="Resources">{instance.cpuCores} vCPU · {instance.memoryMb} MB</Row>
              <Row k="Display">
                {instance.displayWidth && instance.displayHeight ? `${instance.displayWidth} × ${instance.displayHeight} · ${instance.dpi} dpi` : "—"}
              </Row>
            </>
          )}
          <Row k="Console">{instance.consoleProvider}</Row>
          <Row k="Created">{new Date(instance.createdAt).toLocaleString()}</Row>
        </div>
      </Card>

      <Card className="border-[color-mix(in_srgb,var(--destructive)_30%,transparent)]">
        <CardHeader icon={<Trash2 className="size-[15px]" />} title="Danger zone" />
        <div className="space-y-3 p-4">
          <p className="text-[13px] text-muted-foreground">
            {isDesktop
              ? "Deleting this device deregisters its runner and removes its enrollment. The machine itself is left untouched. This cannot be undone."
              : "Deleting this device stops it and removes its Cuttlefish instance and disk state. This cannot be undone."}
          </p>
          {!confirming ? (
            <Button variant="danger" disabled={!canOperate || busy} onClick={() => setConfirming(true)}>
              <Trash2 className="size-3.5" /> Delete device
            </Button>
          ) : (
            <div className="flex items-center gap-2">
              <span className="text-[13px] font-medium">Delete {instance.name}?</span>
              <Button variant="danger" disabled={busy} onClick={() => onDelete(instance.id)}>Confirm delete</Button>
              <Button disabled={busy} onClick={() => setConfirming(false)}>Cancel</Button>
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}

function Row({ k, mono, children }: { k: string; mono?: boolean; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[130px_1fr] gap-2.5 border-t py-2 text-[13px] first:border-t-0" style={{ borderColor: "var(--hairline)" }}>
      <span className="text-muted-foreground">{k}</span>
      <span className={mono ? "break-all font-mono text-[12.5px]" : "text-[13px]"}>{children}</span>
    </div>
  );
}
