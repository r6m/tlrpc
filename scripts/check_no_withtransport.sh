#!/bin/sh
set -e
pattern="WithTransport"
if rg -n "${pattern}\\(" .; then
  echo "WithTransport is not allowed. Use ServeTransport with listeners instead." >&2
  exit 1
fi
