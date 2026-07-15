import { useState } from "react";
import { Activity, ChevronDown, ChevronRight, FlaskConical, ImageIcon, Laptop, Monitor, MonitorSmartphone, Plus, Server, Smartphone, Terminal } from "lucide-react";
import { cn } from "@/lib/utils";
import { StatusDot } from "@/components/StatusDot";
import type { Host, Instance, Platform } from "@/types";

export type InventoryView = "devices" | "tests" | "images" | "activity";

const VIEWS: { id: InventoryView; label: string; Icon: typeof MonitorSmartphone }[] = [
  { id: "devices", label: "Devices", Icon: MonitorSmartphone },
  { id: "tests", label: "Tests", Icon: FlaskConical },
  { id: "images", label: "Images", Icon: ImageIcon },
  { id: "activity", label: "Activity", Icon: Activity },
];

// Platform groups, in display order. Android first (the original product).
const PLATFORMS: { id: Platform; label: string; Icon: typeof Monitor }[] = [
  { id: "android", label: "Android", Icon: Smartphone },
  { id: "windows", label: "Windows", Icon: Monitor },
  { id: "linux", label: "Linux", Icon: Terminal },
  { id: "macos", label: "macOS", Icon: Laptop },
];

export function InventorySidebar({
  host,
  instances,
  selectedId,
  view,
  onView,
  onSelect,
  onNewDevice,
  compact = false,
}: {
  host?: Host;
  instances: Instance[];
  selectedId?: string;
  view: InventoryView;
  onView: (v: InventoryView) => void;
  onSelect: (id: string) => void;
  onNewDevice: () => void;
  compact?: boolean;
}) {
  const [openHost, setOpenHost] = useState(true);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const groups = PLATFORMS.map((p) => ({
    ...p,
    devices: instances.filter((i) => (i.platform || "android") === p.id),
  })).filter((g) => g.devices.length > 0);

  // Collapsed rail: keep navigation reachable (view icons + add) instead of
  // hiding the sidebar entirely, which would strand the user with no nav.
  if (compact) {
    return (
      <aside className="flex flex-col items-center gap-1 overflow-hidden border-r bg-sidebar py-2" style={{ background: "var(--sidebar)" }}>
        {VIEWS.map(({ id, label, Icon }) => (
          <button
            key={id}
            title={label}
            aria-label={label}
            aria-pressed={view === id}
            onClick={() => onView(id)}
            className={cn(
              "grid size-9 place-items-center rounded-md border border-transparent text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
              view === id && "text-foreground",
            )}
            style={{ borderColor: view === id ? "color-mix(in srgb, var(--foreground) 35%, transparent)" : undefined }}
          >
            <Icon className="size-[18px]" />
          </button>
        ))}
        <div className="my-1 h-px w-6" style={{ background: "var(--hairline)" }} />
        <button
          onClick={onNewDevice}
          title="Add a device"
          aria-label="Add a device"
          className="grid size-9 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-primary"
        >
          <Plus className="size-[18px]" />
        </button>
      </aside>
    );
  }

  return (
    <aside className="flex flex-col border-r bg-sidebar" style={{ background: "var(--sidebar)" }}>
      {/* View switcher: the active view is marked with a simple light border. */}
      <div className="flex gap-1 border-b p-2" style={{ borderColor: "var(--hairline)" }}>
        {VIEWS.map(({ id, label, Icon }) => (
          <button
            key={id}
            title={label}
            aria-label={label}
            aria-pressed={view === id}
            onClick={() => onView(id)}
            className={cn(
              "grid h-8 flex-1 place-items-center rounded-md border border-transparent text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
              view === id && "text-foreground",
            )}
            style={{ borderColor: view === id ? "color-mix(in srgb, var(--foreground) 35%, transparent)" : undefined }}
          >
            <Icon className="size-[17px]" />
          </button>
        ))}
      </div>

      <div className="flex items-center px-3.5 pb-1.5 pt-3">
        <span className="text-[11px] uppercase tracking-[0.06em] text-muted-foreground/80">Inventory</span>
        <button
          onClick={onNewDevice}
          title="Add a device"
          className="ml-auto grid size-5 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-primary"
        >
          <Plus className="size-3.5" />
        </button>
      </div>

      <nav className="flex-1 overflow-auto px-1.5 pb-3 text-[13px]">
        <TreeRow depth={0} chevron={openHost} onChevron={() => setOpenHost((v) => !v)} icon={<Server className="size-[15px]" />} label={host?.name || "local host"} count={instances.length} />
        {openHost && (
          <>
            {groups.map((g) => {
              const open = collapsed[g.id] !== true;
              return (
                <div key={g.id}>
                  <TreeRow
                    depth={1}
                    chevron={open}
                    onChevron={() => setCollapsed((c) => ({ ...c, [g.id]: open }))}
                    icon={<g.Icon className="size-[15px]" />}
                    label={g.label}
                    count={g.devices.length}
                  />
                  {open &&
                    g.devices.map((inst) => (
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
                </div>
              );
            })}
            <TreeRow depth={1} chevron={false} onChevron={() => onView("images")} icon={<ImageIcon className="size-[15px]" />} label="Images" />
          </>
        )}
        {instances.length === 0 && (
          <div className="px-3 py-6 text-center text-[12px] text-muted-foreground/70">
            No devices yet.
            <button onClick={onNewDevice} className="mt-1 block w-full text-[12px] text-primary">Add one →</button>
          </div>
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
