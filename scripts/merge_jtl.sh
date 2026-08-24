#!/usr/bin/env bash

# Usage : ./merge_jtl.sh -o result.jtl server1.jtl server2.jtl server3.jtl server4.jtl

if [ $# -lt 3 ]; then
  echo "Usage: $0 -o <output.jtl> <input1.jtl> <input2.jtl> ..."
  exit 1
fi

OUTPUT_FILE=""

# Parse -o option
if [ "$1" = "-o" ]; then
  OUTPUT_FILE="$2"
  shift 2
else
  echo "Error: -o option is required."
  echo "Usage: $0 -o <output.jtl> <input1.jtl> <input2.jtl> ..."
  exit 1
fi

if [ -z "$1" ]; then
  echo "Error: No input files provided."
  exit 1
fi

# Extract CSV header from the first file
FIRST_FILE="$1"
if [ ! -f "$FIRST_FILE" ]; then
  echo "Error: First file $FIRST_FILE not found."
  exit 1
fi

echo "Extracting header from $FIRST_FILE..."
head -n 1 "$FIRST_FILE" > "$OUTPUT_FILE"

# Extract data rows (excluding the first line) from all input files, sort by timeStamp, and merge
echo "Merging and sorting data rows from all files..."

for file in "$@"; do
  if [ ! -f "$file" ]; then
    echo "Warning: File $file not found. Skipping."
    continue
  fi
  # NR>1 excludes the first line (header)
  awk 'NR>1' "$file"
done | sort -t, -k1,1n >> "$OUTPUT_FILE"

echo "Merge successfully completed: $OUTPUT_FILE"

