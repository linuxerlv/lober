// Package iso acquires installation media and imports verified artifacts. The
// assembly point injects all directories and runs blocking FetchOne and Import
// calls in supervised workers. This package never starts a goroutine.
//
// Local media are read in place; the operator-owned original is never copied or
// deleted (the binding system specification). Uploads and downloads publish
// staging files only after complete-byte
// and digest validation. Import builds temporary directories on the artifact
// filesystem before renaming, so staging and artifacts may be on different disks.
// Recover only returns work: ResumeFetch for uploads means await more chunks,
// not call FetchOne. Progress contains the missing upload intervals.
// The assembly point shares one Sources instance for each staging directory:
// PostgreSQL revisions coordinate state, while instance locks serialize file
// access. Concurrent processes must not share a staging directory.
package iso
