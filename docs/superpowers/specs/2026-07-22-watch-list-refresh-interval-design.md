# Watch List Refresh Interval Design

## Problem

The TUI uses one 30-second timer for the fund list, fund detail, and watch
list. The watch list therefore updates stock quotes and intraday minute data
only once every 30 seconds. The requested change is a 10-second refresh for
the visible watch list without increasing the refresh rate of any fund view.

Each watch-list refresh makes one batched quote request and one minute-data
request per stock. A 5-second interval would double this traffic, so the watch
list will use the approved 10-second interval.

## Design

Keep the existing 30-second timer for the fund list and fund detail. Add a
separate 10-second watch-list timer with its own message type. Both timers are
scheduled when the TUI starts and reschedule themselves after each message.

The existing timer will skip refresh work while the watch list or a watch
detail is visible. The new timer will call the existing watch-list load path
only when the watch list is visible and no watch refresh is already running.
On every other view, it will only schedule its next message. This prevents the
two timers from starting duplicate watch refreshes.

Watch detail will retain its current behavior and will not refresh
automatically. Manual `r` and force-refresh `R` behavior will stay unchanged.
The implementation will reuse `LoadWatchRows`, including its current quote and
minute-data requests, cache keys, stale fallback, and error reporting.

## Alternatives Considered

- A single 10-second pulse could check timestamps and refresh fund views only
  every third pulse. This would add timing state to every view and make the
  existing 30-second behavior less direct.
- Replacing the timer whenever the user changes views would require stale
  timer messages to be identified and ignored. Two page-specific timer
  messages provide the same behavior with less state.
- Refreshing every 5 seconds would double upstream traffic for a small visible
  improvement and is unnecessary for this change.

## Verification

Add model-update tests that prove a watch-list timer refreshes only the visible
watch list, does not start a second refresh while one is running, and does not
refresh watch detail. Add a regression test showing that the existing timer
does not refresh the watch list. Keep the current fund-list and fund-detail
timer tests passing, then run `make verify`.
