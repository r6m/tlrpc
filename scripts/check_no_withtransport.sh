#!/bin/sh
set -e
if rg -n "WithTransport\(" .; then
  echo "WithTransport is not allowed. Use ServeTransport with listeners instead." >&2
  exit 1
fi
