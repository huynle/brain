#!/bin/sh
# Experimental build recipe ONLY, not a production runtime selection. The Go
# fixture verifies the official source digest and the installed image identity.
# Arguments are trusted fixture paths: extracted root, C entry, output artifact.
set -eu
export LC_ALL=C TZ=UTC SOURCE_DATE_EPOCH=1780531200
root="$1"
entry="$2"
output="$3"
cd "$root/quickjs-2026-06-04"
exec /usr/bin/cc -O2 -D_GNU_SOURCE -DCONFIG_VERSION='"2026-06-04"' \
  -D_FORTIFY_SOURCE=3 -fstack-protector-strong -fPIE -pie \
  -Wl,-z,relro,-z,now,-z,noexecstack -fno-ident \
  "-ffile-prefix-map=$root=/brain-worker-source" \
  -I. "$root/$entry" quickjs.c dtoa.c libregexp.c libunicode.c cutils.c \
  -lm -o "$output"
