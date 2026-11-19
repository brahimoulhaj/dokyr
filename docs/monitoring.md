# Monitoring

Back to the [docs index](README.md).

## Host metrics

The dashboard samples host CPU, RAM, swap, load, uptime, data-disk capacity and network
traffic every five seconds, keeping ten minutes of history in memory. The disk is the
filesystem holding `DOKYR_DATA_DIR`; available space excludes filesystem reservations.
For host network readings inside Docker, mount `/proc:/host/proc:ro` on the panel
container (included in the installer and development Compose file). Existing installations
can re-run the installer or add that mount and recreate the panel container. Image-only
updates preserve existing mounts. `DOKYR_HOST_PROC` overrides the proc path for custom
setups. On Docker Desktop these are the Linux VM's metrics. Unavailable readings are
shown explicitly, and loopback, container bridges and tunnels are excluded from network
totals. Metrics are available only through the authenticated `/api/host/metrics` endpoint.

## Docker metrics

The Docker dashboard card uses the connected daemon's read-only API to show container
states, images, volumes, networks, engine configuration and per-resource storage usage.
Counts refresh every 15 seconds; aggregate disk scans run at most once a minute.
Sizes and reclaimable space are Docker's estimates, exclude bind mounts and container
logs, and are shown by category because image and build-cache layers may overlap.
If Docker becomes unavailable, the card labels the last known readings. The endpoint
is `/api/docker/metrics` and requires authentication.

## App problems

The dashboard polls `/api/apps/problems` every five seconds to show crashing, down or
missing app containers, with project names and links to their logs. Intentionally stopped
apps and apps currently deploying are excluded. Recovery removes the alert on the next
poll; an unavailable Docker daemon is reported separately from an app crash.
