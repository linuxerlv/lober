// Package builtin owns the content shipped with lober (spec §2.8).
//
// IDs are permanent, valid model IDs prefixed with "builtin-". Names start with "lober:"
// and are fixed for the identity's lifetime: renaming requires a new ID. Templates may
// only refer to versions of hooks in the shipped builtin manifest. All versions are
// approved and read-only; startup loading belongs to the composition layer.
//
// Content changes ship as higher version numbers with a lober release. Each ID/version
// has an immutable serialization fingerprint and a full historical release fixture.
// Never regenerate an old golden to accept changed content. Add a new version's golden
// and retain old release fixtures so upgrades can replay every release into one repository.
// Task 5 establishes a minimal kickstart skeleton; other formats and hook implementations
// will be added separately, with version increments for any existing content changes.
//
// Production code depends only on core and the allowed standard library. Only tests
// import pgstore/pgtest, because EnsureBuiltin* deliberately does not run activation,
// and historical upgrade/idempotence checks need PostgreSQL's real immutable identities.
package builtin
