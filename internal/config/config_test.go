package config

import "testing"

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
