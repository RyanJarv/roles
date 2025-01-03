#!/usr/bin/env bash

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 <json_file>"
    exit 1
fi

json_file=$1

set -euo pipefail

result="$(
  cat "$json_file" \
    | jq '.[]|to_entries|.[]|{key: .key, value: .value.Exists}' \
    | jq -s 'from_entries'
)"

mv "$json_file" "$json_file.bak"

echo "$result" > "$json_file"

echo "Updated $json_file"