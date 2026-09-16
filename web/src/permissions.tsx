import { createContext, useContext } from "react";
export const PermissionContext = createContext<string | undefined>(undefined);
export function useCanWrite(adminOnly = false) {
  const role = useContext(PermissionContext);
  return role === "owner" || role === "admin" || (!adminOnly && role === "developer");
}
