# Module test cases

Index of the module test cases. Every case maps to one Go test function, as in `docs/tests/core/README.md`. Fixtures for every case are in `docs/tests/core/00-fixtures.md`, which includes the `purchase.Order` and `purchase.Request` rights rows and the request fixtures PR-A and PR-B (CD-247), and the `expense.Claim` and `expense.Advance` rights rows, the Fallback Approver role and the expense test seed (Expense spec §15 items 3 and 6). Every Go test name is unique across these files and `docs/tests/core/`.

Sources: PO spec `docs/specs/2026-10-30-module-purchase-order-spec.md` v0.5 §18 and §26 (area `PO`), hooks spec `docs/specs/2026-10-30-module-docscreen-hooks-spec.md` v0.3 §6 (area `HOOK`), Expense spec `docs/specs/2026-10-30-module-expense-spec.md` v0.5 §17 (area `EXP`), and GRN spec `docs/specs/2026-10-30-module-grn-spec.md` v0.5 §16 (area `GRN`). Plans: `docs/superpowers/plans/2026-10-31-mod-po.md`, `docs/superpowers/plans/2026-10-31-mod-exp.md` and `docs/superpowers/plans/2026-10-31-mod-grn.md`. `scripts/checktestids` reads these files once MOD-0 task M0-6 teaches it to (PO spec §15 item 3).

## Counts

| File | Area | Cases | P1 | P2 | unit | integration | e2e |
|---|---|---|---|---|---|---|---|
| [`po.md`](po.md) | PO — purchase orders and requests | 70 | 69 | 1 | 8 | 60 | 2 |
| [`hook.md`](hook.md) | HOOK — docscreen module hooks | 81 | 81 | 0 | 10 | 63 | 8 |
| [`exp.md`](exp.md) | EXP — expense claims and advances | 67 | 67 | 0 | 5 | 56 | 6 |
| [`grn.md`](grn.md) | GRN — goods receipts | 50 | 50 | 0 | 7 | 40 | 3 |
| **Total** | | **268** | **267** | **1** | **30** | **219** | **19** |

Level counts group variants as in core: "unit (fuzz)" counts as unit, "integration (golden file)" as integration. Every e2e case's Go test starts with `TestE2E_`.

**MOD-GRN exit.** PO-025, PO-031, PO-033 and PO-044 need a real GRN, so they are MOD-GRN's exit criteria, not MOD-PO's (PO spec §18). They are P1 and counted above. MOD-PO's own exit is the other 65 P1 PO cases. MOD-GRN builds PO-031 and PO-044 in its task G3 and PO-025 and PO-033 in G4 (`docs/superpowers/plans/2026-10-31-mod-grn.md`).

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

81 cases: 81 P1 / 0 P2; 10 unit, 63 integration, 8 e2e.

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
| HOOK-019 | `TestDraft_ReaderGetsDocumentPage` | P1 | integration |
| HOOK-020 | `TestDraft_ReaderStepAndLineURLsRedirect` | P1 | integration |
| HOOK-021 | `TestDraft_ReaderRealAuth` | P1 | integration |
| HOOK-022 | `TestModule_CheckCards` | P1 | integration |
| HOOK-023 | `TestModule_PageNotice` | P1 | integration |
| HOOK-024 | `TestModule_PageMore` | P1 | integration |
| HOOK-025 | `TestModule_PagePrimary` | P1 | integration |
| HOOK-026 | `TestModule_StepBodyFallback` | P1 | integration |
| HOOK-027 | `TestModule_StepStay` | P1 | integration |
| HOOK-028 | `TestModule_StepBar` | P1 | integration |
| HOOK-029 | `TestModule_StateLabels` | P1 | integration |
| HOOK-030 | `TestPage_ApproverNeedsYourApproval` | P1 | integration |
| HOOK-031 | `TestApprovalStatus_RolesAndWhy` | P1 | integration |
| HOOK-032 | `TestApp_ApproverLineReal` | P1 | integration |
| HOOK-033 | `TestModule_LineTop` | P1 | integration |
| HOOK-034 | `TestModule_LineNoun` | P1 | integration |
| HOOK-035 | `TestModule_AddTop` | P1 | integration |
| HOOK-036 | `TestLineCard_Receipt` | P1 | unit |
| HOOK-037 | `TestModule_LineCardReceipt` | P1 | integration |
| HOOK-038 | `TestModule_PageFiles` | P1 | integration |
| HOOK-039 | `TestModule_LineBar` | P1 | integration |
| HOOK-040 | `TestFileUpload_AlsoDisable` | P1 | unit |
| HOOK-041 | `TestUploadBrowser_SaveDisabledFocus` | P1 | e2e |
| HOOK-042 | `TestModule_CheckTotalsAfter` | P1 | integration |
| HOOK-043 | `TestModule_StepTop` | P1 | integration |
| HOOK-044 | `TestModule_BodyLineErrorLinks` | P1 | integration |
| HOOK-045 | `TestModule_EnterPrimary` | P1 | integration |
| HOOK-046 | `TestModuleBrowser_EnterPressesPrimary` | P1 | e2e |
| HOOK-047 | `TestLargeQtyInput` | P1 | unit |
| HOOK-048 | `TestLargeQtyInputBrowser_Size` | P1 | e2e |
| HOOK-049 | `TestE2E_Field_EchoKeepsSaveInPlace` | P1 | e2e |
| HOOK-050 | `TestScreens_ListURLsAreAccepted` | P1 | integration |
| HOOK-051 | `TestScreens_FilteredOnlyWithStateFilter` | P1 | integration |
| HOOK-052 | `TestE2E_LineGridFitsLargestAmount` | P1 | e2e |
| HOOK-053 | `TestGalleryBrowser_LineGridWidestRow` | P1 | e2e |
| HOOK-054 | `TestCanFilter_AgreesWithBuild` | P1 | unit |
| HOOK-055 | `TestLineGrid_EveryDocTypeFits` | P1 | integration |
| HOOK-056 | `TestLineGrid_FallsBackToCards` | P1 | integration |
| HOOK-057 | `TestLineGrid_FallbackOnEveryPath` | P1 | integration |
| HOOK-058 | `TestHide46_LinePageSummary` | P1 | integration |
| HOOK-059 | `TestHide46_RefusedWithoutWrite` | P1 | integration |
| HOOK-060 | `TestHide46_DefaultCard` | P1 | integration |
| HOOK-061 | `TestHide46_ReasonPage` | P1 | integration |
| HOOK-062 | `TestHide46_GridAlert` | P1 | integration |
| HOOK-063 | `TestHide46_DefaultCardItem` | P1 | integration |
| HOOK-064 | `TestHide46_DefaultCardLineDeps` | P1 | integration |
| HOOK-065 | `TestHide46_PartyHidden` | P1 | integration |
| HOOK-066 | `TestLineGrid_DisplayCells` | P1 | unit |
| HOOK-067 | `TestLineGrid_DisplayNoForm` | P1 | unit |
| HOOK-068 | `TestLineGrid_DisplayActions` | P1 | unit |
| HOOK-069 | `TestLineGrid_EditableUnchanged` | P1 | unit |
| HOOK-070 | `TestLineGrid_DisplayCellLabels` | P1 | unit |
| HOOK-071 | `TestLineGrid_DisplayTotal` | P1 | unit |
| HOOK-072 | `TestLineGrid_DisplayItemsStep` | P1 | integration |
| HOOK-073 | `TestLineGrid_DisplayCheckPage` | P1 | integration |
| HOOK-074 | `TestLineGrid_DisplayReaderDraft` | P1 | integration |
| HOOK-075 | `TestLineGrid_DisplayApprovedPage` | P1 | integration |
| HOOK-076 | `TestLineGrid_DisplayLineSave` | P1 | integration |
| HOOK-077 | `TestLineGrid_DisplayNoLinePages` | P1 | integration |
| HOOK-078 | `TestGalleryBrowser_DisplayLineGrid` | P1 | e2e |
| HOOK-079 | `TestGalleryBrowser_DisplayLineGridKinds` | P1 | e2e |
| HOOK-080 | `TestScreens_NoLinePagesLeavesOutLineScreens` | P1 | integration |
| HOOK-081 | `TestUnswept_NoLinePagesNeedsNoLineScreens` | P1 | integration |

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

### grn.md — GRN — goods receipts

50 cases: 50 P1 / 0 P2; 7 unit, 40 integration, 3 e2e. GRN-037 to GRN-050 are scenario cases (CD-271, CD-272).

| ID | Go test | P | Level |
|---|---|---|---|
| GRN-001 | `TestGRN_POPicker` | P1 | integration |
| GRN-002 | `TestGRN_PONotVisibleReal` | P1 | integration |
| GRN-003 | `TestGRN_CopiesFromPO` | P1 | integration |
| GRN-004 | `TestGRN_POFixedOnceLines` | P1 | unit |
| GRN-005 | `TestGRN_ForeignLine` | P1 | integration |
| GRN-006 | `TestGRN_DateRules` | P1 | unit |
| GRN-007 | `TestGRN_ChallanRule` | P1 | integration |
| GRN-008 | `TestGRN_RejectRules` | P1 | unit |
| GRN-009 | `TestGRN_AcceptedValue` | P1 | unit |
| GRN-010 | `TestGRN_SubmitApproves` | P1 | integration |
| GRN-011 | `TestGRN_PartialReceipts` | P1 | integration |
| GRN-012 | `TestGRN_RejectedStaysPending` | P1 | integration |
| GRN-013 | `TestGRN_OverDeliveryWarns` | P1 | integration |
| GRN-014 | `TestGRN_OverDeliveryPO` | P1 | integration |
| GRN-015 | `TestGRN_ConcurrentReceipts` | P1 | integration |
| GRN-016 | `TestGRN_AlreadyReceivedCounts` | P1 | integration |
| GRN-017 | `TestGRN_WholeNumber` | P1 | integration |
| GRN-018 | `TestGRN_CancelRestores` | P1 | integration |
| GRN-019 | `TestGRN_CancelClosedPO` | P1 | integration |
| GRN-020 | `TestGRN_ClosedPORefused` | P1 | integration |
| GRN-021 | `TestGRN_BlocksPOCancel` | P1 | integration |
| GRN-022 | `TestGRN_AmendRestrictedReal` | P1 | integration |
| GRN-023 | `TestGRN_PeriodLock` | P1 | integration |
| GRN-024 | `TestGRN_LockRollsBack` | P1 | integration |
| GRN-025 | `TestGRNBill_StateRules` | P1 | integration |
| GRN-026 | `TestGRNBill_VersionAndRights` | P1 | integration |
| GRN-027 | `TestGRNBill_DuplicateWarns` | P1 | integration |
| GRN-028 | `TestGRN_LatePhoto` | P1 | integration |
| GRN-029 | `TestGRN_RestrictedCreateReal` | P1 | integration |
| GRN-030 | `TestGRN_PrintGolden` | P1 | integration (golden file) |
| GRN-031 | `TestGRN_BillsReport` | P1 | integration |
| GRN-032 | `TestGRN_ItemsReport` | P1 | integration |
| GRN-033 | `TestGRN_DisabledMasterStillReceives` | P1 | integration |
| GRN-034 | `TestGRN_DefaultRights` | P1 | unit |
| GRN-035 | `FuzzGRN_Invariants` | P1 | unit (fuzz) |
| GRN-036 | `TestE2E_GRNFlow` | P1 | e2e |
| GRN-037 | `TestGRN_UnapprovedPORefused` | P1 | integration |
| GRN-038 | `TestGRN_FullyRejected` | P1 | integration |
| GRN-039 | `TestGRN_DraftAfterPOClosed` | P1 | integration |
| GRN-040 | `TestGRN_CancelClosedPOUpdatesRequest` | P1 | integration |
| GRN-041 | `TestGRN_AmendOnClosedPO` | P1 | integration |
| GRN-042 | `TestGRN_CancelAmendReturn` | P1 | integration |
| GRN-043 | `TestGRN_TwiceOrderedStop` | P1 | unit |
| GRN-044 | `TestGRN_ReceivedAsOrderedNote` | P1 | integration |
| GRN-045 | `TestGRN_ChallanKind` | P1 | integration |
| GRN-046 | `TestGRNBill_CopyFromInvoice` | P1 | integration |
| GRN-047 | `TestGRN_RestNotComing` | P1 | integration |
| GRN-048 | `TestGRN_EWayHints` | P1 | integration |
| GRN-049 | `TestE2E_RequestToShortClose` | P1 | e2e |
| GRN-050 | `TestE2E_GRNCancelRestores` | P1 | e2e |
