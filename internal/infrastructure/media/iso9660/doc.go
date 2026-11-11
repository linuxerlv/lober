// Package iso9660 exposes ISO 9660 images as read-only filesystems. It supports
// primary identifiers, Joliet UCS-2 names and Rock Ridge names, permissions and
// symbolic links. The reader remains owned by the caller. All image references
// are bounds checked, and directory records are read without buffering an
// entire directory. No host paths are accessed when following symbolic links.
package iso9660
