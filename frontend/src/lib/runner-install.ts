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
// bundle is the client-certificate material issued when the appliance requires
// mutual TLS, serialized as the JSON the runner expects. Empty in the common
// case. The command writes it to a temp file, points --identity at it, and
// removes it afterwards — install copies it to the runner's own location, so
// leaving a private key in the download directory would serve no purpose.
//
// Note the download step still uses `origin` and the bearer token even under
// mutual TLS: no client certificate exists until enrollment completes, so the
// download endpoint cannot require one.
export function oneLineInstall(
  platform: string,
  origin: string,
  token: string,
  pin = "",
  bundle = "",
  dialOrigin = "",
): string {
  // Everything below is wrapped in single quotes, which is safe because none of
  // these values can contain one: the token is hex, the pin is base64, a URL has
  // no use for one, and the bundle is JSON.stringify output over PEM (base64 and
  // ASCII hyphens). Asserted rather than assumed — a quote here would end the
  // string and splice the rest into the shell.
  for (const v of [origin, token, pin, bundle, dialOrigin]) {
    if (v.includes("'")) throw new Error("install command values must not contain a single quote");
  }

  const url = (arch: string) => `${origin}/api/v1/runner/download?platform=${platform}&arch=${arch}`;
  // Under mutual TLS the runner dials a separate listener, not the dashboard port.
  const dial = dialOrigin || origin;
  const pinArg = pin ? ` --pin '${pin}'` : "";

  if (platform === "windows") {
    const idFile = "opencuttles-identity.json";
    // WriteAllText rather than Set-Content: Windows PowerShell 5.1 — still the
    // default shell on Windows 10 and 11 — writes a UTF-8 BOM with
    // `-Encoding utf8`, and a BOM makes Go's JSON decoder reject the file. This
    // overload is BOM-free on both 5.1 and 7. $PWD is passed explicitly because
    // .NET resolves relative paths against its own working directory, which is
    // not always PowerShell's.
    const writeId = bundle ? `[IO.File]::WriteAllText("$PWD\\${idFile}", '${bundle}'); ` : "";
    const idArg = bundle ? ` --identity ${idFile}` : "";
    const cleanup = bundle ? `; Remove-Item -Force ${idFile}` : "";
    return (
      `$t='${token}'; ` +
      `iwr '${url("amd64")}' -Headers @{Authorization="Bearer $t"} -OutFile opencuttles-runner.exe; ` +
      writeId +
      `.\\opencuttles-runner.exe install --appliance '${dial}' --token $t${pinArg}${idArg}${cleanup}`
    );
  }
  // Linux/macOS: detect the arch (Apple Silicon vs Intel) so one command covers both.
  const idFile = "opencuttles-identity.json";
  // umask 077 so the key is never briefly world-readable between write and remove.
  const writeId = bundle ? `(umask 077; printf '%s' '${bundle}' > ${idFile}) && ` : "";
  const idArg = bundle ? ` --identity ${idFile}` : "";
  const cleanup = bundle ? `; rm -f ${idFile}` : "";
  return (
    `T='${token}'; A=$(uname -m); [ "$A" = x86_64 ] && A=amd64; [ "$A" = aarch64 ] && A=arm64; ` +
    `curl -fsSL -H "Authorization: Bearer $T" '${origin}/api/v1/runner/download?platform=${platform}&arch='"$A" -o opencuttles-runner && ` +
    `chmod +x opencuttles-runner && ` +
    writeId +
    `./opencuttles-runner install --appliance '${dial}' --token "$T"${pinArg}${idArg}${cleanup}`
  );
}
