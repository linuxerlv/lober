// Package render turns frozen snapshots and request system values into file
// bytes. It is pure: no database, clock, file, or network access. Identical input
// produces identical bytes; maps are looked up by reference, never iterated for
// output ordering.
//
// Rendering relies on five invariants supplied by snapshot.Build:
//  1. Execution slots contain only scripts with digests.
//  2. Fragment slots contain only fragments.
//  3. Family and Format are nonempty.
//  4. Execution timings are supported by the snapshot's format.
//  5. Body slot names correspond one-to-one to Slots, without duplicates.
//
// It expands only body-to-slot, without looking up hooks or implementations.
// Script parameters use model.EnvName and SnapshotValue.EnvValue in the script
// assembler; hook script bodies themselves are never placeholder-expanded.
package render
