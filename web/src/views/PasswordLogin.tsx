import { t } from "../i18n";
import { useEffect, useState } from "react";
import { errorText } from "../lib/api";
import { pair } from "../lib/auth";
import { setToken } from "../lib/api";
import { loginWithPassword, passwordApi, type PasswordState } from "../lib/password";
import { Button, Spinner } from "../ui/atoms";
import { Icon } from "../ui/icons";

// Sign in with a connection password or pair with a hub or device token. `linkError` reports an invalid token from a link.
export function PasswordLogin({ onDone, error: linkError = "" }: { onDone: () => void; error?: string }) {
  const [token, setTok] = useState("");
  const [passwordEnabled, setPasswordEnabled] = useState(false);
  const [mode, setMode] = useState<"password" | "token">("password");
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    let live = true;
    passwordApi<PasswordState>("/state").then((state) => {
      if (!live) return;
      setPasswordEnabled(state.enabled);
      if (!state.enabled) setMode("token");
    }, () => { if (live) setMode("token"); })
      .finally(() => { if (live) setLoading(false); });
    return () => { live = false; };
  }, []);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(linkError);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (busy || loading) return;
    const credential = mode === "password" ? token : token.trim();
    if (!credential) return;
    setBusy(true);
    setError("");
    try {
      if (mode === "password") {
        const pending = loginWithPassword(credential);
        setTok("");
        const result = await pending;
        if (!("token" in result)) throw new Error(t("password.invalid_result"));
        setToken(result.token, result.id);
        onDone();
      } else {
        await pair(credential);
        onDone();
      }
    } catch (err) {
      setError(errorText(err) === "invalid token" ? t("password.invalid_token") : errorText(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex min-h-full items-center justify-center bg-page px-5 py-16">
      <form onSubmit={submit} className="w-full max-w-[360px]" style={{ animation: "fade-up 450ms cubic-bezier(0.23,1,0.32,1) both" }}>
        <div className="mb-7 flex flex-col items-center gap-3 text-center">
          <span className="flex size-11 items-center justify-center rounded-[12px] bg-ink text-surface shadow-raised">
            <Icon name="spark" size={20} />
          </span>
          <div>
            <h1 className="text-[18px] font-semibold tracking-tight text-ink">{t("password.title")}</h1>
            <p className="mt-1 text-[13px] leading-relaxed text-ink-2">{mode === "password" ? t("password.prompt") : t("password.token_prompt")}</p>
          </div>
        </div>
        <div className="rounded-card bg-surface p-1.5 shadow-card">
          <label className="flex h-10 items-center gap-2 rounded-control bg-inset px-3 shadow-hairline focus-within:shadow-[0_0_0_1px_var(--line-strong)]">
            <Icon name="key" size={14} className="text-ink-3" />
            <input
              autoFocus
              type="password"
              value={token}
              onChange={(e) => setTok(e.target.value)}
              placeholder={mode === "password" ? t("password.connection_password") : "hub token"}
              aria-label={mode === "password" ? t("password.connection_password") : "hub token"}
              autoComplete={mode === "password" ? "current-password" : "off"}
              disabled={loading || busy}
              className="min-w-0 flex-1 bg-transparent font-mono text-[13px] text-ink outline-none placeholder:font-sans placeholder:text-ink-3"
            />
          </label>
          <Button type="submit" tone="primary" disabled={loading || busy || !token} className="mt-1.5 h-9 w-full">
            {busy || loading ? <Spinner /> : t("password.connect")}
          </Button>
        </div>
        <p className="mt-3 h-5 text-center text-[12.5px] text-red" role="alert">
          {error}
        </p>
        {passwordEnabled && <button type="button" className="mb-4 w-full text-center text-[12.5px] text-ink-2" disabled={busy} onClick={() => { setMode(mode === "password" ? "token" : "password"); setTok(""); setError(""); }}>
          {mode === "password" ? t("password.use_token") : t("password.use_password")}
        </button>}
        <p className="text-center text-[11.5px] leading-relaxed text-ink-3">{t("password.browser_credential")}</p>
      </form>
    </div>
  );
}
