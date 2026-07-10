import { cn } from "@/lib/utils";
import { isBusy, stateColor } from "@/lib/status";
import type { InstanceState } from "@/types";

export function StatusDot({ state, className }: { state: InstanceState; className?: string }) {
  const color = stateColor(state);
  return (
    <span
      className={cn("inline-block size-2 shrink-0 rounded-full", isBusy(state) && "animate-pulse", className)}
      style={{ background: color, boxShadow: `0 0 6px color-mix(in srgb, ${color} 55%, transparent)` }}
      aria-hidden
    />
  );
}
