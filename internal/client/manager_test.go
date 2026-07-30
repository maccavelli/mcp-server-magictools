package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLRUEviction(t *testing.T) {
	m := NewWarmRegistry(t.TempDir(), nil, &config.Config{})

	// 1. Spawning 52 servers (limit 50)
	for i := 1; i <= 52; i++ {
		name := fmt.Sprintf("Server-%02d", i)
		srv := m.initSubServer(name)
		srv.LastUsed = time.Now().Add(time.Duration(i) * time.Second) // sorted by i
		srv.Session = &mcp.ClientSession{}                            // mock non-nil session to make it 'active' for LRU calc
		m.mu.Lock()
		m.Servers[name] = srv
		m.mu.Unlock()
	}

	// 2. Call EvictLRU("Server-52") - should NOT evict 52
	m.EvictLRU("Server-52")

	// 3. Count remaining
	m.mu.RLock()
	defer m.mu.RUnlock()

	activeCount := 0
	for _, s := range m.Servers {
		if s.Session != nil {
			activeCount++
		}
	}

	if activeCount != 51 {
		t.Errorf("expected 51 active servers after one eviction, got %d", activeCount)
	}

	if _, ok := m.Servers["Server-01"]; ok {
		t.Error("oldest server Server-01 should have been evicted")
	}
}

func TestCircuitBreaker(t *testing.T) {
	m := NewWarmRegistry(t.TempDir(), nil, &config.Config{})
	name := "failing-server"
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	m.mu.Lock()
	m.Servers[name] = m.initSubServer(name)
	m.mu.Unlock()

	for range CircuitBreakerThreshold {
		m.markFailure(name)
	}

	err := m.Connect(ctx, name, "/bin/false", nil, nil, "")
	if err == nil || (!strings.Contains(err.Error(), "circuit breaker") && !strings.Contains(err.Error(), "cooldown")) {
		t.Errorf("expected circuit breaker error, got %v", err)
	}

	m.resetFailure(name)
	err = m.Connect(ctx, name, "/bin/false", nil, nil, "")
	if err != nil && (strings.Contains(err.Error(), "circuit breaker") || strings.Contains(err.Error(), "cooldown")) {
		t.Error("circuit breaker should have been cleared by resetFailure")
	}
}

func TestGetStatusReport(t *testing.T) {
	m := NewWarmRegistry(t.TempDir(), nil, &config.Config{})
	m.mu.Lock()
	srv := m.initSubServer("active")
	srv.StartTime = time.Now().Add(-1 * time.Hour)
	srv.Session = &mcp.ClientSession{} // active
	srv.TotalCalls = 42
	m.Servers["active"] = srv
	m.mu.Unlock()

	report := m.GetStatusReport([]string{"active", "offline"})
	if len(report) != 2 {
		t.Fatal("expected 2 status entries")
	}

	for _, s := range report {
		switch s.Name {
		case "active":
			if !s.Running || s.TotalCalls != 42 {
				t.Errorf("incorrect active status: %+v", s)
			}
		case "offline":
			if s.Running {
				t.Error("offline server should not be running")
			}
		}
	}
}
func TestStdinBridgePersistence(t *testing.T) {
	m := NewWarmRegistry(t.TempDir(), nil, &config.Config{})

	subStdinR, subStdinW := io.Pipe()

	clientStdinR, clientStdinW := io.Pipe()

	m.setupIOBridges(context.Background(), "test-bridge", subStdinW, clientStdinR)

	testMsg := []byte("hello bridge")
	errChan := make(chan error, 1)
	go func() {
		_, err := clientStdinW.Write(testMsg)
		errChan <- err
	}()

	readBuf := make([]byte, len(testMsg))
	_, err := io.ReadFull(subStdinR, readBuf)
	if err != nil {
		t.Fatalf("Failed to read from sub-server stdin: %v", err)
	}

	if string(readBuf) != string(testMsg) {
		t.Errorf("Expected %q, got %q", string(testMsg), string(readBuf))
	}

	if err := <-errChan; err != nil {
		t.Errorf("Client write failed: %v", err)
	}
}

func TestConnectDeadlock(t *testing.T) {
	m := NewWarmRegistry(t.TempDir(), nil, &config.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	name := "deadlock-test"
	m.mu.Lock()
	s := m.initSubServer(name)
	// Set to Starting so Connect takes the "already starting, join wait" path.
	// Pre-exhaust ActorStartedOnce to prevent a real actor loop.
	s.Status = StatusStarting
	s.ActorStartedOnce.Do(func() {})
	readyChan := s.ReadyChan
	m.Servers[name] = s
	m.mu.Unlock()

	// 1. Trigger Connect in a goroutine — it should block waiting on ReadyChan
	done := make(chan error, 1)
	go func() {
		done <- m.Connect(ctx, name, "echo", nil, nil, "")
	}()

	// Give Connect time to reach the wait state
	time.Sleep(200 * time.Millisecond)

	// 2. Signal readiness by closing ReadyChan — simulates actor signaling done
	m.mu.Lock()
	s.Status = StatusHealthy
	m.mu.Unlock()
	close(readyChan)

	// 3. VERIFY: Connect returns success
	if err := <-done; err != nil {
		t.Errorf("Connect failed despite health signal: %v", err)
	}
}

func TestStdinBridgeCorruption(t *testing.T) {
	m := NewWarmRegistry(t.TempDir(), nil, &config.Config{})

	subStdinR, subStdinW := io.Pipe()
	clientStdinR, clientStdinW := io.Pipe()

	m.setupIOBridges(context.Background(), "corruption-test", subStdinW, clientStdinR)

	part1 := "{\"meth\""
	part2 := ":1}"

	go func() {
		clientStdinW.Write([]byte(part1))
		time.Sleep(100 * time.Millisecond) // Ensure bridge reads part 1 separately
		clientStdinW.Write([]byte(part2))
		clientStdinW.Close()
	}()

	// Read all from subStdinR
	data, _ := io.ReadAll(subStdinR)

	if strings.Contains(string(data), "meth\"\n") {
		t.Errorf("CORRUPTION DETECTED: Bridge injected newline into partial JSON chunk: %q", string(data))
	}

	var msg map[string]any
	// Expect valid JSON (maybe with newline at end if we decide to keep that, but NOT in middle)
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Errorf("CORRUPTION DETECTED: Resulting JSON is invalid: %v (data: %q)", err, string(data))
	}
}

func TestOrchestrateRestartNoRace(t *testing.T) {
	m := NewWarmRegistry(t.TempDir(), nil, &config.Config{})
	name := "restart-race-test"

	m.mu.Lock()
	s := m.initSubServer(name)
	s.Status = StatusHealthy
	s.Command = "echo"
	m.Servers[name] = s
	m.mu.Unlock()

	// Trigger restart
	m.orchestrateRestart(s)

	// Immediately check if it's still in the map
	m.mu.RLock()
	_, ok := m.Servers[name]
	m.mu.RUnlock()

	if !ok {
		t.Error("Server was deleted from map during restart, creating a race window!")
	}
}

func TestReconcileReadySignal(t *testing.T) {
	m := NewWarmRegistry(t.TempDir(), nil, &config.Config{})
	name := "ready-signal-test"

	m.mu.Lock()
	s := m.initSubServer(name)
	// Block the real actor loop during this unit test to avoid races
	s.ActorStartedOnce.Do(func() {})
	s.DesiredState = StatusHealthy
	readyChan := s.ReadyChan
	m.Servers[name] = s
	m.mu.Unlock()

	// Start actor manually - NO, don't start it, just test reconcile
	// m.EnsureActorRunning(s)

	// Simulate reconcile manually
	m.reconcile(context.Background(), s)

	// Check if ReadyChan is closed
	select {
	case <-readyChan:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("ReadyChan was not closed by reconcile")
	}
}
