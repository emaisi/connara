import { PageErrorBoundary } from "./error-boundary";
import { InvitationPage } from "./pages/invitation";
import { lazy, Suspense } from "react";
import { Navigate, Route, Routes } from "react-router";
import { DemoProvider } from "./demo";
import { Shell } from "./shell";

const AuthMethodsPage = lazy(() =>
  import("./pages/auth-methods").then((module) => ({ default: module.AuthMethodsPage })),
);
const DemoActionsPage = lazy(() => import("./pages/actions").then((module) => ({ default: module.DemoActionsPage })));
const DemoConnectionsPage = lazy(() =>
  import("./pages/connections").then((module) => ({ default: module.DemoConnectionsPage })),
);
const DemoIntegrationsPage = lazy(() =>
  import("./pages/integrations").then((module) => ({ default: module.DemoIntegrationsPage })),
);
const DemoOverviewPage = lazy(() =>
  import("./pages/overview").then((module) => ({ default: module.DemoOverviewPage })),
);
const DemoProvidersPage = lazy(() =>
  import("./pages/systems").then((module) => ({ default: module.DemoProvidersPage })),
);
const GettingStartedPage = lazy(() =>
  import("./pages/getting-started").then((module) => ({ default: module.GettingStartedPage })),
);
const SyncTasksPage = lazy(() => import("./pages/sync-tasks").then((module) => ({ default: module.SyncTasksPage })));
const OperationsPage = lazy(() =>
  import("./pages/demo-operations").then((module) => ({ default: module.OperationsPage })),
);
const WebhooksPage = lazy(() => import("./pages/demo-operations").then((module) => ({ default: module.WebhooksPage })));
const AuditPage = lazy(() => import("./pages/demo-platform").then((module) => ({ default: module.AuditPage })));
const DemoAccessPage = lazy(() =>
  import("./pages/demo-platform").then((module) => ({ default: module.DemoAccessPage })),
);
const PlatformSettingsPage = lazy(() =>
  import("./pages/demo-platform").then((module) => ({ default: module.PlatformSettingsPage })),
);
const ResourcesPage = lazy(() => import("./pages/demo-platform").then((module) => ({ default: module.ResourcesPage })));
const TeamPage = lazy(() => import("./pages/demo-platform").then((module) => ({ default: module.TeamPage })));
export function App() {
  return (
    <PageErrorBoundary>
      <DemoProvider>
        <Suspense fallback={<p role="status">正在加载页面…</p>}>
          <Routes>
            <Route path="invitation" element={<InvitationPage />} />
            <Route element={<Shell />}>
              <Route index element={<DemoOverviewPage />} />
              <Route path="getting-started" element={<GettingStartedPage />} />
              <Route path="providers" element={<DemoProvidersPage />} />
              <Route path="auth" element={<AuthMethodsPage />} />
              <Route path="auth-methods" element={<Navigate to="/auth" replace />} />
              <Route path="integrations" element={<DemoIntegrationsPage />} />
              <Route path="connections" element={<DemoConnectionsPage />} />
              <Route path="oauth-apps" element={<Navigate to="/auth" replace />} />
              <Route path="actions" element={<DemoActionsPage />} />
              <Route path="sync" element={<SyncTasksPage />} />
              <Route path="functions" element={<Navigate to="/sync" replace />} />
              <Route path="webhooks" element={<WebhooksPage />} />
              <Route path="access" element={<DemoAccessPage />} />
              <Route path="operations" element={<OperationsPage />} />
              <Route path="metrics" element={<Navigate to="/operations?view=metrics" replace />} />
              <Route path="audit" element={<AuditPage />} />
              <Route path="agent" element={<Navigate to="/resources" replace />} />
              <Route path="files" element={<Navigate to="/resources" replace />} />
              <Route path="marketplace" element={<Navigate to="/providers" replace />} />
              <Route path="resources" element={<ResourcesPage />} />
              <Route path="settings" element={<PlatformSettingsPage />} />
              <Route path="team" element={<TeamPage />} />
              <Route path="billing" element={<Navigate to="/operations?view=metrics" replace />} />
              <Route path="profile" element={<Navigate to="/settings" replace />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Route>
          </Routes>
        </Suspense>
      </DemoProvider>
    </PageErrorBoundary>
  );
}
