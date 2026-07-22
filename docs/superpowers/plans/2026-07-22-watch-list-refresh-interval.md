# Watch List Refresh Interval Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refresh the visible TUI watch list every 10 seconds while keeping fund views on their existing 30-second schedule.

**Architecture:** Keep the current fund timer and add a separate Bubble Tea command and message for watch-list ticks. Each timer reschedules itself, but only the watch-list timer may start `LoadWatchRows`, and only while the watch list is visible and idle.

**Tech Stack:** Go, Bubble Tea commands and messages, Go's built-in `testing` package, Make.

## Global Constraints

- Refresh the visible watch list every 10 seconds.
- Keep the fund list and fund detail refresh interval at 30 seconds.
- Do not auto-refresh watch detail.
- Keep manual `r` and force-refresh `R` behavior unchanged.
- Keep `stock_quote/<code>` and `stock_minute/<code>` cache semantics unchanged.
- Reuse the existing watch-list quote, minute-data, stale-fallback, and error paths.
- Do not add dependencies or change TUI navigation.

---

## File Structure

- Modify `internal/tui/app.go`: own both timer intervals, timer messages, scheduling commands, and page-specific tick handling.
- Modify `internal/tui/model_test.go`: verify timer registration and model transitions without making network requests or waiting for real timers.

### Task 1: Add the watch-list timer and isolate its refresh behavior

**Files:**
- Modify: `internal/tui/app.go:20-20,86-86,163-168,299-324,725-729`
- Test: `internal/tui/model_test.go`

**Interfaces:**
- Consumes: `model.loadWatch() tea.Cmd`, `model.spinnerTickCmd() tea.Cmd`, and the existing `tick() tea.Cmd` fund timer.
- Produces: `watchRefreshEvery time.Duration`, `watchTickMsg`, and `watchTick() tea.Cmd` for a watch-list-only 10-second schedule.

- [ ] **Step 1: Write the failing model-transition tests**

Add these tests after `TestTabSwitchesBetweenFundAndWatchLists` in `internal/tui/model_test.go`:

```go
func TestRefreshIntervals(t *testing.T) {
	if refreshEvery != 30*time.Second {
		t.Fatalf("refreshEvery = %s, want 30s", refreshEvery)
	}
	if watchRefreshEvery != 10*time.Second {
		t.Fatalf("watchRefreshEvery = %s, want 10s", watchRefreshEvery)
	}
}

func TestInitSchedulesBothRefreshTimers(t *testing.T) {
	cmd := (model{watch: newWatchState()}).Init()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init command message = %T, want tea.BatchMsg", cmd())
	}
	if len(batch) != 5 {
		t.Fatalf("Init batch length = %d, want 5 commands", len(batch))
	}
}

func TestWatchTickRefreshesOnlyVisibleWatchList(t *testing.T) {
	tests := []struct {
		name          string
		start         model
		wantLoading   bool
		wantErrText   string
		wantLoadBatch bool
	}{
		{
			name:          "visible watch list",
			start:         model{page: pageList, listMode: listWatch, watch: watchState{ErrText: "stale"}},
			wantLoading:   true,
			wantLoadBatch: true,
		},
		{
			name:        "refresh already running",
			start:       model{page: pageList, listMode: listWatch, watch: watchState{Loading: true, ErrText: "keep"}},
			wantLoading: true,
			wantErrText: "keep",
		},
		{
			name:        "fund list",
			start:       model{page: pageList, listMode: listFunds, watch: watchState{ErrText: "keep"}},
			wantErrText: "keep",
		},
		{
			name:        "watch detail",
			start:       model{page: pageWatchDetail, listMode: listWatch, watch: watchState{ErrText: "keep"}},
			wantErrText: "keep",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, cmd := tt.start.Update(watchTickMsg(time.Now()))
			got := updated.(model)
			if got.watch.Loading != tt.wantLoading {
				t.Fatalf("watch.Loading = %v, want %v", got.watch.Loading, tt.wantLoading)
			}
			if got.watch.ErrText != tt.wantErrText {
				t.Fatalf("watch.ErrText = %q, want %q", got.watch.ErrText, tt.wantErrText)
			}
			if cmd == nil {
				t.Fatal("watch tick must schedule its next command")
			}
			if tt.wantLoadBatch {
				batch, ok := cmd().(tea.BatchMsg)
				if !ok || len(batch) != 3 {
					t.Fatalf("visible watch tick command = %T len=%d, want three-command batch", batch, len(batch))
				}
			}
		})
	}
}

func TestFundTickDoesNotRefreshWatchList(t *testing.T) {
	start := model{
		page:     pageList,
		listMode: listWatch,
		watch:    watchState{ErrText: "keep"},
	}

	updated, cmd := start.Update(tickMsg(time.Now()))
	got := updated.(model)
	if got.watch.Loading {
		t.Fatal("30-second fund tick should not refresh the watch list")
	}
	if got.watch.ErrText != "keep" {
		t.Fatalf("watch.ErrText = %q, want existing text preserved", got.watch.ErrText)
	}
	if cmd == nil {
		t.Fatal("fund tick must schedule its next command")
	}
}
```

- [ ] **Step 2: Run the focused tests and verify the red state**

Run:

```bash
GOCACHE=$PWD/.gocache go test ./internal/tui -run 'Test(RefreshIntervals|InitSchedulesBothRefreshTimers|WatchTickRefreshesOnlyVisibleWatchList|FundTickDoesNotRefreshWatchList)$' -count=1
```

Expected: FAIL to compile because `watchRefreshEvery` and `watchTickMsg` do not exist. The existing `tickMsg` behavior would also fail `TestFundTickDoesNotRefreshWatchList` once those symbols are present.

- [ ] **Step 3: Add the separate timer and page-specific message handling**

Replace the single interval declaration in `internal/tui/app.go` with:

```go
const (
	refreshEvery      = 30 * time.Second
	watchRefreshEvery = 10 * time.Second
)
```

Declare the watch message beside `tickMsg`:

```go
type tickMsg time.Time
type watchTickMsg time.Time
```

Schedule both timers from `Init`:

```go
func (m model) Init() tea.Cmd {
	if m.watch.Input.Prompt == "" {
		m.watch = newWatchState()
	}
	return tea.Batch(m.load(), m.loadWatch(), tick(), watchTick(), m.spinnerTickCmd())
}
```

Replace the current `case tickMsg` branch and add the watch branch immediately after it:

```go
	case tickMsg:
		if m.page == pageDetail {
			if !m.detail.Loading {
				m.detail.Loading = true
				m.detail.ErrText = ""
				return m, tea.Batch(tick(), m.loadDetail(), m.spinnerTickCmd())
			}
			return m, tick()
		}
		if m.page == pageWatchDetail || m.listMode == listWatch {
			return m, tick()
		}
		if !m.loading {
			m.loading = true
			m.errText = ""
			return m, tea.Batch(tick(), m.load(), m.spinnerTickCmd())
		}
		return m, tick()
	case watchTickMsg:
		if m.page == pageList && m.listMode == listWatch && !m.watch.Loading {
			m.watch.Loading = true
			m.watch.ErrText = ""
			return m, tea.Batch(watchTick(), m.loadWatch(), m.spinnerTickCmd())
		}
		return m, watchTick()
```

Add the watch timer beside `tick()`:

```go
func watchTick() tea.Cmd {
	return tea.Tick(watchRefreshEvery, func(t time.Time) tea.Msg {
		return watchTickMsg(t)
	})
}
```

- [ ] **Step 4: Format the changed Go files**

Run:

```bash
gofmt -w internal/tui/app.go internal/tui/model_test.go
```

Expected: command exits with status 0 and produces no output.

- [ ] **Step 5: Run the focused tests and verify the green state**

Run:

```bash
GOCACHE=$PWD/.gocache go test ./internal/tui -run 'Test(RefreshIntervals|InitSchedulesBothRefreshTimers|WatchTickRefreshesOnlyVisibleWatchList|FundTickDoesNotRefreshWatchList)$' -count=1
```

Expected: PASS.

- [ ] **Step 6: Run all TUI tests**

Run:

```bash
GOCACHE=$PWD/.gocache go test ./internal/tui -count=1
```

Expected: PASS. Existing manual refresh, force refresh, list/detail navigation, loading, and rendering tests remain green.

- [ ] **Step 7: Run the repository verification suite**

Run:

```bash
make verify
```

Expected: `go test ./...`, `go vet ./...`, and the `fundpeek` build all succeed.

- [ ] **Step 8: Review the final diff**

Run:

```bash
git diff --check
git diff -- internal/tui/app.go internal/tui/model_test.go
```

Expected: no whitespace errors; the diff contains only the separate 10-second watch timer, the removal of watch refresh work from the 30-second timer, and the matching tests.

- [ ] **Step 9: Commit the implementation**

```bash
git add internal/tui/app.go internal/tui/model_test.go
git commit -m "Refresh watch list every 10 seconds"
```
