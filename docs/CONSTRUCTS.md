# Temporal code shapes PathKit reads

PathKit lists the paths of a Temporal workflow written in Go, and shows which of them the tests ran. This page lists, one by one, the code shapes of the Temporal SDK that PathKit has been checked against, and says for each one what PathKit does with it.

Every shape ends in one of two states, and none is left silently miscounted:

- **handled**: PathKit reads it. The paths it shows are right for the shape, and the table says how the shape appears in them.
- **reported**: PathKit does not turn it into paths (or cannot). It says so in the "PathKit could not fully read" panel of the report (`--json` key `unread`), with the file and the line, so nobody takes the numbers for more than they are.

Each row has a small made-up workflow in `testdata/constructs/<folder>` with an `expected.json` that fixes the paths PathKit must show for it (a test, `internal/constructs`, checks every one, and checks that this table says the same as the files).

A few words used below: a *future* is the handle Temporal gives you when you start an activity or a child workflow; you read the result later with `Get`. A *selector* (`workflow.Selector`) waits for the first of several things. A *handler* is code a workflow registers so the outside can call it (a signal, a query, an update). A *path* is one way through the workflow's decisions, from the start to an end.

| Shape | Status | Folder | What PathKit does |
|---|---|---|---|
| A future read later (`f := ExecuteActivity(...)`, then `f.Get`) | handled | `future_later` | A step happens where the future is read (Get), not where the activity is started: the early return before the Get has no activity step. |
| Several futures started, then read in a loop | handled | `futures_loop` | Futures started in one loop and read in another: the read is the step ("Future (future)"). PathKit sees the second loop walks the futures built by the first, so the paths that skip one of them are shown as impossible. |
| Futures in a `workflow.Selector` (`AddFuture`) | handled | `future_selector` | workflow.Selector.AddFuture gives one exit per future ("activity FastPath", "activity SlowPath"). |
| A future that is never read | reported | `future_unread` | A future that is started and never read is not a step of any path, and its failure is not a decision; the panel lists it as "result never read". |
| `workflow.Sleep` | handled | `timer_sleep` | workflow.Sleep is a decision when the code checks the error: the timer fired (success) or ended with an error such as cancellation (failure). |
| `workflow.NewTimer` that the code can cancel | handled | `timer_cancel` | workflow.NewTimer is read where the timer future is read: fired (success) or ended with an error such as being cancelled (failure). Cancelling the timer with its cancel function is a plain call. |
| A `NewTimer` future in a `workflow.Selector` | handled | `timer_selector` | A timer future in a workflow.Selector is an exit ("timeout") next to the signal case. |
| `workflow.Await` | handled | `await_plain` | workflow.Await is a decision: the condition was met (success) or the wait ended with an error such as cancellation (failure), when the code checks the error. The goroutine that sets the condition is a closure PathKit does not map, so it is listed as not read. |
| `workflow.AwaitWithTimeout` | handled | `await_timeout` | workflow.AwaitWithTimeout gives three outcomes: error (failure), signaled (the condition was met) and timeout (ok is false). The goroutine that sets the condition is a closure PathKit does not map, so it is listed as not read. |
| `SetUpdateHandler` (the handler's code) | handled | `update_handler` | The code of an update handler is mapped like a small workflow of its own ("<workflow>.update[add]"): its decisions are its paths, a test that sends the update records a trace for it, and it has a row in the handler table. |
| `SetUpdateHandler` with a method or function of the package (not a function literal) | handled | `update_named` | An update handler that is a method or function of the workflow's own package (not a function literal) is mapped like a small workflow of its own too ("<workflow>.update[add]"), recorded when a test sends the update. |
| `SetUpdateHandlerWithOptions` with a validator (accept / reject by returning an error) | handled | `update_validator` | An update validator is mapped like a small workflow ("<workflow>.validator[add]"): returning nil accepts the update, returning an error rejects it. It has a row in the handler table. |
| A validator that rejects by panicking | reported | `update_validator_panic` | A validator that rejects by panicking: a path that panics is not a path, so the rejection is missing from the validator's map. The panel lists the panic; reject by returning an error to have it mapped. |
| A Nexus operation (`NewNexusClient(...).ExecuteOperation`) | handled | `nexus_op` | workflow.NewNexusClient(...).ExecuteOperation is read like a child workflow: the result of the operation is a decision, success or failure (cancelled when the code checks for it). |
| `ctx.Done()` as a selector case | handled | `cancel_done_selector` | ctx.Done() as a workflow.Selector case is an exit of the selector ("receive ctx.Done()"), next to the other cases. |
| `temporal.IsCanceledError` after a call | handled | `cancel_is_canceled` | temporal.IsCanceledError(err) after a call that failed is a decision of its own: cancelled (true) or another error (false). |
| `workflow.WithCancel` and calling the cancel function | reported | `cancel_with_cancel` | workflow.WithCancel and calling cancel() are plain calls. The activity read by "return f.Get(ctx, nil)" is returned directly, so by default its failure and success are one path; the panel lists it. With classification.direct_returns set to "step" they are two paths. |
| `workflow.NewDisconnectedContext` for cleanup after cancellation | handled | `cancel_disconnected` | workflow.NewDisconnectedContext and its cancel function are plain calls, not decisions. The work before the cleanup and the cleanup after a failure are on the path; a cleanup activity whose error the code ignores is not a decision. |
| Telling an application error apart (`errors.As` to `*temporal.ApplicationError`) | handled | `errkind_application` | errors.As to *temporal.ApplicationError after a failure is a decision of the code (true: an application error). The call outcome table lists it as a kind of failure of the call. |
| Telling a non-retryable application error apart (`NonRetryable()`) | handled | `errkind_nonretryable` | errors.As to *temporal.ApplicationError together with NonRetryable() is a decision (true: a non-retryable application error). The call outcome table lists it as a kind of failure. |
| Telling a timeout apart (`temporal.IsTimeoutError`) | handled | `errkind_timeout` | temporal.IsTimeoutError(err) after a failure is a decision (true: a timeout). The call outcome table lists it as a kind of failure of the call. |
| Telling a cancelled child workflow apart (`temporal.IsCanceledError`) | handled | `errkind_child_canceled` | temporal.IsCanceledError(err) after a child workflow failed is a decision (true: cancelled). The call outcome table lists "cancelled" as a kind of failure of the child workflow call. |
| `ReceiveWithTimeout` on a signal channel | handled | `signal_timeout` | ReceiveWithTimeout is a decision: a signal came in time (received) or not (not received). |
| `ReceiveAsync` on a signal channel | handled | `signal_async` | ReceiveAsync is a decision: a signal was waiting (received) or not (not received). |
| A loop (0, 1 or more rounds) | handled | `loop_rounds` | A loop is one round per path (the path model stays as it is). The loop rounds table tells from the recorded runs whether the loop ran with 0, 1 and 2 or more rounds. |
| A company wrapper function that runs an activity (`classification.activity_wrappers`) | handled | `wrapper_activity` | A function of a company library that runs an activity is told to PathKit with classification.activity_wrappers: it is then a step with a success and a failure outcome. Without the entry it is listed as not read, with the entry to add. |

## Shapes read since the first versions

These were handled before this list existed and are covered by the sample project `testdata/pilot` and the fixtures in `testdata/fixtures`: `if` / `else`, `switch` and type switches, `for` and `range` loops (one round per path), `workflow.Selector` with signals, timers, activities and `default`, activities and local activities and child workflows with a checked error, saga compensation in `defer`, `ContinueAsNew`, `workflow.GetVersion` gates, helper functions of the workflow's package, and the signal channel shapes `Receive`, `ReceiveAsync` and `ReceiveWithTimeout`.

## Shapes outside these rows

- **A function that returns the error of a Temporal call directly** (`return fut.Get(ctx, nil)`, `return workflow.Sleep(...)`, `return workflow.Await(...)`): by default it stays one path and the panel lists it as "result returned directly", so a test of either outcome covers it. `classification.direct_returns: "step"` makes it a decision with a failure and a success outcome (this changes the stable ids of the paths through it, which is why it is opt-in).
- **A closure that calls the Temporal SDK and is not a selector callback, a handler or a deferred compensation** (a goroutine started with `workflow.Go`, say): its calls are not steps of any path; the panel lists it as "closure not mapped".
- **Calls PathKit cannot follow into** (an interface method, a function value, a function of another package or module that takes a `workflow.Context`): listed in the panel, with the entry to add to `classification.activity_wrappers` when the function runs an activity.
- **Several Go modules in one folder** (no `go.mod` in the folder itself, or a `go.work`): every module is read, the report is one combined report, each workflow carries its module, and `--module` limits it to one. A module inside the module being read is not read; run PathKit in its folder.
