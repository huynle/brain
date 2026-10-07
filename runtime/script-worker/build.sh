#!/bin/sh
# Reproducible release recipe for the sealed QuickJS worker (see release.json).
# Run only inside the pinned compiler image, offline, after verifying the source
# archive SHA-256. Arguments: extracted root (containing quickjs-2026-06-04/, the
# entry C file and seal.h), entry C file, output artifact. Not activation.
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
