import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import type { AndroidVersion, Instance } from "./types";

describe("Deploy flow", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders the version dropdown and posts androidVersion on deploy", async () => {
    const posted: { url: string; body: unknown }[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (method === "POST" && url.endsWith("/instances")) {
        posted.push({ url, body: JSON.parse(String(init?.body)) });
        return Promise.resolve(response(runningInstance()));
      }
      if (url.endsWith("/bootstrap")) return Promise.resolve(response({ required: false }));
      if (url.endsWith("/auth/me")) return Promise.resolve(response(principal()));
      if (url.endsWith("/host")) {
        return Promise.resolve(
          response({ id: "local", name: "host-01", cpuCount: 8, memoryBytes: 0, diskFreeBytes: 0, prerequisites: [], updatedAt: new Date().toISOString() }),
        );
      }
      if (url.endsWith("/android-versions")) return Promise.resolve(response(versions()));
      if (url.endsWith("/instances")) return Promise.resolve(response([]));
      if (url.endsWith("/images") || url.endsWith("/operations") || url.endsWith("/audit")) {
        return Promise.resolve(response([]));
      }
      if (url.endsWith("/health")) {
        return Promise.resolve(response({ status: "ok", checks: [], generatedAt: new Date().toISOString() }));
      }
      return Promise.resolve(response({}));
    });

    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: "Instances" }));
    await screen.findByText("Deploy Android instance");
    await waitFor(() => expect(screen.getByRole("option", { name: "Android 14 (GSI)" })).toBeInTheDocument());

    await userEvent.type(screen.getByPlaceholderText("android-01"), "android-test");
    await userEvent.click(screen.getByRole("button", { name: "Deploy instance" }));

    await waitFor(() => expect(posted.length).toBeGreaterThan(0));
    expect(posted[0].body).toMatchObject({ name: "android-test", androidVersion: "android14" });
  });

  it("embeds the interactive console iframe for a running instance", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const url = String(input);
      if (url.endsWith("/bootstrap")) return Promise.resolve(response({ required: false }));
      if (url.endsWith("/auth/me")) return Promise.resolve(response(principal()));
      if (url.endsWith("/host")) {
        return Promise.resolve(
          response({ id: "local", name: "host-01", cpuCount: 8, memoryBytes: 0, diskFreeBytes: 0, prerequisites: [], updatedAt: new Date().toISOString() }),
        );
      }
      if (url.endsWith("/android-versions")) return Promise.resolve(response(versions()));
      if (url.endsWith("/instances")) return Promise.resolve(response([runningInstance()]));
      if (url.endsWith("/images") || url.endsWith("/operations") || url.endsWith("/audit")) {
        return Promise.resolve(response([]));
      }
      if (url.endsWith("/health")) {
        return Promise.resolve(response({ status: "ok", checks: [], generatedAt: new Date().toISOString() }));
      }
      return Promise.resolve(response({}));
    });

    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: "Instances" }));
    const frame = await screen.findByTitle("android-01 console");
    expect(frame).toHaveAttribute("src", "/api/v1/instances/cvd_1/console/devices/cvd_1-1-1/files/client.html");
  });
});

function versions(): AndroidVersion[] {
  return [
    { id: "android14", label: "Android 14 (GSI)", branch: "aosp-android14-gsi", buildTarget: "aosp_cf_x86_64_phone-userdebug" },
    { id: "android13", label: "Android 13 (GSI)", branch: "aosp-android13-gsi", buildTarget: "aosp_cf_x86_64_phone-userdebug" },
  ];
}

function runningInstance(): Instance {
  return {
    id: "cvd_1",
    name: "android-01",
    hostId: "local",
    imageId: "img_1",
    androidVersion: "android14",
    state: "running",
    cpuCores: 2,
    memoryMb: 4096,
    displayWidth: 720,
    displayHeight: 1280,
    dpi: 320,
    adbPort: 6520,
    webrtcPort: 8443,
    deviceId: "cvd_1-1-1",
    consoleProvider: "cuttlefish-webrtc",
    consoleUrl: "/api/v1/instances/cvd_1/console/devices/cvd_1-1-1/files/client.html",
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  };
}

function response(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function principal() {
  return {
    userId: "usr_1",
    username: "admin",
    displayName: "Admin",
    role: "admin",
    permissions: ["admin", "operate", "console", "view"],
  };
}
