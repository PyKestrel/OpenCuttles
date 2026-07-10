import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import { ThemeProvider } from "./theme";

describe("App", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders the bootstrap gate when no admin exists", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(response({ required: true }));
    render(
      <ThemeProvider>
        <App />
      </ThemeProvider>,
    );
    expect(await screen.findByText("Create the first admin")).toBeInTheDocument();
  });

  it("renders the workspace shell for an authenticated user", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const url = String(input);
      if (url.endsWith("/bootstrap")) return Promise.resolve(response({ required: false }));
      if (url.endsWith("/auth/me")) return Promise.resolve(response(principal()));
      if (url.endsWith("/host")) {
        return Promise.resolve(response({ id: "local", name: "host-01", cpuCount: 8, memoryBytes: 0, diskFreeBytes: 0, prerequisites: [], updatedAt: new Date().toISOString() }));
      }
      return Promise.resolve(response([]));
    });

    render(
      <ThemeProvider>
        <App />
      </ThemeProvider>,
    );

    // The top bar brand appears once the session is established.
    expect(await screen.findByText("OpenCuttles")).toBeInTheDocument();
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
