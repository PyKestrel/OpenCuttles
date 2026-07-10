import type { InstanceState } from "@/types";

/** CSS color for a device lifecycle state — semantic, not the brand accent. */
export function stateColor(state: InstanceState): string {
  switch (state) {
    case "running":
      return "var(--running)";
    case "error":
      return "var(--destructive)";
    case "stopped":
      return "var(--stopped)";
    default:
      return "var(--warn)"; // provisioning / starting / booting / stopping / deleting
  }
}

const BUSY: InstanceState[] = ["provisioning", "starting", "booting", "stopping", "deleting"];

export function isBusy(state: InstanceState): boolean {
  return BUSY.includes(state);
}
