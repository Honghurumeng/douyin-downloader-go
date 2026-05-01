import { ArrowRight, LoaderCircle, Lock, Shield, Sparkles } from "lucide-react";
import { useEffect, useState, type ChangeEvent, type FormEvent, type ReactNode } from "react";

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
  retryAfterSeconds?: number;
};

export default function LoginPage() {
  const [authEnabled, setAuthEnabled] = useState(true);
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

      setAuthEnabled(true);
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
    <div className="relative min-h-screen overflow-hidden bg-[#08101f] text-white">
      <AtmosphericGlassBackdrop />

      <div className="relative mx-auto flex min-h-screen w-full max-w-[1400px] items-center px-6 py-10 lg:px-10">
        <div className="grid w-full items-center gap-8 lg:grid-cols-[minmax(0,1.15fr)_430px]">
          <section className="space-y-8">
            <div className="inline-flex items-center gap-2 rounded-full border border-white/14 bg-white/8 px-4 py-2 text-xs uppercase tracking-[0.22em] text-white/72 backdrop-blur-xl">
              <Sparkles className="h-3.5 w-3.5" />
              Atmospheric Glass Login
            </div>

            <div className="max-w-3xl space-y-5">
              <h1 className="text-4xl font-semibold tracking-[-0.04em] text-white sm:text-5xl lg:text-6xl">
                服务端校验通过后，才会返回视频管理页。
              </h1>
              <p className="max-w-2xl text-base leading-8 text-white/68 sm:text-lg">
                现在登录页和应用页已经在服务端分离。未通过密码校验前，服务端只会返回这张登录页，不会返回视频管理页的 HTML。
              </p>
            </div>

            <div className="grid gap-4 sm:grid-cols-3">
              <GlassStat icon={<Shield className="h-4 w-4" />} title="真实分离" value="登录页和应用页由服务端分别返回" />
              <GlassStat icon={<Lock className="h-4 w-4" />} title="接口受保护" value="视频、标签、媒体资源都要求已登录" />
              <GlassStat icon={<ArrowRight className="h-4 w-4" />} title="限流登录" value="密码错误过多会临时拒绝继续尝试" />
            </div>
          </section>

          <Card className="overflow-hidden rounded-[30px] border-white/14 bg-white/10 text-white shadow-[0_28px_120px_rgba(2,6,23,0.55)] backdrop-blur-[34px]">
            <div className="border-b border-white/10 bg-white/6 px-6 py-5">
              <div className="flex items-center gap-3">
                <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-white/12 bg-white/10">
                  <Lock className="h-5 w-5 text-white/88" />
                </div>
                <div>
                  <div className="text-lg font-semibold text-white">进入视频管理页</div>
                  <div className="mt-1 text-sm text-white/62">
                    {authEnabled ? "请输入服务启动时设置的访问密码。" : "当前服务未开启密码保护，正在为你跳转。"}
                  </div>
                </div>
              </div>
            </div>

            <CardContent className="space-y-5 px-6 py-6">
              <form className="space-y-5" onSubmit={handleLogin}>
                <div className="space-y-2">
                  <Label htmlFor="login-password" className="text-sm font-medium text-white/80">
                    访问密码
                  </Label>
                  <Input
                    id="login-password"
                    type="password"
                    value={password}
                    onChange={(event: ChangeEvent<HTMLInputElement>) => setPassword(event.target.value)}
                    placeholder="输入启动参数中的密码"
                    autoComplete="current-password"
                    className="h-12 rounded-2xl border-white/14 bg-white/10 px-4 text-base text-white placeholder:text-white/35 focus:border-white/24 focus:ring-white/12"
                    disabled={checkingSession || authSubmitting}
                  />
                </div>

                {authError ? (
                  <div className="rounded-2xl border border-rose-400/30 bg-rose-400/12 px-4 py-3 text-sm text-rose-100">
                    {authError}
                  </div>
                ) : (
                  <div className="rounded-2xl border border-white/10 bg-black/10 px-4 py-3 text-sm leading-6 text-white/62">
                    登录成功后，浏览器会重新请求受保护的应用页，而不是仅在前端切换显示状态。
                  </div>
                )}

                <Button
                  type="submit"
                  size="lg"
                  disabled={checkingSession || authSubmitting}
                  className="h-12 w-full rounded-2xl border border-white/16 bg-white text-[#0b1326] shadow-[0_10px_32px_rgba(255,255,255,0.18)] hover:bg-white/92"
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

function AtmosphericGlassBackdrop() {
  return (
    <>
      <div className="absolute inset-0 bg-[radial-gradient(circle_at_top,_rgba(56,189,248,0.18),_transparent_32%),radial-gradient(circle_at_20%_20%,_rgba(139,92,246,0.24),_transparent_28%),linear-gradient(180deg,_#0b1220_0%,_#08101f_42%,_#060b16_100%)]" />
      <div className="absolute left-[8%] top-[10%] h-64 w-64 rounded-full bg-cyan-300/14 blur-[120px]" />
      <div className="absolute right-[10%] top-[18%] h-72 w-72 rounded-full bg-fuchsia-300/12 blur-[140px]" />
      <div className="absolute bottom-[8%] left-[28%] h-80 w-80 rounded-full bg-sky-200/10 blur-[160px]" />
      <div className="absolute inset-0 opacity-30 [background-image:linear-gradient(rgba(255,255,255,0.08)_1px,transparent_1px),linear-gradient(90deg,rgba(255,255,255,0.06)_1px,transparent_1px)] [background-size:28px_28px]" />
      <div className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-white/30 to-transparent" />
    </>
  );
}

function GlassStat({ icon, title, value }: { icon: ReactNode; title: string; value: string }) {
  return (
    <div className="rounded-[26px] border border-white/12 bg-white/8 p-5 shadow-[0_18px_50px_rgba(2,6,23,0.34)] backdrop-blur-[24px]">
      <div className="flex h-9 w-9 items-center justify-center rounded-2xl border border-white/12 bg-white/10 text-white/85">
        {icon}
      </div>
      <div className="mt-6 text-sm font-medium text-white/86">{title}</div>
      <div className="mt-2 text-sm leading-6 text-white/56">{value}</div>
    </div>
  );
}
