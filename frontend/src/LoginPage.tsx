import { ArrowRight, LoaderCircle, Lock } from "lucide-react";
import { useEffect, useState, type CSSProperties, type ChangeEvent, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { getProtectedAppRoute, withBasePath } from "@/lib/app-paths";

type AuthSessionResponse = {
  enabled: boolean;
  authenticated: boolean;
};

type ErrorResponse = {
  error: string;
};

const loginThemeStyle = {
  "--canvas": "#ffffff",
  "--surface-subtle": "#fafafa",
  "--surface-alt": "#fafafa",
  "--surface-accent": "#ebf5ff",
  "--foreground": "#171717",
  "--foreground-muted": "#525252",
  "--foreground-soft": "#8a8a8a",
  "--primary": "#171717",
  "--primary-strong": "#000000",
  "--border": "#ebebeb",
  "--border-strong": "#171717",
  "--ring": "hsla(212, 100%, 48%, 1)",
} as CSSProperties;

export default function LoginPage() {
  const [password, setPassword] = useState("");
  const [authError, setAuthError] = useState("");
  const [authSubmitting, setAuthSubmitting] = useState(false);
  const [checkingSession, setCheckingSession] = useState(true);

  useEffect(() => {
    void loadSession();
  }, []);

  async function loadSession() {
    try {
      const response = await fetch(withBasePath("/api/auth/session"));
      if (!response.ok) {
        throw new Error("获取登录状态失败");
      }

      const data = (await response.json()) as AuthSessionResponse;
      if (!data.enabled || data.authenticated) {
        window.location.replace(getProtectedAppRoute());
        return;
      }

      setAuthError("");
    } catch (sessionError) {
      setAuthError(sessionError instanceof Error ? sessionError.message : "获取登录状态失败");
    } finally {
      setCheckingSession(false);
    }
  }

  async function handleLogin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    if (!password) {
      setAuthError("请输入访问密码");
      return;
    }

    setAuthSubmitting(true);
    setAuthError("");

    try {
      const response = await fetch(withBasePath("/api/auth/login"), {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ password }),
      });

      const data = (await response.json()) as AuthSessionResponse | ErrorResponse;
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "登录失败");
      }

      setPassword("");
      window.location.replace(getProtectedAppRoute());
    } catch (loginError) {
      setAuthError(loginError instanceof Error ? loginError.message : "登录失败");
    } finally {
      setAuthSubmitting(false);
    }
  }

  return (
    <div style={loginThemeStyle} className="min-h-screen bg-[color:var(--canvas)] text-[color:var(--foreground)]">
      <div className="mx-auto flex min-h-screen w-full max-w-[1280px] items-center justify-center px-6 py-10 lg:px-10">
        <div className="w-full max-w-[420px] space-y-8">
          <div className="space-y-3 text-center">
            <div className="text-[40px] font-semibold tracking-[-0.08em] text-[color:var(--foreground)]">Douyin Library</div>
          </div>

          <Card className="overflow-hidden rounded-2xl border-[color:var(--border)] bg-white shadow-none">
            <CardContent className="space-y-5 px-6 py-6">
              <div className="flex h-11 w-11 items-center justify-center rounded-xl border border-[color:var(--border)] bg-[color:var(--surface-subtle)] text-[color:var(--foreground)]">
                <Lock className="h-4.5 w-4.5" />
              </div>

              <form className="space-y-5" onSubmit={handleLogin}>
                <div className="space-y-2">
                  <Label htmlFor="login-password" className="text-sm font-medium text-[color:var(--foreground)]">
                    访问密码
                  </Label>
                  <Input
                    id="login-password"
                    type="password"
                    value={password}
                    onChange={(event: ChangeEvent<HTMLInputElement>) => setPassword(event.target.value)}
                    placeholder="输入启动参数中的密码"
                    autoComplete="current-password"
                    className="h-12 rounded-xl border-[color:var(--border)] bg-white px-4 text-base text-[color:var(--foreground)] shadow-none focus:border-[#171717] focus:ring-[#0a72ef]/20"
                    disabled={checkingSession || authSubmitting}
                  />
                </div>

                {authError ? (
                  <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm leading-6 text-rose-700">
                    {authError}
                  </div>
                ) : null}

                <Button
                  type="submit"
                  size="lg"
                  disabled={checkingSession || authSubmitting}
                  className="h-12 w-full rounded-xl bg-[#171717] text-white hover:bg-black"
                >
                  {checkingSession || authSubmitting ? (
                    <LoaderCircle className="mr-2 h-4 w-4 animate-spin" />
                  ) : (
                    <ArrowRight className="mr-2 h-4 w-4" />
                  )}
                  验证密码并进入
                </Button>
              </form>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
