#!/usr/bin/env bash
set -euo pipefail

readonly max_lines=300
failed=0

mapfile -d '' files < <(find cmd internal tests -type f -name '*.go' -print0 | sort -z)
for file in "${files[@]}"; do
  if grep -q '^// Code generated .* DO NOT EDIT\.$' "$file"; then
    continue
  fi
  lines="$(wc -l < "$file")"
  if (( lines > max_lines )); then
    printf '%s has %d lines; maximum is %d\n' "$file" "$lines" "$max_lines" >&2
    failed=1
  fi
done

exit "$failed"
