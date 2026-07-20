// oneLineInstall builds a copy-paste command that downloads the runner (via the
// enrollment token, so it works on the target machine with no browser session),
// then installs it to auto-start at login and connects it now. The token goes in
// the Authorization header, never the URL.
//
// Shared by the onboarding dialog and the device's enrollment card, so a rotated
// token gives the operator the same one-liner as a freshly registered device.
export function oneLineInstall(platform: string, origin: string, token: string): string {
  const url = (arch: string) => `${origin}/api/v1/runner/download?platform=${platform}&arch=${arch}`;
  if (platform === "windows") {
    return (
      `$t='${token}'; ` +
      `iwr '${url("amd64")}' -Headers @{Authorization="Bearer $t"} -OutFile opencuttles-runner.exe; ` +
      `.\\opencuttles-runner.exe install --appliance '${origin}' --token $t`
    );
  }
  // Linux/macOS: detect the arch (Apple Silicon vs Intel) so one command covers both.
  return (
    `T='${token}'; A=$(uname -m); [ "$A" = x86_64 ] && A=amd64; [ "$A" = aarch64 ] && A=arm64; ` +
    `curl -fsSL -H "Authorization: Bearer $T" '${origin}/api/v1/runner/download?platform=${platform}&arch='"$A" -o opencuttles-runner && ` +
    `chmod +x opencuttles-runner && ./opencuttles-runner install --appliance '${origin}' --token "$T"`
  );
}
