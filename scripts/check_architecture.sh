#!/usr/bin/env sh
set -eu

for directory in \
  internal/domain \
  internal/delivery/http \
  internal/service \
  internal/repository/postgres \
  internal/infrastructure/database; do
  if [ ! -d "$directory" ]; then
    echo "Missing architecture boundary: $directory" >&2
    exit 1
  fi
done

if find internal/infrastructure/database -maxdepth 1 -type f \
  \( -name '*repository*.go' -o -name '*service*.go' -o -name '*handler*.go' \) |
  grep -q .; then
  echo "Database infrastructure may contain connection code only" >&2
  exit 1
fi

if grep -R -E '/internal/(delivery|infrastructure)/' internal/service \
  --include='*.go' --exclude='*_test.go' -q; then
  echo "Business services must not depend on delivery or infrastructure" >&2
  exit 1
fi

echo "Ecommerce architecture contract: PASS"
