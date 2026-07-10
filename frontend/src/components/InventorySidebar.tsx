import { useState } from "react";
import { Activity, ChevronDown, ChevronRight, FlaskConical, Folder, ImageIcon, MonitorSmartphone, Plus, Server, Smartphone } from "lucide-react";
import { cn } from "@/lib/utils";
import { StatusDot } from "@/components/StatusDot";
import type { Host, Instance } from "@/types";

export type InventoryView = "devices" | "tests" | "images" | "activity";

const VIEWS: { id: InventoryView; label: string; Icon: typeof MonitorSmartphone }[] = [
  { id: "devices", label: "Devices", Icon: MonitorSmartphone },
  { id: "tests", label: "Tests", Icon: FlaskConical },
  { id: "images", label: "Images", Icon: ImageIcon },
  { id: "activity", label: "Activity", Icon: Activity },
];

export function InventorySidebar({
  host,
  instances,
  selectedId,
  view,
  onView,
  onSelect,
  onNewDevice,
}: {
  host?: Host;
  instances: Instance[];
  selectedId?: string;
  view: InventoryView;
  onView: (v: InventoryView) => void;
  onSelect: (id: string) => void;
  onNewDevice: () => void;
}) {
  const [openHost, setOpenHost] = useState(true);
  const [openDevices, setOpenDevices] = useState(true);

  return (
    <aside className="flex flex-col border-r bg-sidebar" style={{ background: "var(--sidebar)" }}>
      <div className="flex gap-0.5 border-b p-2" style={{ borderColor: "var(--hairline)" }}>
        {VIEWS.map(({ id, label, Icon }) => (
          <button
            key={id}
            title={label}
            onClick={() => onView(id)}
            className={cn(
              "grid h-8.5 flex-1 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
              view === id && "bg-accent text-primary shadow-[inset_0_-2px_0_var(--primary)]",
            )}
            style={{ height: 34 }}
          >
            <Icon className="size-[17px]" />
          </button>
        ))}
      </div>

      <div className="flex items-center px-3.5 pb-1.5 pt-3">
        <span className="text-[11px] uppercase tracking-[0.06em] text-muted-foreground/80">Inventory</span>
        <button
          onClick={onNewDevice}
          title="Deploy new device"
          className="ml-auto grid size-5 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-primary"
        >
          <Plus className="size-3.5" />
        </button>
      </div>

      <nav className="flex-1 overflow-auto px-1.5 pb-3 text-[13px]">
        <TreeRow depth={0} chevron={openHost} onChevron={() => setOpenHost((v) => !v)} icon={<Server className="size-[15px]" />} label={host?.name || "local host"} count={instances.length} />
        {openHost && (
          <>
            <TreeRow depth={1} chevron={openDevices} onChevron={() => setOpenDevices((v) => !v)} icon={<Folder className="size-[15px]" />} label="Cuttlefish devices" count={instances.length} />
            {openDevices &&
              instances.map((inst) => (
                <button
                  key={inst.id}
                  onClick={() => onSelect(inst.id)}
                  className={cn(
                    "flex w-full items-center gap-2 rounded-md py-1.5 pr-2 text-left hover:bg-accent",
                    selectedId === inst.id && "bg-brand-weak shadow-[inset_2px_0_0_var(--primary)]",
                  )}
                  style={{ paddingLeft: 38 }}
                >
                  <StatusDot state={inst.state} />
                  <span className="truncate">{inst.name}</span>
                  {inst.androidVersion && (
                    <span className="ml-auto font-mono text-[10px] text-muted-foreground/70">{inst.androidVersion}</span>
                  )}
                </button>
              ))}
            <TreeRow depth={1} chevron={false} onChevron={() => onView("images")} icon={<Smartphone className="size-[15px]" />} label="Images" />
          </>
        )}
        {instances.length === 0 && (
          <div className="px-3 py-6 text-center text-[12px] text-muted-foreground/70">No devices yet.</div>
        )}
      </nav>
    </aside>
  );
}

function TreeRow({
  depth,
  chevron,
  onChevron,
  icon,
  label,
  count,
}: {
  depth: number;
  chevron: boolean | null;
  onChevron: () => void;
  icon: React.ReactNode;
  label: string;
  count?: number;
}) {
  return (
    <button
      onClick={onChevron}
      className="flex w-full items-center gap-1.5 rounded-md py-1.5 pr-2 text-left text-foreground hover:bg-accent"
      style={{ paddingLeft: 8 + depth * 16 }}
    >
      <span className="grid w-3 place-items-center text-[10px] text-muted-foreground">
        {chevron === null ? null : chevron ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
      </span>
      <span className="grid size-[15px] place-items-center text-muted-foreground">{icon}</span>
      <span className="truncate">{label}</span>
      {count !== undefined && <span className="ml-auto font-mono text-[10px] text-muted-foreground/70">{count}</span>}
    </button>
  );
}
