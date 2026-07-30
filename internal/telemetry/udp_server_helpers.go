package telemetry

import (
	"encoding/json"
	"strings"
	"time"
)

func (s *UDPServer) runPingReceiver() {
	buf := make([]byte, 64)
	for {
		_, remoteAddr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed") {
				return
			}
			select {
			case <-s.cancelChan:
				return
			default:
				continue
			}
		}
		s.clientsMu.Lock()
		if _, exists := s.clients[remoteAddr.String()]; !exists {
			s.ReconnectCount.Add(1)
		}
		s.clients[remoteAddr.String()] = &udpClient{addr: remoteAddr, lastSeen: time.Now()}
		s.clientsMu.Unlock()
		ack := []byte(`{"pipeline_stage":"ACK"}`)
		writeUDPOrWarn(s.conn, ack, remoteAddr)
	}
}

func (s *UDPServer) runSnapshotBroadcaster(snapshotFunc func() map[string]any) {
	ticker := time.NewTicker(EmissionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.cancelChan:
			return
		case <-ticker.C:
			s.broadcastSnapshot(snapshotFunc)
		}
	}
}

func (s *UDPServer) broadcastSnapshot(snapshotFunc func() map[string]any) {
	if snapshotFunc == nil {
		return
	}
	s.clientsMu.RLock()
	if len(s.clients) == 0 {
		s.clientsMu.RUnlock()
		return
	}
	s.clientsMu.RUnlock()

	snapshot := snapshotFunc()
	if snapshot == nil {
		return
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	if len(payload) > BudgetBytes {
		payload = trimPayload(snapshot)
	}
	s.deliverPayload(payload)
}

func (s *UDPServer) deliverPayload(payload []byte) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	now := time.Now()
	for key, c := range s.clients {
		if now.Sub(c.lastSeen) > clientStalenessThreshold {
			delete(s.clients, key)
		}
	}
	for key, c := range s.clients {
		if _, err := s.conn.WriteToUDP(payload, c.addr); err != nil {
			delete(s.clients, key)
			s.PacketsDropped.Add(1)
		} else {
			s.PacketsSent.Add(1)
		}
	}
}
