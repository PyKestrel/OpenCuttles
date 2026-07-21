import { describe, expect, it } from "vitest";
import { oneLineInstall } from "@/lib/runner-install";

describe("oneLineInstall", () => {
  // A single backslash in a template literal silently collapses ("\o" -> "o"),
  // which would emit ".opencuttles-runner.exe" and fail on the target machine
  // with a confusing error. Nothing else catches that.
  it("emits a runnable Windows path", () => {
    const cmd = oneLineInstall("windows", "https://appliance.example", "tok");
    expect(cmd).toContain(".\\opencuttles-runner.exe install");
    expect(cmd).not.toContain(".opencuttles-runner.exe install");
  });

  it("passes the pin when the appliance is self-signed", () => {
    const pin = "sha256/Jbf337w2B2HsFQ3jGFljc3TeEM0Gwi1YDT9sAcuGSHY=";
    for (const platform of ["windows", "linux", "macos"]) {
      const cmd = oneLineInstall(platform, "https://10.1.0.104", "tok", pin);
      expect(cmd, platform).toContain(`--pin '${pin}'`);
    }
  });

  // A publicly-trusted appliance needs no pin, and passing an empty one would
  // make the runner reject its own flag value.
  it("omits the pin flag entirely when there is no pin", () => {
    for (const platform of ["windows", "linux", "macos"]) {
      const cmd = oneLineInstall(platform, "https://testral.example", "tok");
      expect(cmd, platform).not.toContain("--pin");
    }
  });

  it("uses the appliance origin it is given, not a hardcoded one", () => {
    const cmd = oneLineInstall("linux", "https://testral.example", "tok");
    expect(cmd).toContain("--appliance 'https://testral.example'");
    expect(cmd).toContain("https://testral.example/api/v1/runner/download");
  });

  // The token authenticates the download, so it must travel in the header and
  // never in the URL where it would land in proxy and server logs.
  it("keeps the token out of the URL", () => {
    for (const platform of ["windows", "linux"]) {
      const cmd = oneLineInstall(platform, "https://appliance.example", "secret-token");
      const urlPart = cmd.slice(cmd.indexOf("/api/v1/runner/download"));
      expect(urlPart.split(" ")[0], platform).not.toContain("secret-token");
      expect(cmd, platform).toContain("Authorization");
    }
  });
});
