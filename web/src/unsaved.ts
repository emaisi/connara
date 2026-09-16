import { useEffect } from "react";
export function useUnsavedChanges(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return;
    const unload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
    };
    const navigate = (event: MouseEvent) => {
      const link = (event.target as Element)?.closest?.("a[href]") as HTMLAnchorElement | null;
      if (
        !link ||
        link.origin !== window.location.origin ||
        link.href === window.location.href ||
        event.defaultPrevented
      )
        return;
      if (!window.confirm("存在尚未保存的输入，确认离开当前页面？")) {
        event.preventDefault();
        event.stopPropagation();
      }
    };
    window.addEventListener("beforeunload", unload);
    document.addEventListener("click", navigate, true);
    return () => {
      window.removeEventListener("beforeunload", unload);
      document.removeEventListener("click", navigate, true);
    };
  }, [dirty]);
}
