#!/bin/sh
# Checks that a running stand is wired correctly. Run after `make stand-up`.
set -eu

cd "$(dirname "$0")/.."

echo '--> Pyroscope is ready'
docker/wait-for-url.sh http://127.0.0.1:4040/ready

echo '--> Toxiproxy exposes the pyroscope proxy'
curl -fsS http://127.0.0.1:8474/proxies | grep -q '"pyroscope"'

echo '--> Pyroscope is reachable through Toxiproxy'
docker/wait-for-url.sh http://127.0.0.1:14040/ready

echo 'Stand smoke test passed'
