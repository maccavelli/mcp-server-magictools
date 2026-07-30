package telemetry

import (
	"encoding/json"
	"log/slog"
	"maps"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// TelemetryPorts are the UDP ports used for dashboard telemetry (serve listens, dashboard connects).
	DefaultTelemetryPorts = []int{49166, 49167, 49168, 49169, 49170}
	// EmissionInterval controls how frequently the serve process pushes metrics to the dashboard.
	EmissionInterval = 500 * time.Millisecond
	// BudgetBytes is the maximum datagram size for priority trimming.
	BudgetBytes = 8192
	// GlobalUDPServer holds the active UDP server instance for dashboard telemetry reading.
	GlobalUDPServer *UDPServer
)

// udpClient tracks a dashboard client with its last-seen timestamp for staleness reaping.
type udpClient struct {
	addr     *net.UDPAddr
	lastSeen time.Time
}

// UDPServer handles the UDP broadcast of telemetry data to connected dashboards.
type UDPServer struct {
	conn       *net.UDPConn
	boundPort  int
	clients    map[string]*udpClient
	clientsMu  sync.RWMutex
	cancelChan chan struct{}

	// Telemetry counters exposed for the Network Status dashboard tab
	PacketsSent    atomic.Int64
	PacketsDropped atomic.Int64
	ReconnectCount atomic.Int64
}

// clientStalenessThreshold defines how long a client can be silent before being reaped.
const clientStalenessThreshold = 30 * time.Second

// NewUDPServer initializes the UDP listener on the first available port in the range.
func NewUDPServer() (*UDPServer, error) {
	var conn *net.UDPConn
	var boundPort int
	var lastErr error

	for _, port := range GetTelemetryPorts() {
		addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}
		c, err := net.ListenUDP("udp", addr)
		if err == nil {
			conn = c
			boundPort = port
			slog.Info("telemetry udp listener bound", "port", port)
			break
		}
		lastErr = err
		slog.Warn("telemetry port unavailable", "port", port, "error", err)
	}

	if conn == nil {
		slog.Warn("all telemetry ports exhausted; starting without dashboard emission")
		return nil, lastErr
	}

	return &UDPServer{
		conn:       conn,
		boundPort:  boundPort,
		clients:    make(map[string]*udpClient),
		cancelChan: make(chan struct{}),
	}, nil
}

// BoundPort returns the port the server is listening on, or 0 if not bound.
func (s *UDPServer) BoundPort() int {
	if s == nil {
		return 0
	}
	return s.boundPort
}

// ActiveClients returns the count of registered dashboard clients.
func (s *UDPServer) ActiveClients() int {
	if s == nil {
		return 0
	}
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	return len(s.clients)
}

// Start begins listening for dashboard pings and broadcasting snapshots.
func (s *UDPServer) Start(snapshotFunc func() map[string]any) {
	if s == nil || s.conn == nil {
		return
	}
	go s.runPingReceiver()
	go s.runSnapshotBroadcaster(snapshotFunc)
}

// Stop gracefully shuts down the UDP listener and broadcast loop.
func (s *UDPServer) Stop() {
	if s == nil {
		return
	}
	close(s.cancelChan)
	if s.conn != nil {
		closeConnOrWarn(s.conn)
	}
}

// trimPayload reduces the snapshot size by removing low-priority keys in order.
// It clones the map first to avoid mutating the caller's shared snapshot.
func trimPayload(snapshot map[string]any) []byte {
	// Clone the map to prevent permanent key deletion from the live snapshot
	clone := make(map[string]any, len(snapshot))
	maps.Copy(clone, snapshot)

	trimOrder := []string{
		"recent_errors",
		"databases_history",
		"scoring_factors",
		"volatility_index",
	}

	for _, key := range trimOrder {
		delete(clone, key)
		b, err := json.Marshal(clone)
		if err == nil && len(b) <= BudgetBytes {
			return b
		}
	}

	b, err := json.Marshal(clone)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// GetTelemetryPorts parses the environment variable or falls back to the server's specific default ports.
func GetTelemetryPorts() []int {
	env := os.Getenv("MCP_TELEMETRY_UDP_PORTS")
	if env == "" {
		return DefaultTelemetryPorts
	}

	var ports []int
	for part := range strings.SplitSeq(env, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			bounds := strings.Split(part, "-")
			if len(bounds) == 2 {
				start, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
				end, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
				if err1 == nil && err2 == nil && start <= end {
					for i := start; i <= end; i++ {
						ports = append(ports, i)
					}
				}
			}
		} else {
			if port, err := strconv.Atoi(part); err == nil {
				ports = append(ports, port)
			}
		}
	}

	if len(ports) == 0 {
		return DefaultTelemetryPorts // Fallback on malformed environment variable
	}
	return ports
}
