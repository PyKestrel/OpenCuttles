import { useState } from "react";
import { KeyRound, Settings2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Card, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { CopyField } from "@/components/ui/copy-field";
import { api } from "@/api";
import { isDesktopPlatform, platformLabel } from "@/lib/platform";
import { oneLineInstall } from "@/lib/runner-install";
import type { Instance } from "@/types";

// Read-only device configuration, enrollment-credential management, and a
// guarded delete.
export function ConfigureTab({
  instance,
  busy,
  canOperate,
  canAdmin,
  onDelete,
}: {
  instance: Instance;
  busy: boolean;
  canOperate: boolean;
  canAdmin: boolean;
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

      {isDesktop && <EnrollmentCard instance={instance} canAdmin={canAdmin} />}

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

// EnrollmentCard manages the credential a desktop runner uses to dial home.
//
// A token grants screenshot and input on someone's real machine, so it needs a
// way out: rotate when a machine is rebuilt or a token may have leaked, revoke
// outright when a laptop is lost. Both drop any live tunnel immediately —
// revocation that only applied to the next connection would leave an existing
// session running.
function EnrollmentCard({ instance, canAdmin }: { instance: Instance; canAdmin: boolean }) {
  const [busy, setBusy] = useState(false);
  const [issued, setIssued] = useState("");
  const [confirmingRevoke, setConfirmingRevoke] = useState(false);

  async function rotate() {
    setBusy(true);
    try {
      const res = await api.rotateRunnerToken(instance.id);
      setIssued(res.enrollmentToken);
      toast.success(
        res.sessionDropped
          ? "New token issued — the connected runner was disconnected."
          : "New token issued.",
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not rotate the token");
    } finally {
      setBusy(false);
    }
  }

  async function revoke() {
    setBusy(true);
    try {
      const res = await api.revokeRunnerToken(instance.id);
      setIssued("");
      setConfirmingRevoke(false);
      toast.success(
        res.sessionDropped
          ? "Token revoked — the connected runner was disconnected."
          : "Token revoked.",
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not revoke the token");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardHeader icon={<KeyRound className="size-[15px]" />} title="Enrollment credential" />
      <div className="space-y-3 p-4">
        <p className="text-[13px] leading-relaxed text-muted-foreground">
          This device's runner authenticates with an enrollment token. Rotate it if the machine is
          rebuilt or the token may have leaked; revoke it if the machine is lost. Either way, a
          connected runner is disconnected immediately.
        </p>

        {issued ? (
          <div className="space-y-2">
            <CopyField label="New enrollment token — shown once" value={issued} mono />
            <CopyField
              label={`One-line install for ${instance.name}`}
              value={oneLineInstall(platformOf(instance), window.location.origin, issued)}
              mono
            />
            <p className="text-[11px] text-muted-foreground/80">
              The previous token no longer works. Run this on {instance.name} to reconnect it.
            </p>
          </div>
        ) : null}

        {!canAdmin ? (
          <div className="text-[11.5px] text-muted-foreground/70">
            Your role cannot manage enrollment credentials.
          </div>
        ) : !confirmingRevoke ? (
          <div className="flex items-center gap-2">
            <Button disabled={busy} onClick={rotate}>
              <KeyRound className="size-3.5" /> Rotate token
            </Button>
            <Button variant="danger" disabled={busy} onClick={() => setConfirmingRevoke(true)}>
              Revoke
            </Button>
          </div>
        ) : (
          <div className="flex items-center gap-2">
            <span className="text-[13px] font-medium">Revoke {instance.name}'s token?</span>
            <Button variant="danger" disabled={busy} onClick={revoke}>
              Confirm revoke
            </Button>
            <Button disabled={busy} onClick={() => setConfirmingRevoke(false)}>
              Cancel
            </Button>
          </div>
        )}
      </div>
    </Card>
  );
}

// The install one-liner is platform-specific; desktop devices always carry one.
function platformOf(instance: Instance): string {
  return instance.platform || "windows";
}

function Row({ k, mono, children }: { k: string; mono?: boolean; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[130px_1fr] gap-2.5 border-t py-2 text-[13px] first:border-t-0" style={{ borderColor: "var(--hairline)" }}>
      <span className="text-muted-foreground">{k}</span>
      <span className={mono ? "break-all font-mono text-[12.5px]" : "text-[13px]"}>{children}</span>
    </div>
  );
}
