import { useState, type FormEvent } from "react";
import { BrandMark } from "@/components/Brand";
import { DitherField } from "@/components/DitherField";
import { api } from "@/api";
import type { Principal } from "@/types";

export function AuthGate({
  bootstrapRequired,
  onAuthenticated,
}: {
  bootstrapRequired: boolean;
  onAuthenticated: (p: Principal) => void;
}) {
  const [username, setUsername] = useState("admin");
  const [displayName, setDisplayName] = useState("Testral Admin");
  const [password, setPassword] = useState("");
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (bootstrapRequired) {
        const p = await api.bootstrapAdmin({ username, displayName, password, token });
        await api.login({ username, password });
        onAuthenticated(p);
      } else {
        const res = await api.login({ username, password });
        onAuthenticated(res.principal);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Authentication failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="relative grid min-h-screen place-items-center overflow-hidden bg-background p-4">
      {/* dithered brand hero */}
      <div className="pointer-events-none absolute inset-0">
        <DitherField />
      </div>
      <div className="pointer-events-none absolute inset-0" style={{ backgroundImage: "radial-gradient(var(--dot-tex) 1px, transparent 1.5px)", backgroundSize: "20px 20px" }} />
      <form onSubmit={submit} className="relative w-full max-w-[380px] rounded-2xl border bg-card/95 p-7 backdrop-blur-sm" style={{ boxShadow: "var(--card-shadow), 0 20px 60px rgba(0,0,0,0.18)" }}>
        <div className="mb-5 flex items-center gap-2.5">
          <BrandMark className="size-8" />
          <div>
            <div className="font-semibold">Testral</div>
            <div className="text-[12px] text-muted-foreground">{bootstrapRequired ? "Create the first admin" : "Sign in"}</div>
          </div>
        </div>

        {error && (
          <div className="mb-3 rounded-lg border px-3 py-2 text-[13px]" style={{ borderColor: "color-mix(in srgb, var(--destructive) 35%, transparent)", background: "color-mix(in srgb, var(--destructive) 10%, transparent)", color: "var(--destructive)" }}>
            {error}
          </div>
        )}

        <Field label="Username" value={username} onChange={setUsername} />
        {bootstrapRequired && <Field label="Display name" value={displayName} onChange={setDisplayName} />}
        <Field label="Password" type="password" value={password} onChange={setPassword} placeholder={bootstrapRequired ? "At least 12 characters" : "Password"} />
        {bootstrapRequired && <Field label="Bootstrap token" type="password" value={token} onChange={setToken} placeholder="From OPENCUTTLES_BOOTSTRAP_TOKEN" />}

        <button
          disabled={busy || !username || !password}
          className="mt-2 w-full rounded-lg py-2.5 text-[14px] font-medium text-primary-foreground disabled:opacity-50"
          style={{ background: "var(--primary-strong)" }}
        >
          {bootstrapRequired ? "Create admin" : "Sign in"}
        </button>
      </form>
    </div>
  );
}

function Field({
  label,
  value,
  onChange,
  type = "text",
  placeholder,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  type?: string;
  placeholder?: string;
}) {
  return (
    <label className="mb-3 block">
      <span className="mb-1 block text-[12px] text-muted-foreground">{label}</span>
      <input
        type={type}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        className="w-full rounded-lg border bg-secondary px-3 py-2 text-[14px] outline-none focus:border-[var(--ring)]"
      />
    </label>
  );
}
