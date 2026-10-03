package config

import "testing"

func loadWithRequiredEnv(t *testing.T) (Config, error) {
	t.Helper()
	t.Setenv("APIHUB_DATABASE_URL", "postgres://user:pass@127.0.0.1:5432/apihub")
	t.Setenv("APIHUB_REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("APIHUB_ENCRYPTION_KEY", "AAAA")
	return Load()
}

func TestParseCIDRs(t *testing.T) {
	prefixes, err := parseCIDRs("10.20.0.0/16, 192.168.50.10/32")
	if err != nil {
		t.Fatal(err)
	}
	if len(prefixes) != 2 || prefixes[0].String() != "10.20.0.0/16" {
		t.Fatalf("unexpected prefixes: %v", prefixes)
	}
}

func TestParseCIDRsRejectsInvalidValue(t *testing.T) {
	if _, err := parseCIDRs("private-network"); err == nil {
		t.Fatal("expected invalid CIDR to be rejected")
	}
}

func TestDatabasePoolDefaultsAreRoleAware(t *testing.T) {
	for _, test := range []struct {
		role     string
		workers  int
		max, min int
	}{{"all", 4, 24, 2}, {"api", 4, 16, 2}, {"worker", 4, 12, 1}, {"scheduler", 4, 4, 1}} {
		maximum, minimum := databasePoolDefaults(test.role, test.workers)
		if maximum != test.max || minimum != test.min {
			t.Fatalf("databasePoolDefaults(%q)=(%d,%d), want (%d,%d)", test.role, maximum, minimum, test.max, test.min)
		}
	}
}

func TestIntegerRejectsOutOfRangeEnvironmentValue(t *testing.T) {
	t.Setenv("APIHUB_TEST_INTEGER", "65")
	if _, err := integer("APIHUB_TEST_INTEGER", 1, 0, 64); err == nil {
		t.Fatal("expected out-of-range value to fail")
	}
}

func TestWorkflowWorkersEnvironment(t *testing.T) {
	t.Setenv("APIHUB_WORKFLOW_WORKERS", "5")
	settings, err := loadWithRequiredEnv(t)
	if err != nil {
		t.Fatal(err)
	}
	if settings.WorkflowWorkers != 5 {
		t.Fatalf("APIHUB_WORKFLOW_WORKERS=5 parsed as %d", settings.WorkflowWorkers)
	}
}

func TestWorkflowWorkersDefaultsToTwo(t *testing.T) {
	t.Setenv("APIHUB_WORKFLOW_WORKERS", "")
	settings, err := loadWithRequiredEnv(t)
	if err != nil {
		t.Fatal(err)
	}
	if settings.WorkflowWorkers != 2 {
		t.Fatalf("default workflow workers = %d, want 2", settings.WorkflowWorkers)
	}
}
