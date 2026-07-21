import { describe, expect, it } from "vitest";
import { deviceSource, isLive, isProvisioned, PLATFORMS, usesRunnerTunnel } from "@/lib/platform";
import type { Instance } from "@/types";

// Minimal instance; only the fields these helpers read matter.
function device(partial: Partial<Instance>): Instance {
  return {
    id: "x",
    name: "x",
    hostId: "local",
    platform: "android",
    imageId: "",
    state: "offline",
    cpuCores: 0,
    memoryMb: 0,
    displayWidth: 0,
    displayHeight: 0,
    dpi: 0,
    adbPort: 0,
    webrtcPort: 0,
    deviceId: "",
    consoleProvider: "screenshot",
    consoleUrl: "",
    createdAt: "",
    updatedAt: "",
    ...partial,
  };
}

describe("deviceSource", () => {
  it("treats a missing source as cuttlefish, matching the backend", () => {
    expect(deviceSource(device({}))).toBe("cuttlefish");
  });

  it("passes an explicit source through", () => {
    expect(deviceSource(device({ source: "physical" }))).toBe("physical");
    expect(deviceSource(device({ source: "runner" }))).toBe("runner");
  });
});

describe("isProvisioned", () => {
  // Only a VM the appliance launches has a start/stop lifecycle. A handset and
  // a desktop are simply reachable or not, and deleting one deregisters it
  // rather than tearing anything down.
  it("is true only for cuttlefish", () => {
    expect(isProvisioned(device({}))).toBe(true);
    expect(isProvisioned(device({ source: "cuttlefish" }))).toBe(true);
    expect(isProvisioned(device({ source: "physical" }))).toBe(false);
    expect(isProvisioned(device({ source: "runner", platform: "windows" }))).toBe(false);
  });
});

describe("usesRunnerTunnel", () => {
  // A physical Android device is driven over ADB, like a Cuttlefish VM — not
  // over the runner tunnel, despite not being provisioned.
  it("is true only for desktops", () => {
    expect(usesRunnerTunnel(device({ source: "runner", platform: "windows" }))).toBe(true);
    expect(usesRunnerTunnel(device({ source: "physical" }))).toBe(false);
    expect(usesRunnerTunnel(device({}))).toBe(false);
  });
});

describe("isLive", () => {
  // Which state a device reaches depends on its source: a provisioned VM
  // reaches "running", anything the appliance connects to reaches "online".
  // The previous version keyed on platform, so a physical Android device would
  // have been required to reach "running" — which it never does, leaving it
  // permanently not-live.
  it("accepts both running and online for every source", () => {
    expect(isLive(device({ state: "running" }))).toBe(true);
    expect(isLive(device({ source: "physical", state: "online" }))).toBe(true);
    expect(isLive(device({ source: "runner", platform: "windows", state: "online" }))).toBe(true);
  });

  it("is false for anything else", () => {
    for (const state of ["offline", "stopped", "error", "provisioning", "booting"] as const) {
      expect(isLive(device({ state })), state).toBe(false);
    }
  });

  it("does not require a physical Android device to reach running", () => {
    // The regression this guards: an Android-platform device that is online.
    expect(isLive(device({ source: "physical", platform: "android", state: "online" }))).toBe(true);
  });
});

describe("PLATFORMS", () => {
  // Single source of truth — this list was duplicated in the sidebar and two
  // views, so adding a platform meant finding all three.
  it("covers every platform, android first", () => {
    expect(PLATFORMS).toEqual(["android", "windows", "linux", "macos"]);
  });
});
