# E6 Stage D — Scan Orchestrator Research

nrobotcheck (Electron TypeScript) → fleetguard (Go) asset analysis for worker pool + robot×check fan-out orchestration.

---

## 1. Asset Inventory

### 1.1 Scan Execution Engine

**File:** /d/robot/dev/nrobotcheck/src/main/services/scanner/ScanEngine.ts (L1–L744)

Core orchestrator: batches robots, runs checks with configurable concurrency (default 50 robots, 5 checks per robot). Key methods:

- startScan(robotIds[], checks[], mainWindow) (L146–L223): Main entry point. Creates ScanSession, batches robots, calls scanRobot() concurrently.
- scanRobot(robot, checks, sessionId, progressMap, mainWindow) (L225–L353): Executes checks on one robot via SSH. Applies differential plan, runs checks in batches, persists results, triggers intelligence hooks (drift/anomaly/RCA/prediction/attack-path/compliance scoring).
- executeCheck(client, robot, check, sessionId) (L394–L505): Single check execution. Runs conditions sequentially, evaluates pass_logic, records evidence.
- pplyDifferentialPlan(robot, checks, sessionId) (L355–L392): Optional differential skip (time/hash/full strategies).
- cancelScan() (L507–L509): Sets shouldCancel flag; breaks outer/inner loops at next batch boundary.
- Evidence/drift/RCA/anomaly hooks injected via constructor; all fail-safe (catch, log, continue).

**SSH Layer:** /d/robot/dev/nrobotcheck/src/main/services/ssh/SSHClient.ts (L29–L246)

- connect(robot) (L39–L69): Retries (default 3x) with exponential backoff, no AbortController (loops + timeout).
- exec(command, timeout) (L119–L157): Stream-based, per-command timeout (default 30s), no cancellation token.
- execWithSudo(command, sudoPassword, timeout) (L159–L216): Wraps in sudo -S bash -c '...' to preserve shell operators.
- Timeout via setTimeout() → eject(), no graceful shutdown.

**Command Fallback Executor:** /d/robot/dev/nrobotcheck/src/domains/scan/services/CommandExecutor.ts (L58–L176)

- Primary + retry logic (default maxRetries=1, retryDelayMs=0).
- Falls back to command aliases from repository if primary fails.
- Returns CommandResult with full ttempts[] log for evidence.

### 1.2 Result Models

**ScanSession / ScanResult:** /d/robot/dev/nrobotcheck/src/shared/types/index.ts (L40–L73)

- ScanSession: id, name, status (running|completed|failed|cancelled), startedAt, completedAt?, totalRobots, totalChecks
- ScanResult: id, sessionId, robotId, checkId, status (pass|fail|error|skip), actualValue, commandOutput, checkedAt, severity, required

State machine: session starts as 'running', transitions to 'completed'|'failed'|'cancelled' after final batch. Per-result status determined by evaluateCondition + evaluatePassLogic.

### 1.3 Event Model

**EventBus:** /d/robot/dev/nrobotcheck/src/main/platform/eventBus/EventBus.ts (L11–L123)

In-memory pub-sub with pattern subscriptions. DomainEvent: { id, type, payload, occurredAt, correlationId?, causationId? }

Used for **IPC progress**: ScanEngine calls mainWindow.webContents.send('scan:progress', progress) + 'scan:result' + 'scan:complete' (L562–L578). Not persistence layer; domain events for audit only.

### 1.4 Differential Scan Strategy

**File:** /d/robot/dev/nrobotcheck/src/domains/scan/types/DifferentialScan.ts (L1–L63)

Three strategies:

- ull: Run every check.
- 	ime_based (days param): Skip if checkedAt > now - N days.
- hash_based: Skip if file hashes match last recorded snapshot.

Returns ScanPlan { toRun[], skipped[], strategy }. Skipped checks generate synthetic ScanResult with status='skip' + reason.

### 1.5 Evidence Sink

**File:** /d/robot/dev/nrobotcheck/src/domains/scan/services/ScanEvidenceSink.ts (L1–L80)

Glue: fires-and-forgets (never throws) per-condition evidence to EvidenceRepository. Failure → log + return null. Gated by settings toggle on every call. Records: command, stdout, stderr, exitCode, durationMs per condition.

---

## 2. Model Mapping (nrobotcheck → fleetguard scan domain)

| nrobotcheck | Concept | → fleetguard mapping |
|---|---|---|
| ScanSession | Session identity | Scan.id, startTime, status |
| ScanResult | Per robot×check outcome | Check.Result with status, actualValue, evidence |
| status: pass/fail/error/skip | Outcome enum | status: PASS, FAIL, ERROR, SKIP |
| evaluatePassLogic | Multi-condition AND/OR/NOT logic | EvaluationRule.expression |
| actualValue: string | Extracted + typed value | extractedValue: string or number |
| commandOutput: string | Full stdout + stderr dump | evidence.commandOutput |
| checkedAt: ISO timestamp | Result timestamp | result.timestamp |
| session.status transition | State machine | Scan.phase: QUEUED to COMPLETED/FAILED/CANCELLED |
| DifferentialStrategy | Skip logic | ScanOptions.scanMode: FULL, DIFFERENTIAL_TIME, DIFFERENTIAL_HASH |
| shouldCancel flag | Cancellation | context.Done() channel |
| IPC/BrowserWindow events | Progress streaming | Server-Sent Events or Websocket |
| Evidence + links | Audit trail | Evidence domain: store command execution |

---

## 3. Cancel, EventBus & Test Patterns

### 3.1 Cancellation

**Current (nrobotcheck):** Synchronous flag shouldCancel: boolean, checked at outer loop (batch) and inner loop (check) boundaries. No in-flight command termination (timeout only).

**Graceful semantics:** Waits for current batch to finish before breaking. In-progress SSH exec runs to completion (30s timeout). No SIGTERM or stream abort.

**Risks:** If a check hangs in loop, cancel waits for timeout. No per-robot task tracking for targeted interruption.

**Reusable pattern:** Batch-scoped cancellation token. Use ctx context.Context + select ctx.Done() in goroutines.

### 3.2 EventBus / Progress Reporting

**Current (nrobotcheck):** Direct Electron IPC:
- mainWindow.webContents.send('scan:progress', { robotId, currentCheck, totalChecks, status })
- mainWindow.webContents.send('scan:result', result)
- mainWindow.webContents.send('scan:complete', session)

No domain event bus integration in ScanEngine (audit is decoupled via writeAuditEntry). Progress is UI-only (IPC, no persistence).

**fleetguard mapping:** 
- Progress: Server-Sent Events or Websocket streaming from scan handler.
- Completion event: Publish to domain event bus (scan.completed) for downstream insights/compliance.
- Audit: Separate from progress (audit handler logs to immutable chain).

### 3.3 Testing

**Unit tests:** In-memory doubles (ScriptedRunner, HistoryStub, HashStub) in test files (267 total tests).

Example (CommandExecutor.test.ts): ScriptedRunner queues RawCommandResult per command string. Allows scripting "first call fails → retry succeeds". Vitest framework.

**Integration setup:** Docker Compose at benchmark/docker/docker-compose.yml with single Ubuntu SSH service. Minimal; used for benchmarks, not integration tests. Tests use doubles or skip SSH.

**Recommendation for fleetguard:**
- Unit: testify/assert + table-driven tests, in-memory service doubles.
- Integration: Use github.com/gliderlabs/ssh for mock server or containerized Linux.
- No custom EventBus test harness needed initially; use context cancellation for control flow.

---

## 4. Applicable Recommendations for Stage D

### 4.1 Batch-Based Concurrency

**Adopt:** Batch (robot pool) + inner batch (checks per robot) model directly. Simplify by dropping robot-scoped progress in MVP; report overall progress (X of N checks completed).

### 4.2 Differential Scanning

**Reuse:** Time-based and hash-based skip strategies verbatim. Port CheckRef, LastResult, ScanPlan types. Implement ScanHistoryProvider against fleetguard scan_result table.

### 4.3 Result Aggregation & Persistence

**Single transaction per robot:** Batch all checks for a robot into one INSERT, then run intelligence hooks. Avoid row-level locking storms.

### 4.4 Cancellation Handling

**Use context.Context:** Pass ctx to all goroutines. Check select case <-ctx.Done() at loop boundaries (robot, check batches). Keep timeout in SSH exec layer but let parent context override.

### 4.5 Progress Streaming

**Replace IPC with Server-Sent Events (SSE)** or WebSocket. Emit { event: 'scan:progress', data: {...} } per check. Client subscribes to /scan/{sessionID}/events.

### 4.6 Intelligence Hook Fail-Safety

**Copy pattern:** All hooks (drift, anomaly, RCA, compliance scoring) must be fault-tolerant. If a hook panics, log and continue scan. Consider a per-hook timeout (e.g., 10s).

---

## 5. Pitfalls to Avoid

### 5.1 Sudo Password in Process Arguments

**nrobotcheck pitfall:** Writes sudo password to stream stdin (SSHClient.ts L190). Avoids process list exposure but resets stdin for each subsequent command. Callers must rewrite password per exec.

**Action:** In fleetguard, either use SSH agent forwarding, keep per-connection sudo session (sudo -v once, reuse), or do NOT store password in memory; read from secure store per-call.

### 5.2 Timeout Semantics

**Current (nrobotcheck):** Per-command timeout via setTimeout(), but does NOT kill the remote process. Only client closes stream. Remote may keep running.

**Action:** Implement SSH channel close + SIGTERM on timeout. Go ssh2 library supports this via stream.CloseWrite().

### 5.3 Connection Pool Reuse Across Cancellation

**Risk:** If cancel is called mid-batch, SSHClient is still in connect pool. On next scan, stale connection may fail.

**Action:** Scan cancellation must drain connection pool. Implement connection factory with lifecycle tracking.

### 5.4 Evidence Sink Not Awaited Properly

**Current:** ScanEvidenceSink.recordConditionOutput() is awaited, but failure is silently logged. If Evidence table is down, scan continues without evidence but no alert to user.

**Action:** For critical evidence (audit compliance), escalate failures. For non-critical (performance tuning), continue + warn.

### 5.5 Pass Logic Evaluation as String

**Current (nrobotcheck):** evaluatePassLogic uses recursive tree walk. If PassLogic is corrupted or undefined, returns false (safe default).

**Action:** In fleetguard, validate PassLogic at spec load time, not at scan time. Use AST or expression language (e.g., CEL, Lua) for clarity.

### 5.6 No Circuit Breaker on Robot Failures

**Current:** If a robot SSH fails, that robot is retried per connection retry policy, but no circuit breaker. A dead robot delays batch.

**Action:** After 3 SSH connection failures, mark robot as offline and skip remaining checks with status='skip', reason='robot_offline'. Emit alert event.

### 5.7 Batch Wait Semantics

**Current (nrobotcheck):** Promise.all() on batch. If one promise rejects, the whole batch fails.

**Action:** Use Promise.allSettled() to wait for all, then inspect failures. Log per-robot failures without breaking overall scan.

---

## 6. Key Code Locations for Reference

| Task | File | Lines |
|---|---|---|
| Main scan orchestration | ScanEngine.ts | 146-223, 225-353 |
| SSH command execution | SSHClient.ts | 39-69, 119-157, 159-216 |
| Result evaluation | ResultEvaluator.ts | 7-80 |
| Differential plan | DifferentialScanner.ts + DifferentialScan.ts | Type defs + logic |
| Evidence recording | ScanEvidenceSink.ts | 48-80 |
| IPC progress | scan.router.ts | 29-69, 72-86 |
| Tests | CommandExecutor.test.ts | In-memory doubles |

---

**Status:** Research complete. Ready for Stage D implementation kickoff.
