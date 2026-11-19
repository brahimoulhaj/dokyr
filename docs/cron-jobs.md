# Cron jobs

Back to the [docs index](README.md).

Cron jobs (main menu, owner and admins) runs commands on the server on a schedule, in
place of `crontab -e`: a 5-field cron expression in UTC, a bash script, the user it runs as,
an optional name. Each job can be run now, switched off, edited or deleted, and keeps its
last 20 runs with what they printed (the last 64 KiB) and their exit code.

## How jobs run

The panel keeps the schedule itself and runs each job on the host through the helper the
host terminal uses. So the jobs need no cron daemon, do not appear in `crontab -l`, leave
hand-written crontabs alone, and run only while the panel is up: a slot missed while it
was stopped is not caught up. A job still running at its next slot is skipped, and a run
is stopped after 6 hours.

## Access rules

A job is a command on the host, so it follows the host terminal's rules: browser session
of the owner or an admin only (no API token), the password again before adding, changing,
enabling or running one, and nothing runs or changes while host access is switched off in
Settings → Security. A new job runs as the user set there; root has to be typed. Changes,
manual runs and failed runs are recorded in Activity.
