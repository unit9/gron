# gron

Simple cron in Go.

Use with one or more `*.yml` files, formatted like so:

```yaml
cron:
- description: some minutely task
  command: echo minutely; date
  lock: yes

- description: some hourly task
  command: echo hourly; date
  minute: 0
  lock: yes

- description: some daily task
  command: echo daily; date
  hour: 1
  minute: 0

- description: some weekly task
  command: echo weekly; date
  weekday: 0
  hour: 2
  minute: 0

- description: some monthly task
  command: echo monthly; date
  day: 5
  hour: 3
  minute: 0

- description: some hourly task with specific working directory
  command: echo task; pwd; date
  pwd: /tmp
  minute: 0
```

Each job will run when the server's wall clock hits the exact
specified time. Omitted time constraints mean no constraint.

Weekdays start with 0 (Sunday). 7 is NOT aliased to Sunday.

Command is interpreted with `/bin/sh` via `-c`.

`pwd` specifies command's working directory. It is optional; defaults
to `gron`'s own cwd.

Each task runs independently of the others that may have started at
the same time - there is no blocking. Failures are simply logged -
there is no retry or backoff.

If `lock` is set to `yes` (default is `no`), a simple locking
mechanism is used. While an instance of the task is still running, the
task will not be re-run. So an hourly task, that runs for 90 minutes,
will run 12 times per day.

Pass `-d` to get more debug spam with nicer formatting.
