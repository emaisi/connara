import { useContext, useEffect } from "react";
import { UNSAFE_DataRouterContext, useBlocker } from "react-router";
import { useUnsavedChanges } from "../unsaved";

function Blocker({ dirty, prefix }: { dirty: boolean; prefix: string }) {
  const blocker = useBlocker(
    ({ nextLocation }) => dirty && nextLocation.pathname !== prefix && !nextLocation.pathname.startsWith(prefix + "/"),
  );
  useEffect(() => {
    if (blocker.state === "blocked") {
      if (window.confirm("存在尚未保存的输入，确认离开当前工作流？")) blocker.proceed();
      else blocker.reset();
    }
  }, [blocker]);
  useEffect(() => {
    if (!dirty) return;
    const unload = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", unload);
    return () => window.removeEventListener("beforeunload", unload);
  }, [dirty]);
  return null;
}
export function WorkflowNavigationGuard(props: { dirty: boolean; prefix: string }) {
  const dataRouter = useContext(UNSAFE_DataRouterContext);
  useUnsavedChanges(!dataRouter && props.dirty, props.prefix);
  return dataRouter ? <Blocker {...props} /> : null;
}
