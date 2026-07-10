import { useEffect, useMemo, useState } from "react";
import { api } from "@/api";
import type { Host, Instance, Principal } from "@/types";
import { AuthGate } from "@/components/AuthGate";
import { TopBar } from "@/components/TopBar";
import { InventorySidebar, type InventoryView } from "@/components/InventorySidebar";
import { CommandPalette } from "@/components/CommandPalette";
import { DeviceWorkspace } from "@/components/device/DeviceWorkspace";

export default function App() {
  const [authChecked, setAuthChecked] = useState(false);
  const [bootstrapRequired, setBootstrapRequired] = useState(false);
  const [principal, setPrincipal] = useState<Principal>();
  const [host, setHost] = useState<Host>();
  const [instances, setInstances] = useState<Instance[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [view, setView] = useState<InventoryView>("devices");
  const [showSidebar, setShowSidebar] = useState(true);
  const [cmdOpen, setCmdOpen] = useState(false);
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
    if (!principal) {
      return;
    }
    const tick = () => {
      if (!document.hidden) {
        refresh().catch(() => undefined);
      }
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

  if (!authChecked) {
    return <div className="grid min-h-screen place-items-center text-muted-foreground">Loading OpenCuttles…</div>;
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
      <TopBar principal={principal} onOpenCommand={() => setCmdOpen(true)} onToggleSidebar={() => setShowSidebar((v) => !v)} />
      <div className="grid min-h-0" style={{ gridTemplateColumns: showSidebar ? "290px 1fr" : "0 1fr" }}>
        {showSidebar && (
          <InventorySidebar
            host={host}
            instances={instances}
            selectedId={selected?.id}
            view={view}
            onView={setView}
            onSelect={selectDevice}
          />
        )}
        <main className="flex min-w-0 flex-col overflow-hidden">
          {error && (
            <div className="border-b px-5 py-2 text-[13px]" style={{ color: "var(--destructive)" }}>
              {error}
            </div>
          )}
          {view === "devices" ? (
            selected ? (
              <DeviceWorkspace
                instance={selected}
                busy={busy}
                onStart={(id) => act(() => api.startInstance(id))}
                onStop={(id) => act(() => api.stopInstance(id))}
              />
            ) : (
              <CenterEmpty>No devices yet. Create one from the Devices view.</CenterEmpty>
            )
          ) : (
            <CenterEmpty>
              The <span className="font-medium capitalize text-foreground">{view}</span> view is being rebuilt in the next phase.
            </CenterEmpty>
          )}
        </main>
      </div>

      <CommandPalette open={cmdOpen} onOpenChange={setCmdOpen} instances={instances} onSelectDevice={selectDevice} onView={setView} />
    </div>
  );
}

function CenterEmpty({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid flex-1 place-items-center p-8 text-center text-[14px] text-muted-foreground">
      <p className="max-w-md">{children}</p>
    </div>
  );
}
