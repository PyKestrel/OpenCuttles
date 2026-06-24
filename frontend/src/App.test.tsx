import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "./App";

describe("App", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders bootstrap form when no admin exists", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(response({ required: true }));
    render(<App />);
    expect(await screen.findByText("Bootstrap local admin")).toBeInTheDocument();
  });

  it("renders dashboard for an authenticated user", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const url = String(input);
      if (url.endsWith("/bootstrap")) return Promise.resolve(response({ required: false }));
      if (url.endsWith("/auth/me")) return Promise.resolve(response(principal()));
      if (url.endsWith("/host")) {
        return Promise.resolve(response({ id: "local", name: "host-01", cpuCount: 8, memoryBytes: 0, diskFreeBytes: 0, prerequisites: [], updatedAt: new Date().toISOString() }));
      }
      if (
        url.endsWith("/images") ||
        url.endsWith("/instances") ||
        url.endsWith("/operations") ||
        url.endsWith("/audit") ||
        url.endsWith("/android-versions")
      ) {
        return Promise.resolve(response([]));
      }
      if (url.endsWith("/health")) {
        return Promise.resolve(response({ status: "ok", checks: [], generatedAt: new Date().toISOString() }));
      }
      return Promise.resolve(response({}));
    });

    render(<App />);
    await waitFor(() => expect(screen.getByText("Android device control plane")).toBeInTheDocument());
    expect(screen.getByText("host-01")).toBeInTheDocument();
  });
});

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
