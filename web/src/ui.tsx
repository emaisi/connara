import { useUnsavedChanges } from "./unsaved";
import { useCanWrite } from "./permissions";
import * as Dialog from "@radix-ui/react-dialog";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import clsx, { type ClassValue } from "clsx";
import { AlertCircle, CheckCircle2, Copy, Inbox, LoaderCircle, X } from "lucide-react";
import type { ButtonHTMLAttributes, HTMLAttributes, ReactNode, Ref } from "react";
import { useEffect, useState, type FormHTMLAttributes } from "react";
import { twMerge } from "tailwind-merge";

export function cn(...values: ClassValue[]): string {
  return twMerge(clsx(values));
}

const buttonVariants = cva(
  "inline-flex h-9 items-center justify-center gap-2 rounded-lg px-3.5 text-sm font-semibold transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        primary: "bg-slate-950 text-white shadow-sm hover:bg-slate-800 dark:bg-blue-500 dark:hover:bg-blue-400",
        secondary: "border border-[var(--border)] bg-[var(--surface)] text-[var(--text)] hover:bg-[var(--muted)]",
        ghost: "text-[var(--muted-text)] hover:bg-[var(--muted)] hover:text-[var(--text)]",
        danger: "bg-red-600 text-white hover:bg-red-500",
      },
    },
    defaultVariants: { variant: "primary" },
  },
);

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  asChild?: boolean;
  write?: boolean;
  adminOnly?: boolean;
  ref?: Ref<HTMLButtonElement>;
}

export function Button({ className, variant, asChild, ref, write = false, adminOnly = false, ...props }: ButtonProps) {
  const canWrite = useCanWrite(adminOnly);
  const [pending, setPending] = useState(false);
  const click = props.onClick;
  if (!asChild)
    props.onClick = (event) => {
      if (pending) return;
      const result = click?.(event) as unknown;
      if (result && typeof (result as Promise<unknown>).then === "function") {
        setPending(true);
        void Promise.resolve(result)
          .finally(() => setPending(false))
          .catch(() => undefined);
      }
    };
  props.disabled = props.disabled || pending;
  props["aria-busy"] = pending;
  if ((write || adminOnly) && !canWrite) {
    props.disabled = true;
    props.title = "当前角色没有操作权限";
  }
  const Component = asChild ? Slot : "button";
  return <Component ref={ref} className={cn(buttonVariants({ variant }), className)} {...props} />;
}

export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("rounded-2xl border border-[var(--border)] bg-[var(--surface)] shadow-sm", className)}
      {...props}
    />
  );
}

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description: string;
  actions?: ReactNode;
}) {
  return (
    <header className="flex flex-col justify-between gap-4 sm:flex-row sm:items-end">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-[var(--text)]">{title}</h1>
        <p className="mt-1 max-w-3xl text-sm leading-6 text-[var(--muted-text)]">{description}</p>
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </header>
  );
}

export function Badge({
  children,
  tone = "neutral",
}: {
  children: ReactNode;
  tone?: "neutral" | "success" | "warning" | "danger" | "info";
}) {
  const tones = {
    neutral: "bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300",
    success: "bg-emerald-50 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300",
    warning: "bg-amber-50 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
    danger: "bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300",
    info: "bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300",
  };
  return (
    <span className={cn("inline-flex rounded-full px-2 py-1 text-xs font-semibold", tones[tone])}>{children}</span>
  );
}

export function LoadingState({ label = "正在加载" }: { label?: string }) {
  return (
    <div className="flex min-h-48 items-center justify-center gap-2 text-sm text-[var(--muted-text)]">
      <LoaderCircle className="size-4 animate-spin" /> {label}
    </div>
  );
}

export function EmptyState({ title, description, action }: { title: string; description: string; action?: ReactNode }) {
  return (
    <div className="flex min-h-48 flex-col items-center justify-center px-6 text-center">
      <span className="mb-3 rounded-xl bg-[var(--muted)] p-3">
        <Inbox className="size-5 text-[var(--muted-text)]" />
      </span>
      <h3 className="font-semibold text-[var(--text)]">{title}</h3>
      <p className="mt-1 max-w-md text-sm text-[var(--muted-text)]">{description}</p>
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

export function ErrorState({ error }: { error: unknown }) {
  const message = error instanceof Error ? error.message : "发生未知错误";
  return (
    <div
      role="alert"
      className="flex min-h-40 items-center justify-center gap-2 rounded-xl border border-red-200 bg-red-50 p-6 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300"
    >
      <AlertCircle className="size-4" /> {message}
    </div>
  );
}

export function SuccessNote({ children }: { children: ReactNode }) {
  return (
    <div className="flex gap-2 rounded-xl bg-emerald-50 p-3 text-sm text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300">
      <CheckCircle2 className="mt-0.5 size-4 shrink-0" />
      {children}
    </div>
  );
}

export function Modal({
  open,
  onOpenChange,
  title,
  description,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  children: ReactNode;
}) {
  const [dirty, setDirty] = useState(false);
  useUnsavedChanges(dirty && open);
  useEffect(() => {
    if (!open) setDirty(false);
  }, [open]);
  return (
    <Dialog.Root
      open={open}
      onOpenChange={(value) => {
        if (!value && dirty && !window.confirm("存在尚未保存的输入，确认关闭？")) return;
        onOpenChange(value);
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-slate-950/55 backdrop-blur-[2px]" />
        <Dialog.Content className="fixed left-1/2 top-1/2 z-50 max-h-[90vh] w-[min(94vw,34rem)] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-2xl border border-[var(--border)] bg-[var(--surface)] p-6 shadow-2xl focus:outline-none">
          <div className="pr-10">
            <Dialog.Title className="text-lg font-bold text-[var(--text)]">{title}</Dialog.Title>
            <Dialog.Description className="mt-1 text-sm leading-6 text-[var(--muted-text)]">
              {description}
            </Dialog.Description>
          </div>
          <Dialog.Close asChild>
            <Button variant="ghost" className="absolute right-4 top-4 size-8 p-0" aria-label="关闭">
              <X className="size-4" />
            </Button>
          </Dialog.Close>
          <div className="mt-5" onChangeCapture={() => setDirty(true)}>
            {children}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function CopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState(false);
  return (
    <Button
      type="button"
      variant="secondary"
      title={copyError ? "无法访问剪贴板，请选择内容后手动复制" : undefined}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setCopyError(false);
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1500);
        } catch {
          setCopyError(true);
        }
      }}
    >
      <Copy className="size-4" />
      {copyError ? "复制失败，请手动复制" : copied ? "已复制" : "复制"}
    </Button>
  );
}

export const fieldClass =
  "h-10 min-w-0 w-full max-w-full rounded-lg border border-[var(--border)] bg-[var(--surface)] px-3 text-sm text-[var(--text)] outline-none transition placeholder:text-[var(--muted-text)] focus:border-blue-500 focus:ring-2 focus:ring-blue-500/15";
export const textAreaClass =
  "min-h-24 w-full resize-y rounded-lg border border-[var(--border)] bg-[var(--surface)] px-3 py-2 text-sm text-[var(--text)] outline-none transition placeholder:text-[var(--muted-text)] focus:border-blue-500 focus:ring-2 focus:ring-blue-500/15";

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="grid min-w-0 gap-1.5 text-sm font-medium text-[var(--text)]">
      <span>{label}</span>
      {children}
      {hint && <span className="text-xs font-normal text-[var(--muted-text)]">{hint}</span>}
    </label>
  );
}

export function MutationForm({
  children,
  onSubmit,
  adminOnly = false,
  ...props
}: FormHTMLAttributes<HTMLFormElement> & { adminOnly?: boolean }) {
  const allowed = useCanWrite(adminOnly);
  const [pending, setPending] = useState(false);
  return (
    <form
      {...props}
      aria-busy={pending}
      onSubmit={(event) => {
        if (!allowed || pending) {
          event.preventDefault();
          return;
        }
        const result = onSubmit?.(event) as unknown;
        if (result && typeof (result as Promise<unknown>).then === "function") {
          setPending(true);
          void Promise.resolve(result)
            .finally(() => setPending(false))
            .catch(() => undefined);
        }
      }}
    >
      <fieldset disabled={!allowed || pending} className="contents">
        {children}
      </fieldset>
    </form>
  );
}
