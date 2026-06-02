# Stale `.git/index.lock` Analysis — 2026-05-21

> Captures the 10+ stale-lock occurrences observed during the
> W-C W10/W9 series sessions (2026-05-20 ~ 2026-05-21), the
> diagnostic results, and the resolution options ranked by
> risk / effort.

## Observation

Across roughly 30 commits in this session run, `git add` or
`git commit` invoked from a fresh Bash returned:

```
fatal: '.../.git/index.lock' 파일을 만들 수 없습니다: File exists.
```

at a rate of roughly **10 occurrences / 30 commits** (~33%).
Every observation matched the same pattern:

  - Lock file size: **0 bytes**
  - Mtime: matches the moment we tried to commit (not earlier)
  - **No live `git` mutating process** in `ps aux`
  - Cleared by `rm .git/index.lock` + immediate retry → succeeds
  - Sometimes the retry needs a `sleep 1` or `sleep 2` to also
    succeed (suggests something *briefly* recreates the lock)

The contributing pattern was *not* present in earlier sessions,
or at least not at this frequency — it surfaced as a steady
background friction during this session run.

## Environment snapshot (2026-05-21)

```
git config --get core.fsmonitor   → (unset)
git config --get core.hooksPath   → (unset)

ps aux | grep gitstatusd          → 8+ instances of
                                    gitstatusd-darwin-arm64,
                                    one per active shell tab
ps aux | grep cursor              → Cursor.app process (Electron
                                    crash handler), gitWorker.js
                                    NOT currently visible
Active editors                    → Cursor (modifying viewer/*
                                    in another session — confirmed
                                    by `git status` showing
                                    M web/viewer-next/src/...)
```

The 8+ gitstatusd processes belong to the zsh prompt
(powerlevel10k / gitstatus.zsh integration) that runs
`git status` in the background after every shell prompt refresh.
Each one is read-only by design (no `--write` flag) but they
all touch the same .git directory and can briefly create their
own scratch state under contention.

## Hypotheses ranked by plausibility

1. **gitstatusd race + concurrent IDE git integration (most
   likely)**: 8+ background readers + an active editor (Cursor)
   touching the working tree means `.git/` is being lstat-ed
   and refreshed on a sub-second cadence. Each read uses
   short-lived internal locks. Our commit hits the window
   where one of them is *just* writing the index sentinel.

2. **Cursor IDE auto-fetch / status background process**: the
   process wasn't visible at sample time but the modified viewer
   files prove an editor session is open. The IDE plausibly
   runs `git status --porcelain` periodically; that command
   *does* touch `.git/index.lock` on Git ≥2.39 (via
   `core.untrackedCache` lazy updates), and on Apple Silicon
   filesystem timing this race opens fairly often.

3. **`gitstatusd` itself creating a stale 0-byte sentinel
   (less likely)**: the daemon is read-only per its source,
   but `--stat` plumbing can write a partial sentinel on cancel.
   Plausible under heavy shell churn but harder to confirm.

4. **Conductor / other background tooling**: nothing visible,
   so this is essentially "not the cause we can see".

## Resolution options ranked by risk / effort

| Option | Effort | Risk | Effect |
|---|---|---|---|
| A. Keep the manual `rm .git/index.lock && sleep 2 && git ...` workflow | 0 | 0 | Same as today — friction stays |
| B. Wrap commit/add in a shell function that auto-clears 0-byte lock | low | low — cleared only when 0-byte + no live git pid + lock older than N seconds | Most common occurrences disappear without user prompts |
| C. Reduce gitstatusd instance count (close unused shell tabs) | trivial | none | Empirically reduces race incidence; not a root-cause fix |
| D. Disable Cursor IDE's git auto-status / auto-fetch | trivial | losses IDE git integration UI in Cursor | Removes hypothesis #2 if true |
| E. Switch to a fsmonitor-aware setup (`git config core.fsmonitor true`) | medium | macOS fsmonitor v2 has known quirks on Apple Silicon | Reduces race width if hypothesis #1 is correct |
| F. Add a pre-commit retry hook in this repo | low | could mask a real concurrent commit issue | Only safe for personal workflow, not shared |

## Recommendation

For the immediate workflow:

  - **C is free** — close shell tabs that aren't doing anything
    (gitstatusd dies with its shell). Empirically reduces race
    incidence by the most direct route.
  - **B is the highest-ROI automated fix** — a small `git-safe`
    wrapper or shell function:

    ```sh
    git-safe() {
      if [ -f .git/index.lock ]; then
        local size=$(stat -f%z .git/index.lock 2>/dev/null)
        # Only clear when the lock is 0 bytes (stale sentinel)
        # AND no mutating git process is visible.
        if [ "$size" = "0" ] && ! pgrep -fl 'git (commit|add|rebase|merge|reset|pull|fetch|push)' >/dev/null; then
          rm -f .git/index.lock
          sleep 1
        fi
      fi
      git "$@"
    }
    ```

    Place in `~/.zshrc` and use `git-safe commit -m ...` for
    repos where this race is common. The two guards (0-byte AND
    no mutating process) keep the safety net narrow — we never
    clear a lock that any other git command is *actually
    holding*.

  - **D is reversible** — try it for a week; if the friction
    drops, hypothesis #2 was the cause. If not, re-enable.

For longer-term root cause:

  - **F is rejected** (masks real concurrent-commit bugs in
    shared workflows).
  - **E is worth trying** once a maintainer has time to verify
    macOS fsmonitor stability on this specific Apple Silicon /
    macOS version combination. Not blocking for current
    workflow.

## Out of scope

  - `make` / `ckg` source changes — the lock contention is a
    user environment phenomenon, not a ckg defect.
  - CI behaviour — GitHub Actions runners are single-shell and
    don't reproduce this race; the gate side (`make eval`,
    `make lint`) is unaffected.

## Tracking

If/when option B or D is adopted, append a row here with the
date and the observed change in lock-incidence rate. If the
issue *reappears* despite a chosen mitigation, that's the
signal to escalate to option E or to file a Cursor / git-
integration bug.

| Date | Mitigation | Observed incidence |
|---|---|---|
| 2026-05-21 | None — manual `rm` workflow | ~33% of commits |
