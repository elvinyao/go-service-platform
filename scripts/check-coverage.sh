#!/bin/sh

set -eu

minimum=${COVERAGE_MIN:-90}
if ! awk -v value="$minimum" 'BEGIN { exit !(value ~ /^[0-9]+([.][0-9]+)?$/) }'; then
  echo "COVERAGE_MIN must be a non-negative number" >&2
  exit 2
fi

packages=$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)
if [ -z "$packages" ]; then
  echo "No packages with tests found"
  exit 0
fi

if ! output=$(go test -cover $packages 2>&1); then
  printf '%s\n' "$output"
  exit 1
fi
printf '%s\n' "$output"

failures=$(printf '%s\n' "$output" | awk -v minimum="$minimum" '
  /coverage:/ {
    for (i = 1; i <= NF; i++) {
      if ($i == "coverage:") {
        value = $(i + 1)
        sub(/%$/, "", value)
        if ((value + 0) < (minimum + 0)) {
          printf "%s: %s%%\n", $2, value
        }
      }
    }
  }
')

if [ -n "$failures" ]; then
  printf '\nCoverage below %s%%:\n%s\n' "$minimum" "$failures" >&2
  exit 1
fi

printf '\nAll packages with tests meet the %s%% coverage threshold.\n' "$minimum"
