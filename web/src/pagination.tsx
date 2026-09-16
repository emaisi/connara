import { useInfiniteQuery } from "@tanstack/react-query";
import { useDeferredValue, useMemo } from "react";
import { fetchPage } from "./api";
import { queryClient } from "./query";
import { Button, EmptyState, ErrorState } from "./ui";
export function useResourcePages(path: string, params: Record<string, string>, enabled = true) {
  const query = useDeferredValue(params.q ?? "");
  const filters = { ...params, q: query };
  const result = useInfiniteQuery(
    {
      queryKey: [path, filters],
      initialPageParam: "",
      enabled,
      queryFn: ({ pageParam }) => fetchPage(path, filters, pageParam),
      getNextPageParam: (page) => page.nextCursor || undefined,
    },
    queryClient,
  );
  const items = useMemo(() => result.data?.pages.flatMap((page) => page.items) ?? [], [result.data]);
  return { ...result, items };
}
export function PageControls({ page, onClear }: { page: ReturnType<typeof useResourcePages>; onClear?: () => void }) {
  return (
    <div>
      <div className="flex items-center gap-3 text-sm text-[var(--muted-text)]">
        <span>{page.isPending ? "正在加载…" : `已加载 ${page.items.length} 条`}</span>

        {page.hasNextPage && (
          <Button variant="secondary" disabled={page.isFetchingNextPage} onClick={() => void page.fetchNextPage()}>
            {page.isFetchingNextPage ? "正在加载…" : "加载更多"}
          </Button>
        )}
        <Button variant="ghost" disabled={page.isFetching} onClick={() => void page.refetch()}>
          刷新
        </Button>
      </div>
      {page.error ? (
        <ErrorState error={page.error} />
      ) : !page.isPending && !page.items.length ? (
        <EmptyState
          title="暂无结果"
          description="尚未配置数据或当前筛选没有匹配项。可创建对应资源、清除筛选或重新加载。"
          action={
            onClear ? (
              <Button variant="secondary" onClick={onClear}>
                清除筛选
              </Button>
            ) : (
              <Button variant="secondary" onClick={() => page.refetch()}>
                重新加载
              </Button>
            )
          }
        />
      ) : null}
    </div>
  );
}
