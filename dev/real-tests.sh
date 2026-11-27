#!/bin/sh
# The Go tests that need a real Docker daemon: containers through the executor, a real
# Caddy, a real deploy, the registry, the built-in storage, database instances and image
# builds. `go test -short` leaves them out, and without -short each one skips itself when
# the daemon or an image is missing, which reads as a pass. This script makes sure they
# ran: it points the tests at the daemon the docker CLI uses, pulls the images they look
# for, and fails when a test skipped that is not on the short list below.
#
#   make test-docker
set -eu

# Tests that may skip: they need a tool or an image too large to fetch on every run.
ALLOWED_SKIPS="TestBuildRailpack TestBuildNixpacks TestBuildBuildpacks"

PACKAGES="./internal/executor ./internal/proxy ./internal/deploy ./internal/registry ./internal/objectstore ./internal/database ./internal/builder"

command -v docker >/dev/null || { echo "docker is not installed" >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "the Docker daemon is not reachable" >&2; exit 1; }

# The Go client reads DOCKER_HOST or falls back to /var/run/docker.sock. Docker Desktop,
# OrbStack and others keep their socket elsewhere and tell only the CLI, through its
# context: without this the tests find no daemon and skip.
if [ -z "${DOCKER_HOST:-}" ]; then
	DOCKER_HOST=$(docker context inspect --format '{{.Endpoints.docker.Host}}')
	export DOCKER_HOST
fi
echo "Docker: $DOCKER_HOST"

# Images a test looks for locally and skips without.
for image in alpine:3.22 rustfs/rustfs:latest rustfs/rustfs:1.0.0-rc.6 moby/buildkit:v0.34.2; do
	docker image inspect "$image" >/dev/null 2>&1 || docker pull --quiet "$image"
done

log=$(mktemp)
trap 'rm -f "$log"' EXIT
status=0
# shellcheck disable=SC2086
go test -count=1 -timeout "${TEST_TIMEOUT:-30m}" -v $PACKAGES >"$log" 2>&1 || status=$?

grep -E '^(ok|FAIL|---|panic:)|^\s+--- (FAIL|SKIP)' "$log" || true
if [ "$status" -ne 0 ]; then
	echo
	echo "==== full output of the failing run ====" >&2
	cat "$log" >&2
	exit "$status"
fi

skipped=$(sed -n 's/^ *--- SKIP: \([^ ]*\).*/\1/p' "$log")
bad=""
for name in $skipped; do
	top=${name%%/*}
	case " $ALLOWED_SKIPS " in
	*" $top "*) ;;
	*) bad="$bad $name" ;;
	esac
done
if [ -n "$bad" ]; then
	echo
	echo "These tests skipped, so they proved nothing:$bad" >&2
	echo "Their reasons:" >&2
	grep -B3 -E '^ *--- SKIP' "$log" | grep -E '_test\.go:[0-9]+:' >&2 || true
	exit 1
fi
echo
echo "Real-Docker tests ran: $(grep -c '^--- PASS' "$log") passed, allowed to skip:$(for n in $skipped; do printf ' %s' "$n"; done)"
