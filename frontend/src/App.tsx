import { useEffect, useMemo, useState } from "react";
import { api } from "@/api";
import type { Host, Instance, Principal } from "@/types";
import { AuthGate } from "@/components/AuthGate";
import { TopBar } from "@/components/TopBar";
import { InventorySidebar, type InventoryView } from "@/components/InventorySidebar";
import { CommandPalette } from "@/components/CommandPalette";
import { CreateDeviceDialog } from "@/components/CreateDeviceDialog";
import { DitherField } from "@/components/DitherField";
import { BrandMark } from "@/components/Brand";
import { DeviceWorkspace } from "@/components/device/DeviceWorkspace";
import { ImagesView } from "@/components/views/ImagesView";
import { ActivityView } from "@/components/views/ActivityView";
import { TestsPanel } from "@/components/tests/TestsPanel";
import { CasesView } from "@/components/views/CasesView";
import { CyclesView } from "@/components/views/CyclesView";
import { CycleRunsView } from "@/components/views/CycleRunsView";
import { BuildsView } from "@/components/views/BuildsView";
import { Toaster } from "@/components/ui/sonner";

export default function App() {
  const [authChecked, setAuthChecked] = useState(false);
  const [bootstrapRequired, setBootstrapRequired] = useState(false);
  const [principal, setPrincipal] = useState<Principal>();
  const [host, setHost] = useState<Host>();
  const [instances, setInstances] = useState<Instance[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [view, setView] = useState<InventoryView>("devices");
  const [sidebarCompact, setSidebarCompact] = useState(false);
  const [cmdOpen, setCmdOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const selected = useMemo(
    () => instances.find((i) => i.id === selectedId) ?? instances[0],
    [instances, selectedId],
  );

  async function refresh() {
    const [h, list] = await Promise.all([api.host().catch(() => undefined), api.instances().catch(() => [])]);
    if (h) setHost(h);
    setInstances(list ?? []);
  }

  useEffect(() => {
    (async () => {
      try {
        const b = await api.bootstrapStatus();
        setBootstrapRequired(b.required);
        if (!b.required) {
          setPrincipal(await api.me());
          await refresh();
        }
      } catch (e) {
        setError(e instanceof Error ? e.message : "Unable to initialize");
      } finally {
        setAuthChecked(true);
      }
    })();
  }, []);

  // Resource-conscious polling: skip while the tab is hidden.
  useEffect(() => {
    if (!principal) return;
    const tick = () => {
      if (!document.hidden) refresh().catch(() => undefined);
    };
    const id = window.setInterval(tick, 5000);
    const onVis = () => tick();
    document.addEventListener("visibilitychange", onVis);
    return () => {
      window.clearInterval(id);
      document.removeEventListener("visibilitychange", onVis);
    };
  }, [principal]);

  async function act(fn: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await fn();
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Action failed");
    } finally {
      setBusy(false);
    }
  }

  function selectDevice(id: string) {
    setSelectedId(id);
    setView("devices");
  }

  async function deleteDevice(id: string) {
    await act(() => api.deleteInstance(id));
    if (selectedId === id) setSelectedId("");
  }

  if (!authChecked) {
    return <div className="grid min-h-screen place-items-center text-muted-foreground">Loading Testral…</div>;
  }
  if (!principal || bootstrapRequired) {
    return (
      <AuthGate
        bootstrapRequired={bootstrapRequired}
        onAuthenticated={(p) => {
          setPrincipal(p);
          setBootstrapRequired(false);
          refresh();
        }}
      />
    );
  }

  return (
    <div className="grid h-screen grid-rows-[auto_1fr]">
      <TopBar principal={principal} onOpenCommand={() => setCmdOpen(true)} onToggleSidebar={() => setSidebarCompact((v) => !v)} />
      <div className="grid min-h-0" style={{ gridTemplateColumns: sidebarCompact ? "56px 1fr" : "290px 1fr" }}>
        <InventorySidebar
          compact={sidebarCompact}
          host={host}
          instances={instances}
          selectedId={selected?.id}
          view={view}
          onView={setView}
          onSelect={selectDevice}
          onNewDevice={() => setCreateOpen(true)}
        />
        <main className="flex min-w-0 flex-col overflow-hidden">
          {error && (
            <div className="border-b px-5 py-2 text-[13px]" style={{ color: "var(--destructive)" }}>
              {error}
            </div>
          )}
          <MainContent
            view={view}
            selected={selected}
            instances={instances}
            principal={principal}
            host={host}
            busy={busy}
            onStart={(id) => act(() => api.startInstance(id))}
            onStop={(id) => act(() => api.stopInstance(id))}
            onDelete={deleteDevice}
            onNewDevice={() => setCreateOpen(true)}
          />
        </main>
      </div>

      <CommandPalette
        open={cmdOpen}
        onOpenChange={setCmdOpen}
        instances={instances}
        onSelectDevice={selectDevice}
        onView={setView}
        onNewDevice={() => setCreateOpen(true)}
      />
      <CreateDeviceDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={(instance) => {
          refresh();
          selectDevice(instance.id);
        }}
      />
      <Toaster position="bottom-right" />
    </div>
  );
}

function MainContent({
  view,
  selected,
  instances,
  principal,
  host,
  busy,
  onStart,
  onStop,
  onDelete,
  onNewDevice,
}: {
  view: InventoryView;
  selected?: Instance;
  instances: Instance[];
  principal: Principal;
  host?: Host;
  busy: boolean;
  onStart: (id: string) => void;
  onStop: (id: string) => void;
  onDelete: (id: string) => void;
  onNewDevice: () => void;
}) {
  if (view === "images") return <ImagesView principal={principal} />;
  if (view === "activity") return <ActivityView principal={principal} host={host} />;
  if (view === "cases") return <CasesView principal={principal} />;
  if (view === "cycles") return <CyclesView principal={principal} />;
  if (view === "runs") return <CycleRunsView />;
  if (view === "builds") return <BuildsView principal={principal} />;
  if (view === "tests")
    return (
      <div className="mx-auto w-full max-w-6xl p-5">
        <TestsPanel instance={selected} instances={instances} />
      </div>
    );

  // devices
  if (!selected) {
    return (
      <div className="relative grid flex-1 place-items-center overflow-hidden p-8 text-center">
        <div className="pointer-events-none absolute inset-0 opacity-70">
          <DitherField />
        </div>
        <div className="relative max-w-sm rounded-2xl border bg-card/90 px-8 py-9 backdrop-blur-sm" style={{ boxShadow: "var(--card-shadow)" }}>
          <BrandMark className="mx-auto size-14" />
          <h2 className="mt-4 text-[17px] font-semibold tracking-tight">No devices yet</h2>
          <p className="mt-1 text-[13.5px] leading-relaxed text-muted-foreground">
            Deploy a Cuttlefish Android device and Testral fetches the image automatically — no manual setup.
          </p>
          <button onClick={onNewDevice} className="mt-5 rounded-lg px-4 py-2.5 text-[13px] font-medium text-primary-foreground" style={{ background: "var(--primary-strong)" }}>
            Deploy your first device
          </button>
        </div>
      </div>
    );
  }
  return (
    <DeviceWorkspace
      instance={selected}
      instances={instances}
      principal={principal}
      busy={busy}
      onStart={onStart}
      onStop={onStop}
      onDelete={onDelete}
    />
  );
}
