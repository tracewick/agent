#!/bin/sh
# Waits until a URL answers with a 2xx status (images without a healthcheck tool).
# Usage: docker/wait-for-url.sh URL [TIMEOUT_SECONDS]
set -eu

url=$1
timeout=${2:-120}
elapsed=0

until curl -fsS -o /dev/null "$url" 2>/dev/null; do
	if [ "$elapsed" -ge "$timeout" ]; then
		echo "Timed out after ${timeout}s waiting for $url" >&2
		exit 1
	fi
	sleep 2
	elapsed=$((elapsed + 2))
done

echo "$url is ready"
