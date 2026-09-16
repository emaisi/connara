package rediscache

import (
	"context"
	"net"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestClientRecoversAfterStartupOutage(t *testing.T) {
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	address := listener.Addr().String()
	listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	cache, err := Open(ctx, address, "", 0)
	cancel()
	if err == nil || cache == nil {
		t.Fatalf("startup outage must keep client: %v", err)
	}
	defer cache.Close()
	command := exec.Command(binary, "--bind", "127.0.0.1", "--port", strconv.Itoa(port), "--save", "", "--appendonly", "no", "--dir", t.TempDir())
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { command.Process.Kill(); command.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		err := cache.Ready(ctx)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("original cache did not recover")
}
