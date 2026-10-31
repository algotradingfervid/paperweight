# Module test cases

Index of the module test cases. Every case maps to one Go test function, as in `docs/tests/core/README.md`. Fixtures for every case are in `docs/tests/core/00-fixtures.md`, which includes the `purchase.Order` and `purchase.Request` rights rows and the request fixtures PR-A and PR-B (CD-247), and the `expense.Claim` and `expense.Advance` rights rows, the Fallback Approver role and the expense test seed (Expense spec §15 items 3 and 6). Every Go test name is unique across these files and `docs/tests/core/`.

Sources: PO spec `docs/specs/2026-10-30-module-purchase-order-spec.md` v0.5 §18 and §26 (area `PO`), hooks spec `docs/specs/2026-10-30-module-docscreen-hooks-spec.md` v0.3 §6 (area `HOOK`), and Expense spec `docs/specs/2026-10-30-module-expense-spec.md` v0.5 §17 (area `EXP`). Plans: `docs/superpowers/plans/2026-10-31-mod-po.md` and `docs/superpowers/plans/2026-10-31-mod-exp.md`. `scripts/checktestids` reads these files once MOD-0 task M0-6 teaches it to (PO spec §15 item 3).

## Counts

| File | Area | Cases | P1 | P2 | unit | integration | e2e |
|---|---|---|---|---|---|---|---|
| [`po.md`](po.md) | PO — purchase orders and requests | 70 | 69 | 1 | 8 | 60 | 2 |
| [`hook.md`](hook.md) | HOOK — docscreen module hooks | 18 | 18 | 0 | 0 | 18 | 0 |
| [`exp.md`](exp.md) | EXP — expense claims and advances | 67 | 67 | 0 | 5 | 56 | 6 |
| **Total** | | **155** | **154** | **1** | **13** | **134** | **8** |

Level counts group variants as in core: "unit (fuzz)" counts as unit, "integration (golden file)" as integration. Every e2e case's Go test starts with `TestE2E_`.

**MOD-GRN exit.** PO-025, PO-031, PO-033 and PO-044 need a real GRN, so they are MOD-GRN's exit criteria, not MOD-PO's (PO spec §18). They are P1 and counted above. MOD-PO's own exit is the other 65 P1 PO cases.

## Test cases by file

### po.md — PO — purchase orders and requests

70 cases: 69 P1 / 1 P2; 8 unit, 60 integration, 2 e2e.

| ID | Go test | P | Level | Exit |
|---|---|---|---|---|
| PO-001 | `TestPO_ReferenceTotals` | P1 | integration |  |
| PO-002 | `TestPO_InterStateSplit` | P1 | integration |  |
| PO-003 | `TestPO_VendorChangeRecomputes` | P1 | integration |  |
| PO-004 | `TestPO_SEZInterState` | P1 | integration |  |
| PO-005 | `TestPO_PlaceReasonShown` | P1 | integration |  |
| PO-006 | `TestPO_LineDiscount` | P1 | unit |  |
| PO-007 | `TestPO_SnapshotsSurviveMasterEdits` | P1 | integration |  |
| PO-008 | `TestPO_VendorRoleRequired` | P1 | integration |  |
| PO-009 | `TestPO_AddressOwner` | P1 | integration |  |
| PO-010 | `TestPO_SiteProjectMismatch` | P1 | integration |  |
| PO-011 | `TestPO_ProjectDefaultsFromSite` | P2 | integration |  |
| PO-012 | `TestPO_RestrictedDraftNeedsProject` | P1 | integration |  |
| PO-013 | `TestPO_ProjectRestrictionReal` | P1 | integration |  |
| PO-014 | `TestPO_SiteRestrictionReal` | P1 | integration |  |
| PO-015 | `TestPO_WholeNumberUoM` | P1 | integration |  |
| PO-016 | `TestPO_QtyUseRegistered` | P1 | integration |  |
| PO-017 | `TestPO_OverseasRefused` | P1 | integration |  |
| PO-018 | `TestPO_DateRules` | P1 | unit |  |
| PO-019 | `TestPO_ApprovalAndNumber` | P1 | integration |  |
| PO-020 | `TestPO_DirectorThreshold` | P1 | integration |  |
| PO-021 | `TestPO_NoRuleAutoApproved` | P1 | integration |  |
| PO-022 | `TestPO_NoSelfApproval` | P1 | integration |  |
| PO-023 | `TestPO_NumberFYFromOrderDate` | P1 | integration |  |
| PO-024 | `TestPO_AmendCopies` | P1 | integration |  |
| PO-025 | `TestPO_CancelBlockedByGRN` | P1 | integration | MOD-GRN exit |
| PO-026 | `TestPO_ReverseChargeTotals` | P1 | integration |  |
| PO-027 | `TestPO_ReverseChargeNeedsTaxable` | P1 | integration |  |
| PO-028 | `TestPO_ReverseChargePrint` | P1 | integration |  |
| PO-029 | `TestPO_PendingFormula` | P1 | unit |  |
| PO-030 | `TestPO_ReceiptStatus` | P1 | integration |  |
| PO-031 | `TestPO_OpeningSurvivesRollUp` | P1 | integration | MOD-GRN exit |
| PO-032 | `TestPO_AlreadyReceivedRules` | P1 | unit |  |
| PO-033 | `TestPO_CloseStopsReceipts` | P1 | integration | MOD-GRN exit |
| PO-034 | `TestPO_StaleVersion` | P1 | integration |  |
| PO-035 | `TestPO_PrintGolden` | P1 | integration (golden file) |  |
| PO-036 | `TestPO_PrintRights` | P1 | integration |  |
| PO-037 | `TestPO_PendingReport` | P1 | integration |  |
| PO-038 | `TestPO_PendingExportRight` | P1 | integration |  |
| PO-039 | `TestSettings_AdminOnlyAudited` | P1 | integration |  |
| PO-040 | `TestPO_CutoverCounter` | P1 | integration |  |
| PO-041 | `TestPO_DefaultRights` | P1 | unit |  |
| PO-042 | `TestApprovalWaiting_Days` | P1 | integration |  |
| PO-043 | `TestApprovalOverdue_List` | P1 | integration |  |
| PO-044 | `FuzzPO_ReceiptInvariants` | P1 | unit (fuzz) | MOD-GRN exit |
| PO-045 | `TestE2E_PurchaseOrderFlow` | P1 | e2e |  |
| PO-046 | `TestPO_BackEntry` | P1 | integration |  |
| PO-047 | `TestPO_AmendFillsSnapshots` | P1 | integration |  |
| PO-048 | `TestRequest_SubmitNumbers` | P1 | integration |  |
| PO-049 | `TestRequest_SiteRestrictionReal` | P1 | integration |  |
| PO-050 | `TestRequest_Rights` | P1 | integration |  |
| PO-051 | `TestRequest_CreatePO` | P1 | integration |  |
| PO-052 | `TestRequest_AddFromRequests` | P1 | integration |  |
| PO-053 | `TestPO_RequestRowRules` | P1 | integration |  |
| PO-054 | `TestPO_RequestRowRace` | P1 | integration |  |
| PO-055 | `TestRequest_OrderStatus` | P1 | integration |  |
| PO-056 | `TestRequest_CancelBlocked` | P1 | integration |  |
| PO-057 | `FuzzRequest_OrderInvariants` | P1 | unit (fuzz) |  |
| PO-058 | `TestPO_OwnerOnlyReal` | P1 | integration |  |
| PO-059 | `TestPO_HandOver` | P1 | integration |  |
| PO-060 | `TestPO_OwnerBanner` | P1 | integration |  |
| PO-061 | `TestComments_Rules` | P1 | integration |  |
| PO-062 | `TestComments_Panel` | P1 | integration |  |
| PO-063 | `TestPO_MarkSent` | P1 | integration |  |
| PO-064 | `TestPO_Copy` | P1 | integration |  |
| PO-065 | `TestPO_LastPrice` | P1 | integration |  |
| PO-066 | `TestPurchaseHome_Cards` | P1 | integration |  |
| PO-067 | `TestRequest_DefaultRights` | P1 | unit |  |
| PO-068 | `TestE2E_RequestToSentFlow` | P1 | e2e |  |
| PO-069 | `TestRequest_CloseWhileHeld` | P1 | integration |  |
| PO-070 | `TestPurchaseHome_Queues` | P1 | integration |  |

### hook.md — HOOK — docscreen module hooks

18 cases: 18 P1 / 0 P2; 0 unit, 18 integration, 0 e2e.

| ID | Go test | P | Level |
|---|---|---|---|
| HOOK-001 | `TestModule_NoneUnchanged` | P1 | integration |
| HOOK-002 | `TestModule_StepBody` | P1 | integration |
| HOOK-003 | `TestModule_StepSave` | P1 | integration |
| HOOK-004 | `TestModule_FirstStepCreatesReal` | P1 | integration |
| HOOK-005 | `TestModule_SaveErrors` | P1 | integration |
| HOOK-006 | `TestModule_Conflict` | P1 | integration |
| HOOK-007 | `TestModule_SaveError` | P1 | integration |
| HOOK-008 | `TestModule_LinesKept` | P1 | integration |
| HOOK-009 | `TestModule_LinePage` | P1 | integration |
| HOOK-010 | `TestModule_HideKeepsValue` | P1 | integration |
| HOOK-011 | `TestModule_CheckExtra` | P1 | integration |
| HOOK-012 | `TestModule_PageExtra` | P1 | integration |
| HOOK-013 | `TestModule_ReasonExtra` | P1 | integration |
| HOOK-014 | `TestModule_Navigation` | P1 | integration |
| HOOK-015 | `TestModule_NoNestedTx` | P1 | integration |
| HOOK-016 | `TestModule_LineIDs` | P1 | integration |
| HOOK-017 | `TestModule_ClearLines` | P1 | integration |
| HOOK-018 | `TestModule_LineFlags` | P1 | integration |

### exp.md — EXP — expense claims and advances

67 cases: 67 P1 / 0 P2; 5 unit, 56 integration, 6 e2e. EXP-042 onward come from the ERP research (CD-271).

| ID | Go test | P | Level |
|---|---|---|---|
| EXP-001 | `TestExp_EmployeeRules` | P1 | integration |
| EXP-002 | `TestExp_AdminEntersForOther` | P1 | integration |
| EXP-003 | `TestExp_EmployeeAndLeavers` | P1 | integration |
| EXP-004 | `TestExp_Totals` | P1 | unit |
| EXP-005 | `TestExp_DateRules` | P1 | unit |
| EXP-006 | `TestExp_ReceiptOrReason` | P1 | integration |
| EXP-007 | `TestExp_ReceiptLinks` | P1 | integration |
| EXP-008 | `TestExp_AmendKeepsReceipts` | P1 | integration |
| EXP-009 | `TestExp_ReceiptCheckSameTx` | P1 | integration |
| EXP-010 | `TestExp_ReceiptPolicy` | P1 | integration |
| EXP-011 | `TestExp_GSTFields` | P1 | integration |
| EXP-012 | `TestExp_GSTAboveAmount` | P1 | unit |
| EXP-013 | `TestExp_TypeDefaults` | P1 | integration |
| EXP-014 | `TestExp_DuplicateWarnings` | P1 | integration |
| EXP-015 | `TestExp_ManagerApproves` | P1 | integration |
| EXP-016 | `TestExp_Fallback` | P1 | integration |
| EXP-017 | `TestExp_RestrictedManagerReal` | P1 | integration |
| EXP-018 | `TestExp_TeamScopeReal` | P1 | integration |
| EXP-019 | `TestExp_RejectRevise` | P1 | integration |
| EXP-020 | `TestExp_CloseRight` | P1 | integration |
| EXP-021 | `TestExp_ClosedIsFinal` | P1 | integration |
| EXP-022 | `TestDocType_CloseRightParity` | P1 | integration |
| EXP-023 | `TestExp_PeriodLock` | P1 | integration |
| EXP-024 | `TestAdv_Lifecycle` | P1 | integration |
| EXP-025 | `TestAdv_ClaimBelowAdvance` | P1 | integration |
| EXP-026 | `TestAdv_ClaimAboveAdvance` | P1 | integration |
| EXP-027 | `TestAdv_ConcurrentClaims` | P1 | integration |
| EXP-028 | `TestAdv_CancelRestores` | P1 | integration |
| EXP-029 | `TestAdv_CancelBlocked` | P1 | integration |
| EXP-030 | `TestAdv_Sources` | P1 | integration |
| EXP-031 | `TestAdv_Rights` | P1 | integration |
| EXP-032 | `TestExp_PrintGolden` | P1 | integration (golden file) |
| EXP-033 | `TestExp_PrintRights` | P1 | integration |
| EXP-034 | `TestExp_OpenReports` | P1 | integration |
| EXP-035 | `TestExp_LinesReport` | P1 | integration |
| EXP-036 | `TestExp_DefaultRights` | P1 | unit |
| EXP-037 | `FuzzExp_Invariants` | P1 | unit (fuzz) |
| EXP-038 | `TestE2E_ExpenseFlow` | P1 | e2e |
| EXP-039 | `TestExp_DirectorClaim` | P1 | integration |
| EXP-040 | `TestExp_OnlyFallback` | P1 | integration |
| EXP-041 | `TestExp_FallbackLeaves` | P1 | integration |
| EXP-042 | `TestExp_AmountPositive` | P1 | integration |
| EXP-043 | `TestAdv_FullyUsedNotOffered` | P1 | integration |
| EXP-044 | `TestAdv_NotApprovedRefused` | P1 | integration |
| EXP-045 | `TestAdv_LeftForOneClaim` | P1 | integration |
| EXP-046 | `TestAdv_ClosedWhileClaimPending` | P1 | integration |
| EXP-047 | `TestAdv_DraftClaimDoesNotBlockCancel` | P1 | integration |
| EXP-048 | `TestAdv_RejectedClaimRechecks` | P1 | integration |
| EXP-049 | `TestAdv_AmendKeepsAdvance` | P1 | integration |
| EXP-050 | `TestExp_ManagerDisabledFallback` | P1 | integration |
| EXP-051 | `TestAdv_DirectorAdvance` | P1 | integration |
| EXP-052 | `TestExp_LinesReportStates` | P1 | integration |
| EXP-053 | `TestExp_MiddleManagerOwnClaim` | P1 | integration |
| EXP-054 | `TestAdv_MarkAsPaid` | P1 | integration |
| EXP-055 | `TestAdv_OnlyPaidSetOff` | P1 | integration |
| EXP-056 | `TestExp_DuplicateWithinClaim` | P1 | integration |
| EXP-057 | `TestExp_DuplicateInvoice` | P1 | integration |
| EXP-058 | `TestExp_BilledTo` | P1 | integration |
| EXP-059 | `TestAdv_ClosePageShowsLeft` | P1 | integration |
| EXP-060 | `TestAdv_OtherAdvanceWarning` | P1 | integration |
| EXP-061 | `TestExp_TimeBarWarning` | P1 | integration |
| EXP-062 | `TestExp_BackEntryWarnings` | P1 | integration |
| EXP-063 | `TestE2E_AdvanceTwoClaimsClose` | P1 | e2e |
| EXP-064 | `TestE2E_ClaimAboveAdvance` | P1 | e2e |
| EXP-065 | `TestE2E_DirectorClaimFallback` | P1 | e2e |
| EXP-066 | `TestE2E_RejectReviseReceipt` | P1 | e2e |
| EXP-067 | `TestE2E_BuyerCashClaim` | P1 | e2e |
