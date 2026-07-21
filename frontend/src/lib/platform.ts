import { Laptop, Monitor, Smartphone, Terminal, type LucideIcon } from "lucide-react";
import type { DeviceSource, Instance, Platform } from "@/types";

// Every platform a device can run, in presentation order. Single source of
// truth: this list was duplicated in the sidebar and two views, so adding one
// meant finding all three.
export const PLATFORMS: Platform[] = ["android", "windows", "linux", "macos"];

// deviceSource answers "how do we reach it", which is a different question from
// "what OS does it run". Absent means cuttlefish, matching how the backend
// treats rows written before the field existed.
export function deviceSource(instance: Instance): DeviceSource {
  return instance.source || "cuttlefish";
}

// isProvisioned reports whether the appliance creates and destroys the device
// itself. Provisioned devices have a start/stop lifecycle; the rest are simply
// reachable or not, and deleting one only deregisters it.
export function isProvisioned(instance: Instance): boolean {
  return deviceSource(instance) === "cuttlefish";
}

// usesRunnerTunnel reports whether control goes over the dial-home runner —
// true only for desktops. A physical Android handset is driven over ADB, like a
// Cuttlefish VM.
export function usesRunnerTunnel(instance: Instance): boolean {
  return deviceSource(instance) === "runner";
}

// isDesktopPlatform is about the OS, not the transport. Kept for the creation
// form and for labelling, where only the requested platform is known.
export function isDesktopPlatform(platform: Platform | undefined): boolean {
  return (platform || "android") !== "android";
}

export function platformLabel(platform: Platform): string {
  return platform === "macos" ? "macOS" : platform.charAt(0).toUpperCase() + platform.slice(1);
}

export function platformIcon(platform: Platform): LucideIcon {
  switch (platform) {
    case "windows":
      return Monitor;
    case "linux":
      return Terminal;
    case "macos":
      return Laptop;
    default:
      return Smartphone;
  }
}

// "Live" = screenshottable and controllable.
//
// Which state a device reaches is an implementation detail of its source: a
// provisioned VM reaches "running", while anything the appliance merely
// connects to — a desktop runner, a physical handset — reaches "online". Both
// mean the same thing to a caller.
//
// This previously keyed on platform, so a physical Android device would have
// been required to reach "running", which it never does: it would have been
// permanently treated as not live.
export function isLive(instance: Instance): boolean {
  return instance.state === "online" || instance.state === "running";
}
