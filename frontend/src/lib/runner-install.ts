// oneLineInstall builds a copy-paste command that downloads the runner (via the
// enrollment token, so it works on the target machine with no browser session),
// then installs it to auto-start at login and connects it now. The token goes in
// the Authorization header, never the URL.
//
// origin is the appliance's *configured* origin, not the browser's — an operator
// may reach the dashboard by a name the target machine cannot resolve, or over a
// scheme the runner refuses.
//
// pin identifies a self-signed appliance certificate. It is empty when the
// appliance has a publicly-trusted certificate, in which case the runner just
// verifies against the system trust store. It is not a secret.
//
// Shared by the onboarding dialog and the device's enrollment card, so a rotated
// token gives the operator the same one-liner as a freshly registered device.
export function oneLineInstall(platform: string, origin: string, token: string, pin = ""): string {
  const url = (arch: string) => `${origin}/api/v1/runner/download?platform=${platform}&arch=${arch}`;
  if (platform === "windows") {
    const pinArg = pin ? ` --pin '${pin}'` : "";
    return (
      `$t='${token}'; ` +
      `iwr '${url("amd64")}' -Headers @{Authorization="Bearer $t"} -OutFile opencuttles-runner.exe; ` +
      `.\\opencuttles-runner.exe install --appliance '${origin}' --token $t${pinArg}`
    );
  }
  // Linux/macOS: detect the arch (Apple Silicon vs Intel) so one command covers both.
  const pinArg = pin ? ` --pin '${pin}'` : "";
  return (
    `T='${token}'; A=$(uname -m); [ "$A" = x86_64 ] && A=amd64; [ "$A" = aarch64 ] && A=arm64; ` +
    `curl -fsSL -H "Authorization: Bearer $T" '${origin}/api/v1/runner/download?platform=${platform}&arch='"$A" -o opencuttles-runner && ` +
    `chmod +x opencuttles-runner && ./opencuttles-runner install --appliance '${origin}' --token "$T"${pinArg}`
  );
}
