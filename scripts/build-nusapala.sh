#!/bin/sh
# Build on the target Linux machine with its native compiler and development libs.
set -eu
cd "$(dirname "$0")/.."
[ "$(uname -s)" = Linux ] || { echo 'Build Nusapala membutuhkan Linux.' >&2; exit 1; }
pkg-config --exists libusb-1.0 portaudio-2.0
case "$(uname -m)" in arm*|aarch64) pkg-config --exists opus ;; esac
mkdir -p dist/nusapala
CGO_ENABLED=1 go build -tags nusapala -o dist/nusapala/piio-nusapala ./cmd/nusapala
cp config.nusapala.json dist/nusapala/
cp docs/nusapala-lab.md dist/nusapala/README.md
printf 'Paket native tersedia di dist/nusapala/\n'
