// Package definition is the public VoltTest test-definition format.
//
// Parse reads a definition and returns a *Definition. The top-level
// schema_version selects the format. Every error Parse returns matches
// ErrInvalidDefinition.
//
// # v2
//
// A v2 definition ("schema_version": "2.0") is strict: every key must be
// defined for its schema_version with exact spelling and case
// (*UnknownFieldError), every value must have the right JSON type, and the
// structure is validated (*ValidationError).
//
// # v1
//
// A v1 definition (no schema_version, or 1, "1", "1.0" or null) is read the
// way the v1 readers read it: unknown keys are ignored, keys match
// case-insensitively, and v2-only keys (contract:"v2", such as
// target.base_url) are dropped unread. Values are checked for their JSON
// type only, with two classes of field:
//
//   - Core fields are decoded by every v1 reader (the headless CLI, the
//     cloud engine and the backend), all of which reject a value of the
//     wrong JSON type. Parse rejects it too, with a *ValidationError that
//     lists every such value by path.
//   - Loose fields (contract:"loose") are ignored by at least one v1
//     reader: the top-level description, weights, report_type,
//     region_config, cloud, api_key, cloud_timeout, existing_test_id and
//     skip_lookup, target.idle_timeout, a request's headers and a data
//     source's data_download_url. A value of the wrong JSON type does not
//     fail Parse: it is decoded as if the mistyped value were absent (with
//     a duplicate key, an earlier good value survives), and the problem is
//     recorded in Definition.V1TypeProblems, once per field with a Count.
//
// Each reader then fails on its own read set, as it does today. A reader
// declares the loose fields it reads as a ReadSet, with "[]" for every
// array level a pattern passes through and the pattern ending at a field
// name ("weights", not "weights[]"), and fails when V1TypeProblemsIn
// returns anything:
//
//	// Headless CLI: the CLI-only keys, description and target.idle_timeout.
//	var headlessReads = definition.MustReadSet("description", "weights",
//		"report_type", "region_config", "cloud", "api_key", "cloud_timeout",
//		"existing_test_id", "skip_lookup", "target.idle_timeout")
//
//	// Cloud engine: request headers and the dispatch download URL.
//	var cloudReads = definition.MustReadSet(
//		"scenarios[].steps[].request.headers",
//		"scenarios[].data_config.data_download_url")
//
//	if ps := def.V1TypeProblemsIn(headlessReads); len(ps) > 0 {
//		// reject the definition, as the headless engine does today
//	}
//
// A problem falls under a pattern when its path equals the pattern or
// continues it with "." or "[": "weights[1]" falls under "weights", and
// "weightsx" does not. Patterns stop at a map field (headers) and never
// name a literal index or a map key, because only the first mistyped value
// of each field keeps its path. NewReadSet rejects a pattern that breaks
// these rules (including a trailing "[]" or ".") or names an unknown or
// v2-only field; MustReadSet panics
// instead, so a bad package-level read set fails at init.
//
// # Dispatch-only fields
//
// data_config.data_download_url is set by the VoltTest backend when it
// sends a run to the engines, and counts as a data source in v2. The
// backend must strip any user-supplied value before calling Parse on a
// definition a user submits. The published JSON Schema leaves it out.
//
// # JSON Schema
//
// schema/test-definition.schema.json describes v2 definitions. It is
// necessary, not sufficient: every definition Parse accepts as v2 passes
// the schema (data_download_url aside), but Parse is stricter. It also
// checks durations, URL forms, unique names, parts of the load model, and
// threshold expressions and their scopes.
//
// # Thresholds (v2)
//
// The test, each scenario and each step can carry thresholds: expression
// strings such as "p95 < 500ms", or objects with abort settings. Parse
// checks each expression with thresholds.Parse and its scope with
// ValidFor; Bounds returns them with the scenario and step each applies
// to. v1 readers ignore thresholds, and so does Parse on the v1 path.
//
// # Load model (v2)
//
// Either non-empty stages, or virtual_users (at least 1) with optional
// duration and ramp_up. Without duration each virtual user runs its
// scenario once. A duration that is present must be greater than zero, and
// so must every stage duration.
//
// Outside the standard library, this package imports only the thresholds
// package of this module.
package definition
