import { useEffect } from "react";
import { Command } from "cmdk";
import { Activity, FlaskConical, ImageIcon, Moon, MonitorSmartphone, Plus, Sun } from "lucide-react";
import { StatusDot } from "@/components/StatusDot";
import { useTheme } from "@/theme";
import type { Instance } from "@/types";
import type { InventoryView } from "@/components/InventorySidebar";

export function CommandPalette({
  open,
  onOpenChange,
  instances,
  onSelectDevice,
  onView,
  onNewDevice,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  instances: Instance[];
  onSelectDevice: (id: string) => void;
  onView: (v: InventoryView) => void;
  onNewDevice: () => void;
}) {
  const { theme, toggle } = useTheme();

  // Global ⌘K / Ctrl-K to open.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        onOpenChange(!open);
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onOpenChange]);

  function run(fn: () => void) {
    fn();
    onOpenChange(false);
  }

  return (
    <Command.Dialog
      open={open}
      onOpenChange={onOpenChange}
      label="Command menu"
      className="fixed left-1/2 top-[18vh] z-50 w-[min(560px,92vw)] -translate-x-1/2 overflow-hidden rounded-xl border bg-popover text-popover-foreground shadow-2xl"
      overlayClassName="fixed inset-0 z-50 bg-black/40 backdrop-blur-[2px]"
    >
      <Command.Input
        placeholder="Search devices, switch views, run actions…"
        className="w-full border-b bg-transparent px-4 py-3.5 text-[14px] outline-none placeholder:text-muted-foreground/70"
      />
      <Command.List className="max-h-[52vh] overflow-auto p-1.5">
        <Command.Empty className="px-3 py-8 text-center text-[13px] text-muted-foreground">No matches.</Command.Empty>

        {instances.length > 0 && (
          <Command.Group heading="Devices" className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wide [&_[cmdk-group-heading]]:text-muted-foreground/70">
            {instances.map((inst) => (
              <Item key={inst.id} onSelect={() => run(() => onSelectDevice(inst.id))}>
                <StatusDot state={inst.state} />
                <span>{inst.name}</span>
                <span className="ml-auto font-mono text-[11px] text-muted-foreground/70">{inst.deviceId || inst.id}</span>
              </Item>
            ))}
          </Command.Group>
        )}

        <Command.Group heading="Go to" className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wide [&_[cmdk-group-heading]]:text-muted-foreground/70">
          <Item onSelect={() => run(() => onView("devices"))}><MonitorSmartphone className="size-4 text-muted-foreground" />Devices</Item>
          <Item onSelect={() => run(() => onView("tests"))}><FlaskConical className="size-4 text-muted-foreground" />Tests</Item>
          <Item onSelect={() => run(() => onView("images"))}><ImageIcon className="size-4 text-muted-foreground" />Images</Item>
          <Item onSelect={() => run(() => onView("activity"))}><Activity className="size-4 text-muted-foreground" />Activity</Item>
        </Command.Group>

        <Command.Group heading="Actions" className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wide [&_[cmdk-group-heading]]:text-muted-foreground/70">
          <Item onSelect={() => run(onNewDevice)}><Plus className="size-4 text-muted-foreground" />Deploy new device</Item>
          <Item onSelect={() => run(toggle)}>
            {theme === "dark" ? <Sun className="size-4 text-muted-foreground" /> : <Moon className="size-4 text-muted-foreground" />}
            Switch to {theme === "dark" ? "light" : "dark"} theme
          </Item>
        </Command.Group>
      </Command.List>
    </Command.Dialog>
  );
}

function Item({ children, onSelect }: { children: React.ReactNode; onSelect: () => void }) {
  return (
    <Command.Item
      onSelect={onSelect}
      className="flex cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-2 text-[13.5px] data-[selected=true]:bg-accent"
    >
      {children}
    </Command.Item>
  );
}
