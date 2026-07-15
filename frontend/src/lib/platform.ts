import { Laptop, Monitor, Smartphone, Terminal, type LucideIcon } from "lucide-react";
import type { Instance, Platform } from "@/types";

// A desktop runner (Windows/Linux/macOS) vs. an Android Cuttlefish VM. Instances
// default to "android" when the platform is unset (pre-multi-OS rows).
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

// "Live" = screenshottable/controllable: the runner is online (desktop) or the
// VM is running (Android).
export function isLive(instance: Instance): boolean {
  return isDesktopPlatform(instance.platform) ? instance.state === "online" : instance.state === "running";
}
