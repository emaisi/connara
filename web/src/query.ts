import { QueryClient } from "@tanstack/react-query";
export const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 30_000, retry: 1, refetchOnWindowFocus: false } },
});
export function invalidateResources(path: string) {
  const resource = path.split("/")[2];
  const related: Record<string, string[]> = {
    systems: ["system-groups", "actions", "integrations"],
    "system-groups": ["systems"],
    "auth-templates": ["auth-instances", "systems"],
    "auth-instances": ["integrations", "connections"],
    integrations: ["systems", "connections"],
    connections: ["systems", "integrations", "operations"],
    actions: ["systems", "operations", "metrics", "connections"],
    "sync-tasks": ["operations", "metrics"],
    "webhook-endpoints": ["webhook-deliveries"],
    "webhook-deliveries": ["operations"],
    settings: ["meta"],
  };
  const resources = new Set([resource, "lookups", ...(related[resource] ?? [])]);
  void queryClient.invalidateQueries({
    predicate: (q) => typeof q.queryKey[0] === "string" && resources.has(q.queryKey[0].split("/")[2]),
  });
}
