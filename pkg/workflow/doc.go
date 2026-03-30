// Package workflow implements the MOCA workflow engine.
//
// Workflows define state machines over documents with transitions, SLA timers,
// and approval chains. A document's workflow state is stored alongside the
// document and transitions are enforced by this package.
//
// Key components:
//   - Engine: state machine with transition guards and action hooks
//   - SLA: deadline tracking and escalation triggers
//   - Approval: multi-level approval chains with delegation
package workflow
