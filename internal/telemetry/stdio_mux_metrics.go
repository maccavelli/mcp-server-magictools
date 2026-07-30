package telemetry

import "sync/atomic"

// StdioMuxMetricsCounters tracks the stdio↔HTTP proxy multiplexer's internal state.
// TEL-1: All counters use atomic.Int64 for zero-contention increments on the hot path.
var StdioMux StdioMuxMetricsCounters

// StdioMuxMetricsCounters defines the proxy multiplexer telemetry surface.
type StdioMuxMetricsCounters struct {
	// InboundMessages counts total messages received from IDE stdin.
	InboundMessages atomic.Int64
	// OutboundMessages counts total messages routed back to IDE.
	OutboundMessages atomic.Int64
	// OutboundDrops counts payloads dropped due to channel saturation or byte quota.
	OutboundDrops atomic.Int64
	// SSEStreamsActive tracks currently open SSE response streams (gauge).
	SSEStreamsActive atomic.Int64
	// SSEEventsProcessed counts total SSE data: events processed.
	SSEEventsProcessed atomic.Int64
	// RelayErrors counts failed HTTP relays.
	RelayErrors atomic.Int64
	// Relay401Heals counts successful token re-negotiations.
	Relay401Heals atomic.Int64
	// WriteErrors counts failed stdout writes.
	WriteErrors atomic.Int64
	// JSONMarshalErrors counts failed response marshals in OutboundFilter.
	JSONMarshalErrors atomic.Int64
	// UnmarshalErrors counts failed response unmarshals (truncated SSE).
	UnmarshalErrors atomic.Int64
	// NotificationsBroadcast counts notification messages fanned out to clients.
	NotificationsBroadcast atomic.Int64
	// NotificationsDroppedQoS counts notifications dropped by the QoS high-water mark.
	NotificationsDroppedQoS atomic.Int64
	// ByteQuotaDrops counts payloads dropped due to per-client byte quota breach.
	ByteQuotaDrops atomic.Int64
	// FirewallValid counts JSON-RPC frames that passed structural validation.
	FirewallValid atomic.Int64
	// FirewallDropped counts JSON-RPC frames rejected by the stdio firewall.
	FirewallDropped atomic.Int64
}

// Snapshot returns a map representation of the current multiplexer metrics
// for inclusion in the telemetry WriteSnapshot.
func (m *StdioMuxMetricsCounters) Snapshot() map[string]any {
	return map[string]any{
		"inbound_messages":          m.InboundMessages.Load(),
		"outbound_messages":         m.OutboundMessages.Load(),
		"outbound_drops":            m.OutboundDrops.Load(),
		"sse_streams_active":        m.SSEStreamsActive.Load(),
		"sse_events_processed":      m.SSEEventsProcessed.Load(),
		"relay_errors":              m.RelayErrors.Load(),
		"relay_401_heals":           m.Relay401Heals.Load(),
		"write_errors":              m.WriteErrors.Load(),
		"json_marshal_errors":       m.JSONMarshalErrors.Load(),
		"unmarshal_errors":          m.UnmarshalErrors.Load(),
		"notifications_broadcast":   m.NotificationsBroadcast.Load(),
		"notifications_dropped_qos": m.NotificationsDroppedQoS.Load(),
		"byte_quota_drops":          m.ByteQuotaDrops.Load(),
		"firewall_valid":            m.FirewallValid.Load(),
		"firewall_dropped":          m.FirewallDropped.Load(),
	}
}
