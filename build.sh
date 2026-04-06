#!/usr/bin/env bash
set -e

go test -failfast -v -run "^TestSteamAPI" ./sptt/...

GOOS=linux GOARCH=amd64 go build -o spt .
GOOS=linux GOARCH=amd64 go build -o createtoken ./cmd/createtoken

echo "Done."
