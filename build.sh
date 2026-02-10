#!/bin/bash
set -e

OUTPUT_DIR="dist"
MODULE="vmware-inventory"

rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

platforms=(
    "darwin/arm64/mac"
    "linux/amd64/linux"
    "windows/amd64/windows"
)

for platform in "${platforms[@]}"; do
    GOOS="${platform%%/*}"
    GOARCH="${platform#*/}" && GOARCH="${GOARCH%/*}"
    LABEL="${platform##*/}"
    output="$OUTPUT_DIR/${MODULE}-${LABEL}-${GOARCH}"
    if [ "$GOOS" = "windows" ]; then
        output="${output}.exe"
    fi
    echo "Building $output"
    GOOS=$GOOS GOARCH=$GOARCH go build -o "$output"
done

echo "Done. Binaries in $OUTPUT_DIR/"
ls -lh "$OUTPUT_DIR/"
