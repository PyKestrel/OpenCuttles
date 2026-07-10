import { HelpCircle, Menu, Moon, Search, Sun } from "lucide-react";
import { BrandMark } from "@/components/Brand";
import { useTheme } from "@/theme";
import type { Principal } from "@/types";

export function TopBar({
  principal,
  onOpenCommand,
  onToggleSidebar,
}: {
  principal: Principal;
  onOpenCommand: () => void;
  onToggleSidebar: () => void;
}) {
  const { theme, toggle } = useTheme();
  const initials = (principal.displayName || principal.username)
    .split(" ")
    .map((s) => s[0])
    .slice(0, 2)
    .join("")
    .toUpperCase();

  return (
    <header className="flex h-13 items-center gap-3.5 border-b bg-surface px-4" style={{ height: 52 }}>
      <button
        onClick={onToggleSidebar}
        className="grid size-7.5 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
        style={{ width: 30, height: 30 }}
        aria-label="Toggle inventory"
      >
        <Menu className="size-[18px]" />
      </button>

      <div className="flex items-center gap-2.5 font-semibold">
        <BrandMark className="size-6" />
        <span className="text-[15px]">OpenCuttles</span>
      </div>

      <button
        onClick={onOpenCommand}
        className="ml-1 flex h-8 max-w-[520px] flex-1 items-center gap-2.5 rounded-lg border bg-secondary px-3 text-[13px] text-muted-foreground/80 transition-colors hover:border-border-strong"
      >
        <Search className="size-3.5" />
        <span className="truncate">Search devices, tests, actions…</span>
        <kbd className="ml-auto rounded border px-1.5 font-mono text-[11px] text-muted-foreground">⌘K</kbd>
      </button>

      <div className="flex-1" />

      <button
        onClick={toggle}
        className="grid size-7.5 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
        style={{ width: 30, height: 30 }}
        aria-label="Toggle theme"
      >
        {theme === "dark" ? <Sun className="size-[17px]" /> : <Moon className="size-[17px]" />}
      </button>
      <button
        className="grid size-7.5 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
        style={{ width: 30, height: 30 }}
        aria-label="Help"
      >
        <HelpCircle className="size-[17px]" />
      </button>
      <span
        className="grid size-7 place-items-center rounded-md border bg-secondary text-[11px] font-semibold text-muted-foreground"
        title={`${principal.displayName} · ${principal.role}`}
      >
        {initials}
      </span>
    </header>
  );
}
