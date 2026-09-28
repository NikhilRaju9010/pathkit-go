# Pilot answer key

This is the hand-written answer key for the pilot project, written in M1 **before** PathKit could analyze anything. Later milestones check the real tool against it:

- **M2 (`analyze`):** each workflow's path count, junctions, exits and end kinds must match.
- **M3/M4 (`pathkit test`):** each test's trace must land on the path listed under "Tests".
- **M6/M7 (`coverage`/`report`):** the covered counts and the project total must match.

The exact wording PathKit prints is decided in M2. What must match is the **number of paths, the junctions, their exits and the end kind**. If the real tool disagrees, don't quietly change this file. Add a note under "Disagreements" at the bottom saying who was wrong and why.

**How to read it.** `J1`, `J2`, … are junctions (decision points) in source order. "Err check" means an `if err != nil` right after a Temporal call. It is a junction with exits `failure` / `success` (design decision D2). "Transparent" means an `if` that PathKit deliberately does not count as a junction.

## Workflows PathKit must find (and must not find)

Found (8): `orders.OrderWorkflow`, `approval.ApprovalWorkflow`, `polling.ReportPollingWorkflow`, `shipment.ShipmentWorkflow`, `fulfillment.OrderFulfillmentWorkflow`, `fulfillment.PaymentWorkflow`, `billing.SubscriptionWorkflow`, `reports.DailyReportWorkflow`.

Not found, even though they take a `workflow.Context`:
- `fulfillment.newChildCtx`, because it's unexported.
- `fulfillment.AuditLog`, because it has no `error` result.

Not needed: the `RegisterWorkflow` calls in `cmd/worker/main.go` (detection doesn't use them).

---

## 1. `orders.OrderWorkflow` (orders/orders.go)

Junctions: J1 `if in.AmountCents <= 0`, J2 err check after `ChargeCard`.
Transparent: the `if err != nil` after `parseSKU` (not a Temporal call).

**3 paths:**
1. J1 --true--> End (completed)
2. J1 --false--> J2 ChargeCard --failure--> End (failed)
3. J1 --false--> J2 ChargeCard --success--> End (completed)

**Tests (orders_test.go), 3/3 covered:** `TestOrderRejected` → 1, `TestOrderChargeFails` → 2, `TestOrderCharged` → 3.

## 2. `approval.ApprovalWorkflow` (approval/approval.go)

Junctions:
- J1: err check after `AwaitWithTimeout` (fails only on cancellation).
- J2: `if !ok` (the AwaitWithTimeout result; exits `timeout` / `signaled`).
- J3: `if decision == "approved"`.

Transparent: the `if err != nil` after `SetQueryHandler` (not a Temporal call in D2's list).
Not junctions: the `status` query handler, the `workflow.Go` coroutine, and the blocking signal `Receive`.

**4 paths:**
1. J1 AwaitWithTimeout --failure--> End (failed)
2. J1 --success--> J2 `if !ok` --timeout--> End (completed)
3. J1 --success--> J2 --signaled--> J3 --true--> End (completed)
4. J1 --success--> J2 --signaled--> J3 --false--> End (completed)

**Tests (approval_test.go), 2/4 covered:** `TestApprovalApproved` → 3, `TestApprovalTimesOut` → 2. Not covered: 1 (cancelled), 4 (rejected).

## 3. `polling.ReportPollingWorkflow` (polling/polling.go)

Junctions:
- J1: the `for attempt := 1; attempt <= in.MaxPolls; attempt++` loop (exits `iterate` / `exit`, back-edge `retry`).
- J2: err check after `CheckStatus`.
- J3: `switch status`. No `default` is written, so an implicit `default` exit is added.

Loop rule (defined once, in CLAUDE.md's D3 table, loop row): *each loop edge appears at most once on a listed path; a trace with several trips is folded by keeping only the last trip.*

**5 paths:**
1. J1 --exit--> End (completed), zero polls
2. J1 --iterate--> J2 CheckStatus --failure--> End (failed)
3. J1 --iterate--> J2 --success--> J3 --case "complete"--> End (completed)
4. J1 --iterate--> J2 --success--> J3 --case "failed"--> End (completed)
5. J1 --iterate--> J2 --success--> J3 --default--> retry --> J1 --exit--> End (completed)

**Tests (polling_test.go), 3/5 covered:**
- `TestPollingCompleteFirstTime` → 3
- `TestPollingJobFailed` → 4
- `TestPollingPendingThenComplete` → 3 (again, once the loop is collapsed)
- `TestPollingGivesUp` → 5

Not covered: 1, 2.

## 4. `shipment.ShipmentWorkflow` (shipment/shipment.go)

Junctions:
- J1: err check after `CreateLabel`.
- J2: `sel.Select(ctx)`. Exits: `signal "delivery-update"` from `AddReceive`, and `timeout` from `AddFuture` on a `workflow.NewTimer`.
- J3: `switch u.Status` inside the signal callback. It has cases `"delivered"` and `"returned"`, plus a written `default`.
- J4: err check after `NotifyCustomer`.

**9 paths:**
1. J1 CreateLabel --failure--> End (failed)
2. J1 --success--> J2 --signal "delivery-update"--> J3 --case "delivered"--> J4 --failure--> End (failed)
3. J1 --success--> J2 --signal "delivery-update"--> J3 --case "delivered"--> J4 --success--> End (completed)
4. J1 --success--> J2 --signal "delivery-update"--> J3 --case "returned"--> J4 --failure--> End (failed)
5. J1 --success--> J2 --signal "delivery-update"--> J3 --case "returned"--> J4 --success--> End (completed)
6. J1 --success--> J2 --signal "delivery-update"--> J3 --default--> J4 --failure--> End (failed)
7. J1 --success--> J2 --signal "delivery-update"--> J3 --default--> J4 --success--> End (completed)
8. J1 --success--> J2 --timeout--> J4 --failure--> End (failed)
9. J1 --success--> J2 --timeout--> J4 --success--> End (completed)

**Tests (shipment_test.go), 3/9 covered:** `TestShipmentDelivered` → 3, `TestShipmentTimerFiresLost` → 9, `TestShipmentLabelFails` → 1.

This is the example where branch coverage (M6) should read much higher than path coverage.

## 5. `fulfillment.OrderFulfillmentWorkflow` (fulfillment/fulfillment.go)

Junctions:
- J1: err check after `ReserveInventory`.
- J2: err check after the `PaymentWorkflow` child (`ExecuteChildWorkflow`).
- J3: err check after `ShipOrder`.

The `defer` compensation (`ReleaseInventory` if `err != nil`) is **noted, not a junction** (D3). The note appears on every path that passes the `defer` statement.

**4 paths:**
1. J1 ReserveInventory --failure--> End (failed). No compensation note, because the `defer` isn't registered yet.
2. J1 --success--> J2 child PaymentWorkflow --failure--> End (failed) [compensation (defer)]
3. J1 --success--> J2 --success--> J3 ShipOrder --failure--> End (failed) [compensation (defer)]
4. J1 --success--> J2 --success--> J3 --success--> End (completed) [compensation (defer)]. The inner `if` skips here, but v1 only notes that the defer exists.

**Tests (fulfillment_test.go), 2/4 covered:** `TestFulfillmentPaymentFailsReleasesStock` → 2, `TestFulfillmentSucceeds` → 4. Not covered: 1, 3.

## 6. `fulfillment.PaymentWorkflow` (fulfillment/payment.go)

Junctions:
- J1: err check after `AuthorizeCard`.
- J2: `if o.AmountCents > 100_000`.
- J3: err check after `FraudReview`.

**4 paths:**
1. J1 AuthorizeCard --failure--> End (failed)
2. J1 --success--> J2 --false--> End (completed)
3. J1 --success--> J2 --true--> J3 FraudReview --failure--> End (failed)
4. J1 --success--> J2 --true--> J3 --success--> End (completed)

**Tests (payment_test.go), 2/4 covered:** `TestPaymentSmallOrder` → 2, `TestPaymentBigOrderPassesReview` → 4. The parent's tests mock this child with `OnWorkflow`, so they record nothing for it.

## 7. `billing.SubscriptionWorkflow` (billing/subscription.go)

Junctions: J1 the `for i := 0; i < s.CyclesPerRun; i++` loop, and J2 the err check after `ChargeMonthly`. The final `return workflow.NewContinueAsNewError(...)` is an end kind, not a junction.

**3 paths:**
1. J1 --exit--> End (continued-as-new)
2. J1 --iterate--> J2 ChargeMonthly --failure--> End (failed)
3. J1 --iterate--> J2 --success--> retry --> J1 --exit--> End (continued-as-new)

**Tests (subscription_test.go), 2/3 covered:** `TestSubscriptionContinuesAsNew` → 3, `TestSubscriptionChargeFails` → 2. Not covered: 1.

## 8. `reports.DailyReportWorkflow` (reports/dailyreport.go)

Run daily at 06:00 by the Schedule in `cmd/worker/main.go` (cron `0 6 * * *`).

Junctions:
- J1: `if workflow.HasLastCompletionResult(ctx)`.
- J2: err check after `BuildReport`.
- J3: err check after `EmailReport`.

Transparent: `if err := workflow.GetLastCompletionResult(...); err == nil`. It isn't a Temporal call in D2's list. Its "no error" side is the **true** side, so PathKit must walk *into* that `if` body.

**6 paths:**
1. J1 --true--> J2 BuildReport --failure--> End (failed)
2. J1 --true--> J2 --success--> J3 EmailReport --failure--> End (failed)
3. J1 --true--> J2 --success--> J3 --success--> End (completed)
4. J1 --false--> J2 --failure--> End (failed)
5. J1 --false--> J2 --success--> J3 --failure--> End (failed)
6. J1 --false--> J2 --success--> J3 --success--> End (completed)

**Tests (dailyreport_test.go), 3/6 covered:** `TestDailyReportLaterRunStartsFromLastReport` → 3, `TestDailyReportFirstRun` → 6, `TestDailyReportBuildFails` → 4.

---

## Totals

| Workflow | Paths | Covered by tests |
| --- | --- | --- |
| orders.OrderWorkflow | 3 | 3 |
| approval.ApprovalWorkflow | 4 | 2 |
| polling.ReportPollingWorkflow | 5 | 3 |
| shipment.ShipmentWorkflow | 9 | 3 |
| fulfillment.OrderFulfillmentWorkflow | 4 | 2 |
| fulfillment.PaymentWorkflow | 4 | 2 |
| billing.SubscriptionWorkflow | 3 | 2 |
| reports.DailyReportWorkflow | 6 | 3 |
| **Total** | **38** | **20 (52.6%)** |

## Disagreements

*(None yet. Add an entry whenever the real tool and this key differ: the workflow, what the tool said, and which one was wrong and why.)*
