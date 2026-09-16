import { useState, type FormEvent } from "react";
import { Link } from "react-router";
import { api } from "../api";
import { Button, Card, Field, fieldClass } from "../ui";
export function InvitationPage() {
  const token =
    new URLSearchParams(window.location.hash.slice(1)).get("token") ??
    new URLSearchParams(window.location.search).get("token") ??
    "";
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const [pending, setPending] = useState(false);
  async function accept(event: FormEvent) {
    event.preventDefault();
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api.acceptInvitation(token, password, name);
      window.history.replaceState(null, "", "/invitation");
      setPassword("");
      setDone(true);
    } catch (error) {
      setError(error instanceof Error ? error.message : "邀请接受失败");
    } finally {
      setPending(false);
    }
  }
  return (
    <main className="grid min-h-screen place-items-center p-4">
      <Card className="w-full max-w-md p-6">
        <h1 className="text-xl font-bold">接受团队邀请</h1>
        {done ? (
          <p className="mt-4">
            账号已激活。
            <Link className="text-blue-600" to="/">
              前往登录
            </Link>
          </p>
        ) : (
          <form className="mt-4 grid gap-4" onSubmit={accept}>
            <p className="text-sm">邀请链接只能使用一次；请设置自己的登录密码。</p>
            <Field label="显示名称">
              <input
                className={fieldClass}
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                maxLength={100}
              />
            </Field>
            <Field label="登录密码（至少 12 个字符）">
              <input
                className={fieldClass}
                type="password"
                autoComplete="new-password"
                minLength={12}
                maxLength={72}
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </Field>
            {(!token || error) && (
              <p role="alert" className="text-red-600">
                {error || "邀请链接缺少令牌，请联系管理员重新创建邀请。"}
              </p>
            )}
            <Button type="submit" disabled={pending || !token}>
              {pending ? "正在激活…" : "接受邀请并激活账号"}
            </Button>
          </form>
        )}
      </Card>
    </main>
  );
}
