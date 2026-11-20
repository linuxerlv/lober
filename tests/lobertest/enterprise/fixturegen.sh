#!/usr/bin/env bash
set -euo pipefail
# Test-only synthetic media, NEVER an operating-system installation qualification.
# Reuse the product's existing ISO fixture encoder instead of forging a ready row.
root=$(cd "$(dirname "$0")/../../.." && pwd)
mkdir -p "$root/.tmp"
scratch=$(mktemp -d "$root/.tmp/enterprise-fixture-XXXXXX")
cat > "$scratch/main.go" <<'GO'
package main
import (
  "os"
  "github.com/lober/lober/internal/infrastructure/media/isotest"
)
func main() {
  files := map[string][]byte{
    ".treeinfo": []byte("[general]\nname=Rocky Linux\nversion=9.6\narch=x86_64\npackagedir=media/Packages\n"),
    "images/pxeboot/vmlinuz": []byte("TEST-ONLY kernel"),
    "images/pxeboot/initrd.img": []byte("TEST-ONLY initrd"),
    "images/install.img": []byte("TEST-ONLY stage2"),
    "media/BaseOS/repodata/repomd.xml": []byte("TEST-ONLY base"),
    "media/AppStream/repodata/repomd.xml": []byte("TEST-ONLY app"),
    "media/Packages/sample.rpm": []byte("TEST-ONLY package"),
  }
  if err := os.WriteFile(os.Args[1], isotest.Image(files), 0600); err != nil { panic(err) }
}
GO
go run "$scratch/main.go" "$1"
