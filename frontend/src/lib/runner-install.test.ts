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

  describe("mutual TLS", () => {
    const bundle = JSON.stringify({
      clientCertPem: "-----BEGIN CERTIFICATE-----\nAAA\n-----END CERTIFICATE-----\n",
      clientKeyPem: "-----BEGIN EC PRIVATE KEY-----\nBBB\n-----END EC PRIVATE KEY-----\n",
      caCertPem: "-----BEGIN CERTIFICATE-----\nCCC\n-----END CERTIFICATE-----\n",
    });

    // Without a bundle nothing about the command may change — mutual TLS is off
    // for almost every deployment.
    it("adds nothing when there is no bundle", () => {
      for (const platform of ["windows", "linux"]) {
        const cmd = oneLineInstall(platform, "https://app.example", "tok");
        expect(cmd, platform).not.toContain("--identity");
        expect(cmd, platform).not.toContain("opencuttles-identity.json");
      }
    });

    it("writes the bundle, passes --identity, and removes the temp copy", () => {
      for (const platform of ["windows", "linux"]) {
        const cmd = oneLineInstall(platform, "https://app.example", "tok", "", bundle);
        expect(cmd, platform).toContain("--identity opencuttles-identity.json");
        expect(cmd, platform).toContain(bundle);
        // The file holds a private key; leaving it in the download directory
        // after install has copied it elsewhere serves no purpose.
        expect(cmd, platform).toMatch(/rm -f opencuttles-identity\.json|Remove-Item -Force opencuttles-identity\.json/);
      }
    });

    // Windows PowerShell 5.1 is the default shell on Windows 10/11 and writes a
    // UTF-8 BOM with `Set-Content -Encoding utf8`. A BOM makes Go's JSON decoder
    // reject the bundle, so the command must not use that.
    it("writes the Windows file without a BOM", () => {
      const cmd = oneLineInstall("windows", "https://app.example", "tok", "", bundle);
      expect(cmd).toContain("[IO.File]::WriteAllText");
      expect(cmd).not.toContain("Set-Content");
    });

    // The mTLS listener is a separate port, so the runner must dial that rather
    // than the dashboard origin — while the download still comes from the origin,
    // because no certificate exists yet to authenticate with.
    it("dials the mTLS endpoint but downloads from the origin", () => {
      const cmd = oneLineInstall("linux", "https://app.example", "tok", "", bundle, "https://app.example:8443");
      expect(cmd).toContain("--appliance 'https://app.example:8443'");
      expect(cmd).toContain("'https://app.example/api/v1/runner/download");
    });

    // Everything is single-quoted, which is only safe while no value can contain
    // a quote. A quote would close the string and splice the rest into the shell.
    it("refuses values containing a single quote", () => {
      expect(() => oneLineInstall("linux", "https://app.example", "tok'; rm -rf /; echo '")).toThrow();
      expect(() => oneLineInstall("linux", "https://app.example", "tok", "", `{"a":"it's"}`)).toThrow();
    });

    // JSON.stringify escapes the PEM's newlines, so the whole command stays on
    // one line — a literal newline would truncate it at the paste boundary.
    it("stays a single line", () => {
      for (const platform of ["windows", "linux"]) {
        const cmd = oneLineInstall(platform, "https://app.example", "tok", "", bundle);
        expect(cmd.includes("\n"), platform).toBe(false);
      }
    });
  });
});
