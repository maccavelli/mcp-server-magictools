package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// pipelineArgs is the strict-decoded argument set of execute_pipeline. Pointer
// fields distinguish "absent" from "zero" so the gate can refuse a reject:false
// or dry_run:false that rides with the wrong mode.
type pipelineArgs struct {
	SessionID   string   `json:"session_id"`
	Target      string   `json:"target"`
	Intent      string   `json:"intent"`
	PlanHash    string   `json:"plan_hash"`
	DryRun      *bool    `json:"dry_run"`
	TargetRoles []string `json:"target_roles"`
	Reject      *bool    `json:"reject"`
}

// pipelineGateOutcome is the pure argument gate's classification (0013 P4).
// mode is one of "fresh", "dry_run", "continue", "reject", "refuse"; reason
// carries the refusal vocabulary word when mode is "refuse".
type pipelineGateOutcome struct {
	mode   string
	reason string
	args   pipelineArgs
}

func refuse(reason string) pipelineGateOutcome {
	return pipelineGateOutcome{mode: "refuse", reason: reason}
}

// classifyPipelineArgs is the argument gate as a pure function: strict decode
// (unknown key or wrong type → bad_arguments), then the mode rules of 0005's
// D2 contract. The contract table drives it row for row.
func classifyPipelineArgs(raw json.RawMessage) pipelineGateOutcome {
	var args pipelineArgs
	if len(bytes.TrimSpace(raw)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&args); err != nil {
			return refuse("bad_arguments")
		}
	}

	hasSession := args.SessionID != ""
	hasTarget := args.Target != ""
	hasIntent := args.Intent != ""
	hasFresh := hasTarget || hasIntent
	rejectSet := args.Reject != nil
	rejectTrue := args.Reject != nil && *args.Reject
	dryRunTrue := args.DryRun != nil && *args.DryRun

	// reject is decided first, before any executor is in reach (0005's D2).
	if rejectSet {
		if rejectTrue {
			// reject:true with fresh-call arguments is bad_arguments whether or
			// not a session_id rides along; without session_id it is
			// reject_needs_session.
			if hasFresh || args.DryRun != nil || len(args.TargetRoles) > 0 {
				return refuse("bad_arguments")
			}
			if !hasSession {
				return refuse("reject_needs_session")
			}
			return pipelineGateOutcome{mode: "reject", args: args}
		}
		// reject:false rides with a mode only when session_id is present and
		// no fresh argument does; alone or with fresh arguments it is refused.
		if hasSession && !hasFresh && args.DryRun == nil && len(args.TargetRoles) == 0 {
			return pipelineGateOutcome{mode: "continue", args: args}
		}
		return refuse("bad_arguments")
	}

	if hasSession && !hasFresh {
		// Continuation: session_id alone, optionally plan_hash. Nothing else.
		if args.DryRun != nil || len(args.TargetRoles) > 0 {
			return refuse("bad_arguments")
		}
		return pipelineGateOutcome{mode: "continue", args: args}
	}

	if hasTarget && hasIntent {
		if dryRunTrue {
			return pipelineGateOutcome{mode: "dry_run", args: args}
		}
		return pipelineGateOutcome{mode: "fresh", args: args}
	}

	// Partial fresh arguments, or nothing at all.
	return refuse("bad_arguments")
}

// pipelineRefusalText renders a refusal as the tool result text. Each word of
// the vocabulary is distinct and never a fresh run.
func pipelineRefusalText(reason string, args pipelineArgs) string {
	switch reason {
	case "reject_needs_session":
		return "execute_pipeline: reject_needs_session — reject: true requires session_id and no fresh-run arguments."
	case "bad_arguments":
		return "execute_pipeline: bad_arguments — the argument set matches no mode. Fresh: target + intent (session_id, plan_hash, dry_run, target_roles optional). Continue: session_id alone (plan_hash optional). Reject: session_id + reject: true. Unknown keys and wrong types are refused."
	default:
		return fmt.Sprintf("execute_pipeline: %s", reason)
	}
}

// pipelineSessionRefusalText maps a store refusal to its wire message.
func pipelineSessionRefusalText(reason string, artifactURIs []string) string {
	var sb strings.Builder
	sb.WriteString("execute_pipeline: ")
	sb.WriteString(reason)
	switch reason {
	case "unknown_session":
		sb.WriteString(" — no stored session with this id (it may have expired past its grace, or the server crashed before the write synced).")
	case "expired_session":
		sb.WriteString(" — the session is past its 24-hour validity; start a new dry run.")
	case "session_running":
		sb.WriteString(" — the session is already claimed by a running continuation; a repeated continuation never re-runs.")
	case "session_already_run":
		sb.WriteString(" — the session completed; a repeated continuation never re-runs.")
		if len(artifactURIs) > 0 {
			sb.WriteString(" Artifacts: ")
			sb.WriteString(strings.Join(artifactURIs, ", "))
		}
	case "session_failed":
		sb.WriteString(" — the session's run failed; start a new dry run.")
	case "session_rejected":
		sb.WriteString(" — the session was rejected; nothing runs.")
	case "session_interrupted":
		sb.WriteString(" — the server restarted while this session ran; nothing is resumed. Start a new dry run.")
	case "plan_hash_mismatch":
		sb.WriteString(" — the supplied plan_hash does not equal the stored one; nothing ran.")
	case "plan_stale":
		sb.WriteString(" — a stored stage's tool no longer resolves; nothing ran.")
	}
	return sb.String()
}
