# Process Explorer and Disk & Storage

Press **4** for Process Explorer or **5** for Disk & Storage. Both are also available in the top navigation and Actions menu. They inspect the host running Systemdoc, independently of the Docker context, and share the dashboard header, gradient cards, selection styling and keyboard navigation.

Each panel carries its own view rail beneath the cards, naming the two views that suite actually has: **Flat** and **Tree** for processes, **Filesystems** and **Deleted but open** for storage. The rail is clickable and mirrors the keyboard. Where the terminal is at least 100 columns wide the header also carries the host CPU and memory readout described in [the dashboard guide](usage.md).

Each suite is lit by its own signature hue, taken from the theme's decorative accent-to-glow range, so Networking, Process Explorer and Disk & Storage are distinguishable at a glance. Error, warning and success keep one meaning everywhere and are never used for panel identity. Each panel also names its selection band for what it shows - **Process vitals**, **Capacity**, **Open handle** - rather than a generic "selected" label.

At 100 columns or wider the CPU and usage columns carry an inline meter beside the figure, coloured by severity: green below 70%, amber from 70%, red from 85%. Meters follow the shared `G` block/braille/ASCII setting, which changes presentation only and never the measurement.

![Process Explorer showing resource ranking and reviewed signal discovery](assets/processes.png)

*Illustrative process fixture rendered by the actual TUI; `F9` or `K` opens the signal palette for the selected row.*

## Process Explorer

Process Explorer takes a full host process snapshot with `ps`. To prevent short polling settings from creating bursty CPU load, this view refreshes no faster than every 15 seconds (a slower configured interval is respected). The temporary `ps` sampler itself is excluded from the displayed snapshot.

Press `G` to cycle the shared block, braille and ASCII signal style used by Process Explorer and Storage pressure meters. The choice is saved for every suite.

The two resource cards plot the machine's own recent CPU and memory utilisation on a fixed 0-100% scale, beside the summed `ps` figures. The chart is a real series sampled independently of the process snapshot; the summed figures are a single snapshot, and the card names both so they are not read as one measurement. The selection band below the table shows the selected process's CPU and resident memory as gauges, with its state, elapsed time and parent.

The process table shows full command lines, CPU estimate, resident memory (RSS), PID, user, state, parent PID and elapsed time. Narrow terminals keep the primary columns; Enter opens the complete record in a scrollable view.

- `S` cycles CPU, memory and PID sorting.
- `f` shows the flat list and `t` the parent-child tree; `t` still toggles between them. Filtering keeps matching rows; a process whose parent is filtered out becomes a displayed root.
- `/` filters commands, users and states. Combine fields such as `pid:123`, `ppid:1`, `user:alice`, `state:Z` and `command:node`.
- `F9`, `K` or Delete opens reviewed process actions: graceful terminate (`SIGTERM`), interrupt (`SIGINT`), hangup (`SIGHUP`), suspend (`SIGSTOP`), resume (`SIGCONT`) and force kill (`SIGKILL`). Choose an action, then press `y` on the review screen (or Tab to **Run**). The review shows the exact PID, user, state and command. PID 1 and Systemdoc itself are protected. Immediately before signalling, Systemdoc re-reads the command and refuses the action if the PID disappeared or changed identity. Signals use your existing OS permissions and never invoke `sudo`; a service manager may restart a supervised process.
- `n` opens Networking filtered to the selected PID, including listeners and connections. From Networking, `p` opens Process Explorer for the selected socket owner.
- `s` opens an associated service in the current inventory. Linux uses the process's service cgroup; macOS matches PIDs in the current launchd inventory. Standalone processes, inaccessible associations and services outside the current system/user scope are reported explicitly. The service inspector provides its usual logs and configuration tabs.

The collector uses `ps` on Linux and Apple Silicon macOS. CPU is the operating system's `ps` estimate, not an interval-based profiler; it can exceed 100% for multiple cores. Summed RSS can count shared memory more than once. Missing metrics show a dash. Parent relationships and PID ownership can change between the process sample and subsequent inspection.

## Disk & Storage

The selection band shows the selected filesystem's space and inode usage as full-width gauges with used, free and total figures. Filesystems that do not report inode counts say so rather than drawing an empty gauge.

The filesystem table shows mount paths, percentage used, available space, capacity, used space, inode usage and the source device. `S` cycles highest usage, least free space and mount-path order. Mounts at 85% space or inode usage are highlighted. `/` searches mount paths and source devices, including paths with spaces. Enter shows all fields.

`d` switches to **Deleted but open** files and `m` returns to filesystems. This runs `lsof +L1` when that view is opened and refreshes it while visible. It lists regular files that have been unlinked but are still held open, with path, logical size, owner, PID, UID and file descriptor. `S` sorts by size, PID or path. The two views retain separate filters and sample times.

Filesystem capacity comes from `df`; Linux inode counts are sampled separately. APFS/Btrfs shared pools and duplicate/bind mounts mean filesystem capacities cannot safely be added together, so the cards report mount count and pressure instead of a misleading host total. Unsupported inode counts show a dash. The deleted-file size card deduplicates known device/inode pairs; missing identities count per handle, and unknown sizes are excluded. Logical file size is not a promise of reclaimable disk space.

## Refresh and reports

Storage refreshes at the configured interval; Process Explorer uses the configured interval with a 15-second minimum. `r` always requests an immediate refresh, `P` pauses automatic refresh, Tab moves between filter/table/details, and Escape returns. Failed refreshes preserve the previous sample and show the error. Missing `ps`, `df` or `lsof`, permissions and truncated output are reported. Commands have a timeout and bounded output. Process signals are the only host mutation here; Systemdoc does not delete files, unmount storage or elevate privileges.

`e` opens the existing snapshot review/export flow for the filtered rows. Process commands and file paths can contain private information; inspect the report before sharing it.

macOS parsing follows Apple's [ps field definitions](https://github.com/apple-oss-distributions/adv_cmds/blob/main/ps/keyword.c) and [df output implementation](https://github.com/apple-oss-distributions/file_cmds/blob/main/df/df.c). Deleted files use the upstream [lsof field format](https://github.com/lsof-org/lsof/blob/master/Lsof.8).
