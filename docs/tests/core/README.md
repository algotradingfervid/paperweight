# Core test cases

Index of the core platform test cases. Every case maps to one Go test function (spec §8). Fixtures for every case are in `00-fixtures.md`; cross-area rules R1–R8 are in `docs/specs/2026-10-24-core-platform-spec.md` §3.1.

Reconciled with the spec on 24 October 2026 and regenerated after contract decisions CD-01 to CD-83 (`docs/specs/contract-changes.md`). Every Go test name is unique across the seven files.

## Counts

| File | Area | Cases | P1 | P2 | unit | integration | e2e |
|---|---|---|---|---|---|---|---|
| [`01-auth.md`](01-auth.md) | AUTH — identity and permissions | 121 | 92 | 29 | 44 | 76 | 1 |
| [`02-naming.md`](02-naming.md) | NAM — naming series | 65 | 48 | 17 | 20 | 44 | 1 |
| [`03-document.md`](03-document.md) | DOC — document lifecycle and audit | 106 | 76 | 30 | 23 | 79 | 4 |
| [`04-approvals.md`](04-approvals.md) | APR — approvals | 97 | 72 | 25 | 23 | 72 | 2 |
| [`05-files-print.md`](05-files-print.md) | FILE — files and printing | 83 | 67 | 16 | 28 | 48 | 7 |
| [`06-masters.md`](06-masters.md) | MST — masters | 129 | 96 | 33 | 71 | 58 | 0 |
| [`07-numbers-lists.md`](07-numbers-lists.md) | NUM — numbers, lists and CSV | 121 | 105 | 16 | 70 | 49 | 2 |
| **Total** | | **722** | **556** | **166** | **279** | **426** | **17** |

Level counts group variants: "unit (fuzz)" counts as unit, "integration (golden file)" as integration. Every e2e case's Go test starts with `TestE2E_` and is owned by WP3 (CD-40, CD-74). A Go test written `TestFunc/sub` names a subtest of `TestFunc`.

## Test cases by file


### 01-auth.md — AUTH — identity and permissions

121 cases: 92 P1 / 29 P2; 44 unit, 76 integration, 1 e2e.

| ID | Go test | P | Level |
|---|---|---|---|
| AUTH-001 | `TestLogin_ValidCredentials` | P1 | integration |
| AUTH-002 | `TestLogin_EmailCaseAndSpaceInsensitive` | P1 | unit |
| AUTH-003 | `TestLogin_WrongPassword` | P1 | integration |
| AUTH-004 | `TestLogin_UnknownEmailSameResponse` | P1 | integration |
| AUTH-005 | `TestLogin_UnknownUserSpendsHashTime` | P1 | unit |
| AUTH-006 | `TestLogin_IncompleteDetails` | P1 | unit |
| AUTH-007 | `TestLogin_DisabledUserCorrectPassword` | P1 | integration |
| AUTH-008 | `TestLogin_DisabledUserWrongPasswordGeneric` | P1 | unit |
| AUTH-009 | `TestLockout_LocksAfterFiveFailures` | P1 | integration |
| AUTH-010 | `TestLockout_UnlocksAfterWindow` | P1 | unit |
| AUTH-011 | `TestLockout_SuccessResetsCounter` | P1 | unit |
| AUTH-012 | `TestLockout_CounterAlwaysExpires` | P1 | unit |
| AUTH-013 | `TestLockout_ConcurrentFailuresCounted` | P1 | integration |
| AUTH-014 | `TestLockout_UnknownEmailIndistinguishable` | P1 | integration |
| AUTH-015 | `TestLockout_PerIPBlock` | P2 | integration |
| AUTH-016 | `TestLockout_AdminUnlock` | P2 | integration |
| AUTH-017 | `TestLogin_OversizedPasswordSameError` | P1 | unit |
| AUTH-018 | `TestLogin_NextParamLocalOnly` | P1 | unit |
| AUTH-019 | `TestAuthLog_NoSecrets` | P2 | integration |
| AUTH-020 | `TestPassword_StoredAsArgon2id` | P1 | unit |
| AUTH-021 | `TestPassword_UniqueSalt` | P1 | unit |
| AUTH-022 | `TestPassword_OldPasswordRejectedAfterChange` | P1 | integration |
| AUTH-023 | `TestPassword_RehashOnLogin` | P2 | unit |
| AUTH-024 | `TestPassword_UnicodeNFC` | P2 | unit |
| AUTH-025 | `TestPasswordPolicy_MinLength` | P1 | unit |
| AUTH-026 | `TestPasswordPolicy_MaxLength` | P1 | unit |
| AUTH-027 | `TestPasswordPolicy_CommonList` | P1 | unit |
| AUTH-028 | `TestPasswordPolicy_NotContainingEmail` | P2 | unit |
| AUTH-029 | `TestPasswordPolicy_NotSameAsCurrent` | P1 | unit |
| AUTH-030 | `TestChangePassword_RequiresCurrent` | P1 | integration |
| AUTH-031 | `TestChangePassword_ConfirmMismatch` | P1 | unit |
| AUTH-032 | `TestChangePassword_EndsOtherSessions` | P1 | integration |
| AUTH-033 | `TestAdminResetPassword_ForcesChange` | P1 | integration |
| AUTH-034 | `TestAdminResetPassword_AdminOnly` | P1 | integration |
| AUTH-035 | `TestSession_TokenHashedAtRest` | P1 | unit |
| AUTH-036 | `TestSession_HashNotReplayable` | P1 | unit |
| AUTH-037 | `TestSession_IdleExpiry` | P1 | unit |
| AUTH-038 | `TestSession_AbsoluteExpiry` | P2 | unit |
| AUTH-039 | `TestSession_ExpiredHTMXRedirect` | P1 | integration |
| AUTH-040 | `TestLogout_DeletesSession` | P1 | integration |
| AUTH-041 | `TestLogout_PostOnly` | P2 | integration |
| AUTH-042 | `TestDisableUser_EndsSessions` | P1 | integration |
| AUTH-043 | `TestSession_DeletedUserInvalid` | P1 | unit |
| AUTH-044 | `TestSessions_UserRevokesOthers` | P2 | integration |
| AUTH-045 | `TestSessions_MultipleAllowed` | P2 | integration |
| AUTH-046 | `TestCSRF_RequiredOnUnsafeMethods` | P1 | integration |
| AUTH-047 | `TestCSRF_LoginForm` | P2 | integration |
| AUTH-048 | `TestSession_CookieAttributes` | P1 | unit |
| AUTH-049 | `TestBootstrap_CLIFirstAdmin` | P1 | integration |
| AUTH-050 | `TestBootstrap_SetupOnceRaceFree` | P1 | integration |
| AUTH-051 | `TestUser_EmailValidUnique` | P1 | unit |
| AUTH-052 | `TestUser_DefaultRoleAndNoRoleWarning` | P2 | integration |
| AUTH-053 | `TestUser_CannotDisableOrDemoteSelf` | P1 | unit |
| AUTH-054 | `TestUser_AtLeastOneAdmin` | P1 | unit |
| AUTH-055 | `TestUser_DeleteOnlyIfUnreferenced` | P1 | integration |
| AUTH-056 | `TestUser_DisabledStillShownOnHistory` | P2 | integration |
| AUTH-057 | `TestUser_SelfEditLimitedFields` | P1 | integration |
| AUTH-058 | `TestRoles_OnlyAdminAssigns` | P1 | integration |
| AUTH-059 | `TestUser_EmailChangeKeepsIdentity` | P2 | integration |
| AUTH-060 | `TestRoles_ChangeAppliesNextRequest` | P1 | integration |
| AUTH-061 | `TestRoles_DeleteBlockedWhileAssigned` | P2 | unit |
| AUTH-062 | `TestRoles_UniqueAndAdminFixed` | P2 | unit |
| AUTH-063 | `TestRights_UnionAcrossRoles` | P1 | unit |
| AUTH-064 | `TestRights_NoRoleNoAccess` | P1 | integration |
| AUTH-065 | `TestMatrix_ReadRequired` | P1 | unit |
| AUTH-066 | `TestMatrix_InapplicableRightsRejected` | P1 | unit |
| AUTH-067 | `TestMatrix_SeedEditReset` | P2 | integration |
| AUTH-068 | `TestMatrix_ChangesAudited` | P2 | integration |
| AUTH-069 | `TestRights_AdminAll` | P1 | unit |
| AUTH-070 | `TestRights_AdminCannotBypassLifecycle` | P1 | integration |
| AUTH-071 | `TestRights_Read` | P1 | integration |
| AUTH-072 | `TestRights_Create` | P1 | integration |
| AUTH-073 | `TestRights_Write` | P1 | integration |
| AUTH-074 | `TestRights_Submit` | P1 | integration |
| AUTH-075 | `TestRights_Approve` | P1 | integration |
| AUTH-076 | `TestRights_Cancel` | P1 | integration |
| AUTH-077 | `TestRights_Amend` | P1 | integration |
| AUTH-078 | `TestRights_Delete` | P1 | integration |
| AUTH-079 | `TestRights_Print` | P1 | integration |
| AUTH-080 | `TestRights_Export` | P1 | integration |
| AUTH-081 | `TestRights_Import` | P1 | integration |
| AUTH-082 | `TestRights_LinesFollowParent` | P1 | integration |
| AUTH-083 | `TestRights_OwnerNotClientSettable` | P1 | unit |
| AUTH-084 | `TestE2E_ButtonsMatchRights` | P2 | e2e |
| AUTH-085 | `TestOwnScope_ListFiltered` | P1 | integration |
| AUTH-086 | `TestOwnScope_OthersNotFound` | P1 | integration |
| AUTH-087 | `TestOwnScope_WriteDeleteOwnOnly` | P1 | integration |
| AUTH-088 | `TestOwnScope_LinkedEmployee` | P1 | integration |
| AUTH-089 | `TestOwnScope_AllWins` | P1 | unit |
| AUTH-090 | `TestOwnScope_ExportFiltered` | P1 | integration |
| AUTH-091 | `TestRestriction_SiteList` | P1 | integration |
| AUTH-092 | `TestRestriction_ProjectList` | P1 | integration |
| AUTH-093 | `TestRestriction_StrictEmptyHidden` | P1 | integration |
| AUTH-094 | `TestRestriction_ChildLinksChecked` | P1 | integration |
| AUTH-095 | `TestRestriction_DimensionsAnd` | P1 | unit |
| AUTH-096 | `TestRestriction_DoesNotGrant` | P1 | unit |
| AUTH-097 | `TestRestriction_AndOwnScope` | P2 | unit |
| AUTH-098 | `TestRestriction_LinkPickerFiltered` | P1 | integration |
| AUTH-099 | `TestRestriction_SaveDisallowedValue` | P1 | integration |
| AUTH-100 | `TestRestriction_SingleValueDefault` | P2 | integration |
| AUTH-101 | `TestRestriction_LinkedDocumentMustBeVisible` | P1 | integration |
| AUTH-102 | `TestRestriction_MasterSelf` | P2 | integration |
| AUTH-103 | `TestRestriction_AdminOnly` | P1 | integration |
| AUTH-104 | `TestRestriction_ClearLifts` | P2 | integration |
| AUTH-105 | `TestRestriction_DisabledMasterKept` | P2 | unit |
| AUTH-106 | `TestScope_AllChannelsConsistent` | P1 | integration |
| AUTH-107 | `TestRoutes_AllDeclarePermission` | P1 | unit |
| AUTH-108 | `TestScope_IDGuessingNoLeak` | P1 | integration |
| AUTH-109 | `TestScope_AttachmentsFollowParent` | P1 | integration |
| AUTH-110 | `TestScope_FiltersCannotWiden` | P1 | integration |
| AUTH-111 | `TestScope_ReportTotals` | P2 | integration |
| AUTH-112 | `TestScope_APIEqualsUI` | P1 | integration |
| AUTH-113 | `FuzzScope_CanMatchesSQL` | P2 | unit |
| AUTH-114 | `TestDenial_Messages` | P2 | unit |
| AUTH-115 | `TestGuest_OnlyPublicRoutes` | P1 | integration |
| AUTH-116 | `TestTeamScope_DirectReportVisibleAndApprovable` | P1 | integration |
| AUTH-117 | `TestTeamScope_IndirectReportExcluded` | P1 | integration |
| AUTH-118 | `TestTeamScope_LeftEmployeeIgnored` | P1 | integration |
| AUTH-119 | `TestMatrix_TeamOnlyWithEmployeeField` | P1 | unit |
| AUTH-120 | `TestWhoCan_RolesAndUsers` | P2 | integration |
| AUTH-121 | `TestAccessEditor_ShowsAddsRemoves` | P2 | integration |

### 02-naming.md — NAM — naming series

65 cases: 48 P1 / 17 P2; 20 unit, 44 integration, 1 e2e.

| ID | Go test | P | Level |
|---|---|---|---|
| NAM-001 | `TestTemplate_DefaultsRender` | P1 | unit |
| NAM-002 | `TestRender_FYToken` | P1 | unit |
| NAM-003 | `TestRender_CalendarTokens` | P1 | unit |
| NAM-004 | `TestAssign_UsesDocumentDateNotClock` | P1 | integration |
| NAM-005 | `TestAssign_FYReset` | P1 | integration |
| NAM-006 | `TestAssign_BackdatedUsesOwnFY` | P1 | integration |
| NAM-007 | `TestAssign_NoDateTokenNeverResets` | P2 | integration |
| NAM-008 | `TestAssign_MonthlyPrefixResets` | P2 | integration |
| NAM-009 | `TestTemplate_Validate/missing_number` | P1 | unit |
| NAM-010 | `TestTemplate_Validate/two_numbers` | P1 | unit |
| NAM-011 | `TestTemplate_Validate/number_not_last` | P1 | unit |
| NAM-012 | `TestTemplate_Validate/padding_bounds` | P1 | unit |
| NAM-013 | `TestTemplate_Validate/unknown_token` | P1 | unit |
| NAM-014 | `TestTemplate_Validate/bad_characters` | P1 | unit |
| NAM-015 | `TestTemplate_Validate/first_char` | P1 | unit |
| NAM-016 | `TestTemplate_Validate/braces` | P2 | unit |
| NAM-017 | `TestTemplate_Validate/max_length` | P1 | unit |
| NAM-018 | `TestTemplate_Validate/valid` | P1 | unit |
| NAM-019 | `TestTemplate_Prefix` | P1 | unit |
| NAM-020 | `FuzzRender_GSTSafe` | P1 | unit |
| NAM-021 | `TestValidateGSTDocNumber` | P1 | unit |
| NAM-022 | `TestAssign_DraftHasNoNumber` | P1 | integration |
| NAM-023 | `TestAssign_OnApproval` | P1 | integration |
| NAM-024 | `TestAssign_DirectApprove` | P1 | integration |
| NAM-025 | `TestAssign_RejectedThenApproved` | P1 | integration |
| NAM-026 | `TestAssign_DeleteDraftNoGap` | P1 | integration |
| NAM-027 | `TestAssign_ApprovalOrder` | P1 | integration |
| NAM-028 | `TestAssign_PerDocTypeCounters` | P1 | integration |
| NAM-029 | `TestAssign_NumberStableAcrossTransitions` | P1 | integration |
| NAM-030 | `TestCounter_UpsertIncrement` | P1 | unit |
| NAM-031 | `TestAssign_RollbackNoGap` | P1 | integration |
| NAM-032 | `TestCounter_RequiresTx` | P2 | unit |
| NAM-033 | `TestAssign_ConcurrentApprovals` | P1 | integration |
| NAM-034 | `TestAssign_BusyTimeout` | P2 | integration |
| NAM-035 | `TestAssign_UniqueIndexBackstop` | P2 | integration |
| NAM-036 | `TestPreview_DoesNotConsume` | P1 | integration |
| NAM-037 | `FuzzNaming_GaplessInvariant` | P1 | integration |
| NAM-038 | `TestCancel_KeepsNumber` | P1 | integration |
| NAM-039 | `TestAmend_FirstSuffix` | P1 | integration |
| NAM-040 | `TestAmend_SecondSuffix` | P1 | integration |
| NAM-041 | `TestAmend_HyphenatedPrefix` | P1 | integration |
| NAM-042 | `TestAmend_KeepsRootFY` | P1 | integration |
| NAM-043 | `TestAmend_OnlyOnce` | P1 | integration |
| NAM-044 | `TestAmend_DeletedDraftFreesSuffix` | P2 | integration |
| NAM-045 | `TestAmend_Limit` | P2 | integration |
| NAM-046 | `TestRender_AmendmentLength` | P1 | unit |
| NAM-047 | `TestAmend_RequiresCancelled` | P2 | integration |
| NAM-048 | `TestNumber_ClientCannotSet` | P1 | integration |
| NAM-049 | `TestNumber_Immutable` | P1 | integration |
| NAM-050 | `TestCopy_ClearsNumber` | P1 | integration |
| NAM-051 | `TestE2E_NamingSettingsList` | P1 | e2e |
| NAM-052 | `TestSettings_TemplateChangeNewPrefix` | P1 | integration |
| NAM-053 | `TestSettings_PaddingChangeContinues` | P2 | integration |
| NAM-054 | `TestSettings_InvalidEditRejected` | P2 | integration |
| NAM-055 | `TestSettings_DuplicatePrefix` | P1 | integration |
| NAM-056 | `TestSettings_SetCounter` | P1 | integration |
| NAM-057 | `TestSettings_CounterNotBelowIssued` | P1 | integration |
| NAM-058 | `TestSettings_CounterRange` | P2 | unit |
| NAM-059 | `TestSettings_PresetNextFY` | P2 | integration |
| NAM-060 | `TestSettings_AdminOnly` | P1 | integration |
| NAM-061 | `TestSettings_Audited` | P1 | integration |
| NAM-062 | `TestSettings_Preview` | P2 | integration |
| NAM-063 | `TestAssign_PaddingExhausted` | P2 | integration |
| NAM-064 | `TestAssign_AuditedWithTransition` | P2 | integration |
| NAM-065 | `TestPrint_DraftLabel` | P2 | integration |

### 03-document.md — DOC — document lifecycle and audit

106 cases: 76 P1 / 30 P2; 23 unit, 79 integration, 4 e2e.

| ID | Go test | P | Level |
|---|---|---|---|
| DOC-001 | `TestDoc_Create_DraftDefaults` | P1 | integration |
| DOC-002 | `TestDoc_Create_DefaultsOnlyFillMissing` | P2 | unit |
| DOC-003 | `TestDoc_Save_IgnoresSystemFields` | P1 | integration |
| DOC-004 | `TestDoc_Transitions_Allowed` | P1 | integration |
| DOC-005 | `TestDoc_Transitions_Refused` | P1 | integration |
| DOC-006 | `TestDoc_Submit_NoRule_AutoApproves` | P1 | integration |
| DOC-007 | `TestDoc_Submit_WithRule_PendingNoEffects` | P1 | integration |
| DOC-008 | `TestDoc_Approve_RevalidatesThenApplies` | P1 | integration |
| DOC-009 | `TestDoc_Reject_ThenEditToDraft` | P1 | integration |
| DOC-010 | `TestDoc_Resubmit_AfterReject` | P1 | integration |
| DOC-011 | `TestDoc_Submit_Twice` | P1 | integration |
| DOC-012 | `TestDoc_Cancel_ReversesEffects` | P1 | integration |
| DOC-013 | `TestDoc_Cancel_RequiresReason` | P1 | integration |
| DOC-014 | `TestDoc_Close_Approved` | P1 | integration |
| DOC-015 | `TestDoc_Close_Guards` | P1 | integration |
| DOC-016 | `TestDoc_Closed_IsTerminal` | P1 | integration |
| DOC-017 | `TestDoc_Link_ClosedSourceRefused` | P1 | integration |
| DOC-018 | `TestDoc_Cancel_LaterDocOfClosedSource` | P2 | integration |
| DOC-019 | `TestDoc_Validate_MandatoryCollected` | P1 | unit |
| DOC-020 | `TestDoc_Validate_MandatoryBlankRules` | P1 | unit |
| DOC-021 | `TestDoc_Validate_LineMandatoryRowNo` | P1 | unit |
| DOC-022 | `TestDoc_Validate_AtLeastOneLine` | P1 | unit |
| DOC-023 | `TestDoc_Validate_NonNegativeMinMax` | P1 | unit |
| DOC-024 | `TestDoc_Validate_MaxLength` | P2 | unit |
| DOC-025 | `TestDoc_ValidateFromToDates` | P2 | unit |
| DOC-026 | `TestDoc_Validate_HookErrorsMerged` | P1 | unit |
| DOC-027 | `TestDoc_Hooks_CallOrder` | P1 | unit |
| DOC-028 | `TestDoc_Validate_FailureWritesNothing` | P1 | integration |
| DOC-029 | `TestDoc_Save_NoOp` | P2 | integration |
| DOC-030 | `TestDoc_Save_OwnerAndModifier` | P1 | integration |
| DOC-031 | `TestDoc_Save_MissingDoc` | P2 | integration |
| DOC-032 | `TestDoc_Link_MissingMaster` | P1 | unit |
| DOC-033 | `TestDoc_Line_QtyPositive` | P1 | unit |
| DOC-034 | `TestDoc_Link_DisabledMasterRefused` | P1 | integration |
| DOC-035 | `TestDoc_Link_DisabledAfterUse` | P1 | integration |
| DOC-036 | `TestDoc_Approve_DisabledSinceSubmit` | P2 | integration |
| DOC-037 | `TestDoc_Link_SourceStateRules` | P1 | integration |
| DOC-038 | `TestDoc_Link_SourceCancelledBeforeSubmit` | P1 | integration |
| DOC-039 | `TestDoc_Lines_NumberedInOrder` | P1 | integration |
| DOC-040 | `TestDoc_Lines_RemoveRenumbers` | P1 | integration |
| DOC-041 | `TestDoc_Lines_InsertAndMove` | P2 | integration |
| DOC-042 | `TestDoc_Lines_StableIDs` | P1 | integration |
| DOC-043 | `TestDoc_Lines_BlankDropped` | P1 | unit |
| DOC-044 | `TestDoc_Lines_Max500` | P2 | unit |
| DOC-045 | `TestDoc_Lines_DuplicateItemAllowed` | P2 | unit |
| DOC-046 | `TestDoc_Lines_ForeignLineIDRefused` | P1 | integration |
| DOC-047 | `TestDoc_Delete_CascadesLines` | P1 | integration |
| DOC-048 | `TestDoc_Lines_FrozenAfterDraft` | P1 | integration |
| DOC-049 | `TestDoc_Submitted_HeaderImmutable` | P1 | integration |
| DOC-050 | `TestDoc_RollUp_OnlyViaEffects` | P1 | integration |
| DOC-051 | `TestDoc_Schema_NoAllowOnSubmit` | P2 | unit |
| DOC-052 | `TestDoc_Cancel_BlockedByLaterDoc` | P1 | integration |
| DOC-053 | `TestDoc_Cancel_BlockingStates` | P1 | integration |
| DOC-054 | `TestDoc_Cancel_AfterBlockersCancelled` | P1 | integration |
| DOC-055 | `TestDoc_LinkedBlockers_LineLevelDedup` | P1 | unit |
| DOC-056 | `TestDoc_Cancel_LinkCheckFirst` | P1 | integration |
| DOC-057 | `TestDoc_LinkedBlockers_IgnoreAmendedFrom` | P2 | unit |
| DOC-058 | `TestDoc_Cancel_ManyBlockersCapped` | P2 | integration |
| DOC-059 | `TestDoc_RollUp_Recompute` | P1 | integration |
| DOC-060 | `TestDoc_RollUp_IncludesClosed` | P2 | unit |
| DOC-061 | `TestDoc_Delete_DraftKeepsAudit` | P1 | integration |
| DOC-062 | `TestDoc_Delete_OnlyDraft` | P1 | integration |
| DOC-063 | `TestDoc_Delete_AmendmentDraftFreesOriginal` | P2 | integration |
| DOC-064 | `TestDoc_Amend_CopiesToDraft` | P1 | integration |
| DOC-065 | `TestDoc_Amend_OnlyOnce` | P1 | integration |
| DOC-066 | `TestDoc_Amend_Chain` | P2 | integration |
| DOC-067 | `TestDoc_Amend_ValidatedOnSubmit` | P2 | integration |
| DOC-068 | `TestDoc_Amend_AuditBothSides` | P2 | integration |
| DOC-069 | `TestDoc_Concurrency_StaleSave` | P1 | integration |
| DOC-070 | `TestDoc_Concurrency_StaleTransition` | P1 | integration |
| DOC-071 | `TestDoc_Concurrency_ParallelSaves` | P1 | integration |
| DOC-072 | `TestDoc_Concurrency_NoOverfill` | P1 | integration |
| DOC-073 | `TestDoc_Concurrency_CancelVsApprove` | P1 | integration |
| DOC-074 | `TestDoc_Concurrency_VersionOnRollUp` | P2 | integration |
| DOC-075 | `TestDoc_Tx_BusyTimeout` | P2 | integration |
| DOC-076 | `TestDoc_Tx_ApplyEffectsFailureRollsBack` | P1 | integration |
| DOC-077 | `TestDoc_Tx_ReverseEffectsFailureRollsBack` | P1 | integration |
| DOC-078 | `TestDoc_Hooks_ShareTx` | P2 | unit |
| DOC-079 | `FuzzDoc_LifecycleInvariants` | P1 | unit (Go fuzz) |
| DOC-080 | `TestAudit_Create` | P1 | integration |
| DOC-081 | `TestAudit_SaveChangedFieldsOnly` | P1 | integration |
| DOC-082 | `TestAudit_LineDiff` | P1 | integration |
| DOC-083 | `TestAudit_Transitions` | P1 | integration |
| DOC-084 | `TestAudit_EffectWithCause` | P1 | integration |
| DOC-085 | `TestAudit_AppendOnlyTriggers` | P1 | integration |
| DOC-086 | `TestAudit_SameTransaction` | P1 | integration |
| DOC-087 | `TestAudit_RawValues` | P2 | unit |
| DOC-088 | `TestAudit_ExcludesSystemFields` | P2 | unit |
| DOC-089 | `TestE2E_Audit_HistoryView` | P2 | e2e |
| DOC-090 | `TestAudit_EscapedAndPermissioned` | P2 | integration |
| DOC-091 | `TestAudit_DeletedDraftViewable` | P2 | integration |
| DOC-092 | `TestE2E_Doc_PurchaseLifecycle` | P1 | e2e |
| DOC-093 | `TestE2E_Doc_StaleTabConflict` | P2 | e2e |
| DOC-094 | `TestE2E_Doc_ButtonsPerState` | P2 | e2e |
| DOC-095 | `TestDoc_Withdraw_PendingToDraft` | P1 | integration |
| DOC-096 | `TestDoc_Withdraw_Atomic` | P1 | integration |
| DOC-097 | `TestDoc_Info_FromRegistryFields` | P1 | integration |
| DOC-098 | `TestDoc_References_ListsDocuments` | P1 | integration |
| DOC-099 | `TestDoc_Claim_SubmitAndApprove` | P1 | integration |
| DOC-100 | `TestDoc_Save_NumberGuarded` | P1 | integration |
| DOC-101 | `TestDoc_Link_BlockedMaster` | P2 | integration |
| DOC-102 | `TestDoc_Load_PlainValues` | P1 | integration |
| DOC-103 | `TestDoc_Errors_HTTPStatus` | P1 | integration |
| DOC-104 | `TestDoc_ApproveReject_CheckOrder` | P1 | integration |
| DOC-105 | `TestDoc_Save_ChecksRestrictions` | P1 | integration |
| DOC-106 | `TestDoc_Link_MasterWarning` | P2 | integration |

### 04-approvals.md — APR — approvals

97 cases: 72 P1 / 25 P2; 23 unit, 72 integration, 2 e2e.

| ID | Go test | P | Level |
|---|---|---|---|
| APR-001 | `TestApprovalRule_CreateAmountRule` | P1 | integration |
| APR-002 | `TestApprovalRule_OnlyAdminCanEdit` | P1 | integration |
| APR-003 | `TestApprovalRule_FromAmountNotNegative` | P1 | unit |
| APR-004 | `TestApprovalRule_FromAmountPrecision` | P2 | unit |
| APR-005 | `TestApprovalRule_RoleRequired` | P1 | unit |
| APR-006 | `TestApprovalRule_RoleMustBeActive` | P2 | unit |
| APR-007 | `TestApprovalRule_ManagerNeedsFallbackRole` | P1 | unit |
| APR-008 | `TestApprovalRule_ManagerOnlyForExpenses` | P2 | unit |
| APR-009 | `TestApprovalRule_OnlyApprovableDocTypes` | P1 | unit |
| APR-010 | `TestApprovalRule_DuplicateBlocked` | P1 | integration |
| APR-011 | `TestApprovalRule_DisabledIgnored` | P2 | integration |
| APR-012 | `TestApprovalRule_ReenableRechecksDuplicate` | P2 | integration |
| APR-013 | `TestApprovalRule_UsedRuleDisableNotDelete` | P1 | integration |
| APR-014 | `TestApprovalRule_ChangesAudited` | P2 | integration |
| APR-015 | `TestApprovalSubmit_NoRuleApprovesDirectly` | P1 | integration |
| APR-016 | `TestApprovalSubmit_BelowThresholdApproved` | P1 | integration |
| APR-017 | `TestApprovalMatch_EqualToThresholdNeedsApproval` | P1 | unit |
| APR-018 | `TestApprovalMatch_ThresholdTable` | P1 | unit |
| APR-019 | `TestApprovalMatch_UsesGrandTotalWithGST` | P1 | integration |
| APR-020 | `TestApprovalMatch_ExpenseUsesClaimTotal` | P1 | integration |
| APR-021 | `TestApprovalMatch_HighestThresholdWins` | P1 | unit |
| APR-022 | `TestApprovalMatch_OtherDocTypeIgnored` | P1 | unit |
| APR-023 | `TestApprovalMatch_ZeroThresholdAlwaysApplies` | P1 | integration |
| APR-024 | `TestApprovalMatch_OnlyAtSubmit` | P1 | integration |
| APR-025 | `TestApprovalRequest_SnapshotsRule` | P1 | integration |
| APR-026 | `TestApprovalRequest_DisabledRuleKeepsPending` | P2 | integration |
| APR-027 | `TestApprovalSubmit_LocksWithoutNumber` | P1 | integration |
| APR-028 | `TestApprovalSubmit_ValidationBeforeRules` | P1 | integration |
| APR-029 | `TestApprovalApprove_RoleHolderCanApprove` | P1 | integration |
| APR-030 | `TestApprovalApprove_NonApproverRefused` | P1 | integration |
| APR-031 | `TestApprovalApprove_RefusalIsAnError` | P2 | unit |
| APR-032 | `TestApprovalApprove_HigherTierCanApprove` | P1 | integration |
| APR-033 | `TestApprovalApprove_LowerTierRefused` | P1 | integration |
| APR-034 | `TestApprovalApprove_NeedsReadAccess` | P1 | integration |
| APR-035 | `TestApprovalSelf_RequesterBlocked` | P1 | integration |
| APR-036 | `TestApprovalSelf_CreatorOrSubmitter` | P1 | unit |
| APR-037 | `TestApprovalSelf_ClaimantBlocked` | P1 | integration |
| APR-038 | `TestApprovalSelf_AllowedByRule` | P2 | integration |
| APR-039 | `TestApprovalSelf_AdminNotExempt` | P1 | integration |
| APR-040 | `TestApprovalSubmit_NoEligibleApproverRefused` | P1 | integration |
| APR-041 | `TestApprovalEligible_ExcludesDisabled` | P1 | integration |
| APR-042 | `TestApprovalEligible_LiveRoleCheck` | P1 | integration |
| APR-043 | `TestApprovalStuck_ListedForAdmin` | P2 | integration |
| APR-044 | `TestApprovalExpense_ReportingManagerApproves` | P1 | integration |
| APR-045 | `TestApprovalExpense_OtherManagerRefused` | P1 | integration |
| APR-046 | `TestApprovalExpense_FallbackRoleCanApprove` | P2 | integration |
| APR-047 | `TestApprovalExpense_NoManagerUsesFallback` | P1 | integration |
| APR-048 | `TestApprovalExpense_DisabledManagerUsesFallback` | P1 | integration |
| APR-049 | `TestApprovalExpense_ManagerDisabledWhilePending` | P1 | integration |
| APR-050 | `TestApprovalExpense_SelfManagerFallsBack` | P2 | unit |
| APR-051 | `TestApprovalExpense_HighAmountGoesToRole` | P1 | integration |
| APR-052 | `TestApprovalApprove_AppliesEffectsAtomically` | P1 | integration |
| APR-053 | `TestApprovalApprove_RevalidatesAndRollsBack` | P1 | integration |
| APR-054 | `TestApprovalApprove_OptionalComment` | P2 | integration |
| APR-055 | `TestApprovalApprove_OnlyFromPending` | P1 | unit |
| APR-056 | `TestApprovalApprove_StampsApprover` | P1 | integration |
| APR-057 | `TestApprovalReject_NotAfterApprove` | P1 | unit |
| APR-058 | `TestApprovalReject_ReasonRequired` | P1 | unit |
| APR-059 | `TestApprovalReject_ReasonLength` | P2 | unit |
| APR-060 | `TestApprovalReject_StoresReason` | P1 | integration |
| APR-061 | `TestApprovalReject_SameEligibility` | P1 | integration |
| APR-062 | `TestApprovalRevise_RejectedIsLocked` | P1 | integration |
| APR-063 | `TestApprovalRevise_ToDraftKeepsReason` | P1 | integration |
| APR-064 | `TestApprovalRevise_NeedsWriteRight` | P2 | integration |
| APR-065 | `TestApprovalResubmit_LowerAmountSkipsApproval` | P1 | integration |
| APR-066 | `TestApprovalResubmit_HigherAmountHigherRule` | P1 | integration |
| APR-067 | `TestApprovalResubmit_FreshRequest` | P1 | integration |
| APR-068 | `TestApprovalResubmit_SameApproverMayApprove` | P2 | integration |
| APR-069 | `TestApprovalRevise_DraftDeletable` | P2 | integration |
| APR-070 | `TestApprovalReject_NotExportable` | P2 | integration |
| APR-091 | `TestApproval_Withdraw_SubmitterBackToDraft` | P1 | integration |
| APR-092 | `TestApproval_Withdraw_NeedsSubmitRight` | P1 | integration |
| APR-093 | `TestApproval_Withdraw_LeavesInbox` | P1 | integration |
| APR-094 | `TestApproval_Withdraw_OnlyFromPending` | P1 | integration |
| APR-095 | `TestApproval_Withdraw_StaleAfterApprove` | P1 | integration |
| APR-096 | `TestApproval_Withdraw_ResubmitNewRequest` | P1 | integration |
| APR-097 | `TestE2E_ApprovalWithdraw` | P1 | e2e |
| APR-071 | `TestApprovalConcurrency_FirstActionWins` | P1 | integration |
| APR-072 | `TestApprovalConcurrency_ParallelApproveOnce` | P1 | integration |
| APR-073 | `TestApprovalPending_EditRefused` | P1 | integration |
| APR-074 | `TestApprovalPending_ApproverCannotEdit` | P1 | integration |
| APR-075 | `TestApprovalPending_NoDeleteNoCancel` | P1 | integration |
| APR-076 | `TestApprovalTransition_NoDirectStateWrite` | P1 | unit |
| APR-077 | `TestApprovalInbox_OnlyActionable` | P1 | integration |
| APR-078 | `TestApprovalInbox_CountMatches` | P2 | integration |
| APR-079 | `TestE2E_ApprovalInboxRowContents` | P2 | e2e |
| APR-080 | `TestApprovalInbox_ClosesAndReopens` | P1 | integration |
| APR-081 | `TestApprovalInbox_MyRequests` | P1 | integration |
| APR-082 | `TestApprovalInbox_ExcludesOwnUnlessAllowed` | P1 | unit |
| APR-083 | `TestApprovalHistory_AppendOnly` | P1 | integration |
| APR-084 | `TestApprovalHistory_KeepsRuleAndAmount` | P2 | integration |
| APR-085 | `TestApprovalHistory_InDocAudit` | P2 | integration |
| APR-086 | `TestApprovalCancel_NoApprovalNeeded` | P2 | integration |
| APR-087 | `TestApprovalAmend_NeedsFreshApproval` | P1 | integration |
| APR-088 | `TestApprovalCopy_ClearsApprovalFields` | P1 | unit |
| APR-089 | `FuzzApproval_StateInvariants` | P2 | unit |
| APR-090 | `FuzzApprovalMatch_Monotonic` | P2 | unit |

### 05-files-print.md — FILE — files and printing

83 cases: 67 P1 / 16 P2; 28 unit, 48 integration, 7 e2e.

| ID | Go test | P | Level |
|---|---|---|---|
| FILE-001 | `TestAttachment_UploadJPEGToDraft` | P1 | integration |
| FILE-002 | `TestAttachment_SizeLimitBoundary` | P1 | unit |
| FILE-003 | `TestUpload_DeclaredSizeTooLarge` | P1 | integration |
| FILE-004 | `TestUpload_StreamedOverLimitCleansTemp` | P1 | integration |
| FILE-005 | `TestAttachment_EmptyFileRejected` | P1 | unit |
| FILE-006 | `TestSniff_AllowedTypes` | P1 | unit |
| FILE-007 | `TestAttachment_DisallowedExtensions` | P1 | unit |
| FILE-008 | `TestAttachment_ExtensionCaseInsensitive` | P1 | unit |
| FILE-009 | `TestSniff_ContentExtensionMismatch` | P1 | unit |
| FILE-010 | `TestSniff_CSVTextRules` | P1 | unit |
| FILE-011 | `TestAttachment_DoubleExtension` | P1 | unit |
| FILE-012 | `TestFileName_Sanitize` | P1 | unit |
| FILE-013 | `TestServe_UnicodeFileName` | P2 | integration |
| FILE-014 | `TestBlobPath_RejectsTraversal` | P1 | unit |
| FILE-015 | `TestUpload_DBFailureLeavesNoRow` | P1 | integration |
| FILE-016 | `TestAttachment_DedupeAcrossDocuments` | P1 | integration |
| FILE-017 | `TestAttachment_SameContentSameDocRejected` | P2 | integration |
| FILE-018 | `TestAttachment_RemoveKeepsSharedBlob` | P1 | integration |
| FILE-019 | `TestBlobGC_GracePeriod` | P1 | integration |
| FILE-020 | `TestBlobGC_ConcurrentUploadSameContent` | P1 | integration |
| FILE-021 | `TestAttachment_PerDocumentLimit` | P1 | integration |
| FILE-022 | `TestAttachment_LimitConcurrent` | P2 | integration |
| FILE-023 | `TestAttachment_ParentMustExist` | P1 | integration |
| FILE-024 | `TestAttachment_AddNeedsParentWrite` | P1 | integration |
| FILE-025 | `TestAttachment_ReadFollowsParentRecordRule` | P1 | integration |
| FILE-026 | `TestAttachment_NoOwnerBypass` | P1 | integration |
| FILE-027 | `TestServe_RequiresLogin` | P1 | integration |
| FILE-028 | `TestServe_IDORAndHashGuess` | P1 | integration |
| FILE-029 | `TestAttachment_DedupeNoLeak` | P1 | integration |
| FILE-030 | `TestAttachmentList_Formatting` | P2 | integration |
| FILE-031 | `TestServe_SecurityHeaders` | P1 | integration |
| FILE-032 | `TestServe_InlineVsAttachment` | P1 | integration |
| FILE-033 | `TestE2E_ExpenseAttachmentPreview` | P2 | e2e |
| FILE-034 | `TestAttachment_BytesUnchanged` | P1 | integration |
| FILE-035 | `TestE2E_ExifOrientation` | P2 | e2e |
| FILE-036 | `TestServe_MissingBlob` | P1 | integration |
| FILE-037 | `TestUpload_StorageWriteError` | P1 | integration |
| FILE-038 | `TestAttachment_RemoveFromDraftAudited` | P1 | integration |
| FILE-039 | `TestAttachment_RemoveBlockedAfterSubmit` | P1 | integration |
| FILE-040 | `TestAttachment_AddAfterApproval` | P1 | integration |
| FILE-041 | `TestAttachment_AddToCancelledBlocked` | P1 | integration |
| FILE-042 | `TestAttachment_DraftDeleteCascades` | P1 | integration |
| FILE-043 | `TestAttachment_CopiedOnAmend` | P1 | integration |
| FILE-044 | `TestAttachment_AmendCopyIndependent` | P1 | integration |
| FILE-045 | `TestExpenseLine_ReceiptMustBelongToDoc` | P2 | integration |
| FILE-046 | `TestAttachment_RemoveBlockedWhileReferencedByLine` | P2 | integration |
| FILE-047 | `TestUpload_ConcurrentSameContent` | P1 | integration |
| FILE-048 | `TestCompanyLogo_Rules` | P1 | integration |
| FILE-049 | `TestFilesCheck_Report` | P2 | integration |
| FILE-050 | `TestPrint_RequiresPrintRight` | P1 | integration |
| FILE-051 | `TestPrint_PurchaseOrderGolden` | P1 | integration (golden file) |
| FILE-052 | `TestPrint_DraftWatermark` | P1 | integration |
| FILE-053 | `TestPrint_CancelledWatermark` | P1 | integration |
| FILE-054 | `TestPrint_NoWatermarkWhenFinal` | P1 | integration |
| FILE-055 | `TestPrint_WatermarkNotOverridable` | P1 | integration |
| FILE-056 | `TestE2E_PrintA4`, `TestPrintCSS_A4` | P1 | e2e |
| FILE-057 | `TestE2E_PrintLongTable` | P1 | e2e |
| FILE-058 | `TestE2E_PrintPageNumbers` | P2 | e2e |
| FILE-059 | `TestPrint_EscapesUserContent` | P1 | unit |
| FILE-060 | `TestE2E_PrintNoExternal` | P1 | e2e |
| FILE-061 | `TestPrint_TitleIsSafeNumber` | P1 | unit |
| FILE-062 | `TestPrint_IsReadOnly` | P1 | integration |
| FILE-063 | `TestPrint_UsesDocumentSnapshot` | P1 | integration |
| FILE-064 | `TestPrint_OptionalPartsOmitted` | P1 | unit |
| FILE-065 | `TestPrint_UsesNUMFormatting` | P1 | unit |
| FILE-066 | `TestPrint_AllPrintableDocTypesHaveTemplates` | P1 | unit |
| FILE-067 | `TestPrint_GRNGolden` | P2 | integration (golden file) |
| FILE-068 | `TestPrint_ExpenseGolden` | P2 | integration (golden file) |
| FILE-069 | `TestPrint_ApprovalAndAmendLines` | P2 | unit |
| FILE-070 | `TestE2E_PrintButton` | P2 | e2e |
| FILE-071 | `TestPrint_EmptyDraft` | P2 | unit |
| FILE-072 | `TestInWordsINR` | P1 | unit |
| FILE-073 | `TestInWordsINR_ZeroAndPaiseOnly` | P1 | unit |
| FILE-074 | `TestInWordsINR_WholeRupees` | P1 | unit |
| FILE-075 | `TestInWordsINR_BelowThousand` | P1 | unit |
| FILE-076 | `TestInWordsINR_Thousands` | P1 | unit |
| FILE-077 | `TestInWordsINR_Lakh` | P1 | unit |
| FILE-078 | `TestInWordsINR_Crore` | P1 | unit |
| FILE-079 | `TestInWordsINR_Errors` | P1 | unit |
| FILE-080 | `TestInWordsINR_NoFloatDrift` | P1 | unit |
| FILE-081 | `FuzzInWordsINR_RoundTrip` | P1 | unit (fuzz) |
| FILE-082 | `TestPrint_WordsUsePayableTotal` | P1 | integration |
| FILE-083 | `TestPrintModel_WordsDerived` | P2 | unit |

### 06-masters.md — MST — masters

129 cases: 96 P1 / 33 P2; 71 unit, 58 integration, 0 e2e.

| ID | Go test | P | Level |
|---|---|---|---|
| MST-001 | `TestMaster_CodeRequired` | P1 | unit |
| MST-002 | `TestMaster_CodeNormalised` | P1 | unit |
| MST-003 | `TestMaster_CodeUniqueCaseInsensitive` | P1 | integration |
| MST-004 | `TestMaster_CodeFormat` | P1 | unit |
| MST-005 | `TestMaster_CodeUniqueConcurrent` | P2 | integration |
| MST-006 | `TestMaster_RenameCodeKeepsLinks` | P1 | integration |
| MST-007 | `TestMaster_RenameToExistingCodeRefused` | P1 | integration |
| MST-008 | `TestMaster_RenameNeedsRight` | P2 | integration |
| MST-009 | `TestMaster_DisabledHiddenFromSearch` | P1 | integration |
| MST-010 | `TestMaster_DisabledBlocksDraftSave` | P1 | integration |
| MST-011 | `TestMaster_DisabledVisibleOnOldDocs` | P1 | integration |
| MST-012 | `TestMaster_AmendWithDisabledMaster` | P2 | integration |
| MST-013 | `TestMaster_DeleteUnreferenced` | P1 | integration |
| MST-014 | `TestMaster_DeleteReferencedRefused` | P1 | integration |
| MST-015 | `TestMaster_AllFKsRestrict` | P2 | integration |
| MST-016 | `TestMaster_DisableAudited` | P1 | integration |
| MST-017 | `TestMaster_NameRequiredTrimmed` | P1 | unit |
| MST-018 | `TestMaster_ConcurrentEditRejected` | P2 | integration |
| MST-019 | `TestCompany_Singleton` | P1 | integration |
| MST-020 | `TestCompany_PANFormat` | P1 | unit |
| MST-021 | `TestBranch_RequiredFields` | P1 | unit |
| MST-022 | `TestBranch_GSTINStateMatch` | P1 | unit |
| MST-023 | `TestBranch_GSTINMatchesCompanyPAN` | P1 | unit |
| MST-024 | `TestBranch_GSTINOptional` | P1 | unit |
| MST-025 | `TestBranch_SharedGSTINSameState` | P2 | integration |
| MST-026 | `TestBranch_GSTINValidated` | P1 | unit |
| MST-027 | `TestBranch_LastEnabledBranch` | P1 | integration |
| MST-028 | `TestSetup_SeedsCompanyAndBranch` | P2 | integration |
| MST-029 | `TestBranch_Address` | P2 | integration |
| MST-030 | `TestGSTIN_NormaliseAndLength` | P1 | unit |
| MST-031 | `TestGSTIN_CheckDigit` | P1 | unit |
| MST-032 | `FuzzGSTIN_CheckDigit` | P2 | unit |
| MST-033 | `TestGSTIN_FormatPerCategory` | P1 | unit |
| MST-034 | `TestGSTIN_CategoryRequiresGSTIN` | P1 | unit |
| MST-035 | `TestGSTIN_GuessCategory` | P1 | unit |
| MST-036 | `TestGSTIN_PANFromGSTIN` | P1 | unit |
| MST-037 | `TestGSTIN_StateFromGSTIN` | P1 | unit |
| MST-038 | `TestGSTIN_UnknownStateCode` | P1 | unit |
| MST-039 | `TestStates_MatchGSTCodes` | P1 | unit |
| MST-040 | `TestParty_PANValidation` | P1 | unit |
| MST-041 | `TestParty_RoleFlags` | P1 | unit |
| MST-042 | `TestParty_VendorPickerFilter` | P1 | integration |
| MST-043 | `TestParty_RoleInUse` | P2 | integration |
| MST-044 | `TestParty_DuplicateGSTINRefused` | P1 | integration |
| MST-045 | `TestParty_SamePANOrNameWarns` | P2 | integration |
| MST-046 | `TestParty_DeleteCascadesOwnAddresses` | P1 | integration |
| MST-047 | `TestParty_OverseasCountry` | P1 | unit |
| MST-048 | `TestParty_GSTINsSharePAN` | P1 | integration |
| MST-049 | `TestParty_GSTINBackfillsParty` | P1 | integration |
| MST-050 | `TestParty_DefaultPaymentTerms` | P2 | integration |
| MST-051 | `TestParty_PrimaryMustBelong` | P1 | integration |
| MST-052 | `TestParty_GSTINChangeKeepsSnapshots` | P1 | integration |
| MST-053 | `TestAddress_RequiredFields` | P1 | unit |
| MST-054 | `TestAddress_Type` | P1 | unit |
| MST-055 | `TestAddress_PincodeFormat` | P1 | unit |
| MST-056 | `TestAddress_PincodeState` | P2 | unit |
| MST-057 | `TestAddress_Foreign` | P2 | unit |
| MST-058 | `TestAddress_ManyPerParty` | P1 | integration |
| MST-059 | `TestAddress_FirstIsPrimary` | P1 | integration |
| MST-060 | `TestAddress_OnePrimaryPerType` | P1 | integration |
| MST-061 | `TestAddress_Disabled` | P2 | integration |
| MST-062 | `TestContact_NameRequired` | P1 | unit |
| MST-063 | `TestContact_EmailFormat` | P1 | unit |
| MST-064 | `TestContact_MobileFormat` | P1 | unit |
| MST-065 | `TestContact_OnePrimaryEmail` | P2 | unit |
| MST-066 | `TestContact_OnePrimaryPerParty` | P1 | integration |
| MST-067 | `TestAddress_SingleOwner` | P1 | unit |
| MST-068 | `TestAddress_DisplayText` | P2 | unit |
| MST-069 | `TestItem_RequiredFields` | P1 | unit |
| MST-070 | `TestItem_GoodsNeedHSN` | P1 | unit |
| MST-071 | `TestItem_HSNLength` | P1 | unit |
| MST-072 | `TestItem_GoodsHSNNot99` | P1 | unit |
| MST-073 | `TestItem_ServiceSAC` | P1 | unit |
| MST-074 | `TestItem_DefaultTaxCodeEnabled` | P1 | integration |
| MST-075 | `TestItem_TypeLocked` | P2 | integration |
| MST-076 | `TestItem_ServiceNotStock` | P2 | unit |
| MST-077 | `TestUoM_Basic` | P1 | unit |
| MST-078 | `TestUoM_SeedUQC` | P2 | unit |
| MST-079 | `TestItemUoM_StockRowAuto` | P1 | unit |
| MST-080 | `TestItemUoM_StockFactorOne` | P1 | unit |
| MST-081 | `TestItemUoM_FactorPositive` | P1 | unit |
| MST-082 | `TestItemUoM_NoDuplicateUoM` | P1 | unit |
| MST-083 | `TestItemUoM_PurchaseUoMNeedsRow` | P1 | unit |
| MST-084 | `TestItemUoM_ConvertToStock` | P1 | unit |
| MST-085 | `TestItemUoM_FactorPrecision` | P2 | unit |
| MST-086 | `TestUoM_WholeNumberLineQty` | P1 | integration |
| MST-087 | `TestUoM_WholeNumberStockQty` | P1 | integration |
| MST-088 | `TestUoM_WholeNumberToleratesDust` | P2 | unit |
| MST-089 | `TestItemUoM_StockUoMChangeResets` | P1 | integration |
| MST-090 | `TestItemUoM_StockUoMLocked` | P1 | integration |
| MST-091 | `TestItemUoM_FactorSnapshot` | P1 | integration |
| MST-092 | `TestUoM_WholeNumberFlagChange` | P2 | integration |
| MST-093 | `TestUoM_DisabledUoM` | P2 | integration |
| MST-094 | `TestTaxCode_TaxableNeedsRate` | P1 | unit |
| MST-095 | `TestTaxCode_NonTaxableZero` | P1 | unit |
| MST-096 | `TestTaxCode_AllowedRates` | P1 | unit |
| MST-097 | `TestTaxCode_Seed` | P1 | integration |
| MST-098 | `TestTaxCode_Split` | P1 | unit |
| MST-099 | `TestTaxCode_RateLocked` | P1 | integration |
| MST-100 | `TestProject_Dates` | P1 | unit |
| MST-101 | `TestProject_Status` | P1 | unit |
| MST-102 | `TestProject_ClosedBlocksNewDocs` | P1 | integration |
| MST-103 | `TestProject_CustomerRole` | P2 | integration |
| MST-104 | `TestProject_Branch` | P2 | unit |
| MST-105 | `TestSite_RequiredFields` | P1 | unit |
| MST-106 | `TestSite_ProjectBranchMatch` | P2 | integration |
| MST-107 | `TestSite_BranchLocked` | P2 | integration |
| MST-108 | `TestBranch_DisableWithSites` | P2 | integration |
| MST-109 | `TestEmployee_RequiredFields` | P1 | unit |
| MST-110 | `TestEmployee_Dates` | P1 | unit |
| MST-111 | `TestEmployee_UserUniqueAmongActive` | P1 | integration |
| MST-112 | `TestEmployee_UserMustExist` | P2 | integration |
| MST-113 | `TestEmployee_NoSelfManager` | P1 | unit |
| MST-114 | `TestEmployee_NoManagerCycle` | P1 | integration |
| MST-115 | `FuzzEmployee_ManagerChainAcyclic` | P2 | unit |
| MST-116 | `TestEmployee_ManagerActive` | P1 | integration |
| MST-117 | `TestEmployee_LeftWithReportees` | P1 | integration |
| MST-118 | `TestEmployee_LeftNeedsRelievingDate` | P1 | unit |
| MST-119 | `TestEmployee_LeftDisablesUser` | P1 | integration |
| MST-120 | `TestEmployee_LeftHidden` | P1 | integration |
| MST-121 | `TestExpenseType_Basic` | P1 | unit |
| MST-122 | `TestExpenseType_DefaultHSN` | P1 | unit |
| MST-123 | `TestExpenseType_DefaultsCopied` | P2 | integration |
| MST-124 | `TestPaymentTerms_Total100` | P1 | unit |
| MST-125 | `TestPaymentTerms_DaysNonNegative` | P1 | unit |
| MST-126 | `TestPaymentTerms_NoDuplicateRows` | P1 | unit |
| MST-127 | `TestPaymentTerms_PortionPositive` | P1 | unit |
| MST-128 | `TestPaymentTerms_DueDates` | P1 | unit |
| MST-129 | `TestPaymentTerms_ScheduleSumsExactly` | P2 | unit |

### 07-numbers-lists.md — NUM — numbers, lists and CSV

121 cases: 105 P1 / 16 P2; 70 unit, 49 integration, 2 e2e.

| ID | Go test | P | Level |
|---|---|---|---|
| NUM-001 | `TestMoney_ParseFormatRoundTrip` | P1 | unit |
| NUM-002 | `TestRound_HalfUpTies` | P1 | unit |
| NUM-003 | `FuzzRound_SymmetricIdempotent` | P1 | unit (fuzz) |
| NUM-004 | `TestRound_FloatTrapValues` | P1 | unit |
| NUM-005 | `FuzzLineAmount_MatchesBigRat` | P1 | unit (fuzz) |
| NUM-006 | `TestQty_ParseScale` | P1 | unit |
| NUM-007 | `TestQty_WholeNumberUoM` | P1 | unit |
| NUM-008 | `TestRate_ParseFormat` | P1 | unit |
| NUM-009 | `TestPercent_ParseRange` | P1 | unit |
| NUM-010 | `TestMoney_RejectExcessDecimals` | P1 | unit |
| NUM-011 | `TestLineValues_SignRules` | P1 | unit |
| NUM-012 | `TestLimits_NoOverflow` | P1 | unit |
| NUM-013 | `TestLineAmount_Table` | P1 | unit |
| NUM-014 | `TestFactor_StockQtyRounding` | P1 | unit |
| NUM-015 | `TestDiscount_NetRate` | P2 | unit |
| NUM-016 | `TestSchema_NoFloatColumns` | P1 | integration |
| NUM-017 | `TestSQL_SumIsExactInteger` | P1 | integration |
| NUM-018 | `TestMoney_JSONAsString` | P2 | unit |
| NUM-019 | `FuzzValues_RoundTripNoPanic` | P1 | unit (fuzz) |
| NUM-020 | `TestTotals_NetIsSumOfLines` | P1 | unit |
| NUM-021 | `TestTax_RoundPerLine` | P1 | unit |
| NUM-022 | `TestTax_HeaderEqualsSumOfLines` | P1 | unit |
| NUM-023 | `TestTotals_RoundedTotalHalfUp` | P1 | unit |
| NUM-024 | `TestTotals_RoundingDisabledForDocType` | P1 | unit |
| NUM-025 | `TestTax_ZeroRatedLine` | P1 | unit |
| NUM-026 | `TestTotals_RecomputeIsStable` | P1 | integration |
| NUM-030 | `TestGST_IntraStateSplit` | P1 | unit |
| NUM-031 | `TestGST_InterStateIGST` | P1 | unit |
| NUM-032 | `TestGST_HalfPaisaSplit` | P1 | unit |
| NUM-033 | `TestGST_TieRounding` | P1 | unit |
| NUM-034 | `TestGST_KnownValue6251` | P1 | unit |
| NUM-035 | `TestGST_FractionalRates` | P1 | unit |
| NUM-036 | `FuzzGST_HalvesEqual` | P1 | unit (fuzz) |
| NUM-037 | `TestGST_NoMixedTaxTypes` | P1 | integration |
| NUM-038 | `TestGST_UnregisteredSupplierState` | P1 | unit |
| NUM-039 | `TestGST_BranchStateFallback` | P1 | unit |
| NUM-040 | `TestGST_StateFromGSTIN` | P1 | unit |
| NUM-041 | `TestGST_RecomputeOnPartyChange` | P1 | integration |
| NUM-042 | `TestGST_SEZIsInterState` | P2 | unit |
| NUM-043 | `TestGST_EnteredAmountTolerance` | P2 | unit |
| NUM-044 | `TestGST_InclusiveBackCalculation` | P2 | unit |
| NUM-050 | `TestFormat_IndianGrouping` | P1 | unit |
| NUM-051 | `TestFormat_BriefExamples` | P1 | unit |
| NUM-052 | `TestFormat_NegativeAndZero` | P1 | unit |
| NUM-053 | `TestFormat_Qty` | P1 | unit |
| NUM-054 | `TestFormat_Rate` | P1 | unit |
| NUM-055 | `TestFormat_Percent` | P1 | unit |
| NUM-056 | `TestParse_AcceptedForms` | P1 | unit |
| NUM-057 | `TestParse_RejectedForms` | P1 | unit |
| NUM-058 | `TestParse_DecimalLimitsPerType` | P1 | unit |
| NUM-059 | `TestE2E_AmountInputFormatting` | P2 | e2e |
| NUM-070 | `TestFiscalYear_FromDate` | P1 | unit |
| NUM-071 | `TestFiscalYear_Labels` | P1 | unit |
| NUM-072 | `TestFiscalYear_LeapDays` | P1 | unit |
| NUM-073 | `TestDate_BusinessDateFromUTC` | P1 | unit |
| NUM-074 | `TestDate_TodayInIST` | P1 | unit |
| NUM-075 | `TestTime_StorageAndDisplay` | P1 | unit |
| NUM-076 | `TestTime_EmbeddedTZData` | P1 | integration |
| NUM-077 | `TestDate_Format` | P1 | unit |
| NUM-078 | `TestDate_ParseAccepted` | P1 | unit |
| NUM-079 | `TestDate_ParseRejected` | P1 | unit |
| NUM-080 | `TestDate_YearRange` | P1 | unit |
| NUM-081 | `TestDate_AddMonthsClamp` | P1 | unit |
| NUM-082 | `TestDate_AddDays` | P1 | unit |
| NUM-083 | `TestDate_MonthEnd` | P1 | unit |
| NUM-084 | `TestDate_DueDateRules` | P2 | unit |
| NUM-085 | `TestDate_RangePresets` | P2 | unit |
| NUM-086 | `TestFiscalYear_Quarter` | P2 | unit |
| NUM-100 | `TestList_FilterEquals` | P1 | integration |
| NUM-101 | `TestList_NotEqualsIncludesEmpty` | P1 | integration |
| NUM-102 | `TestList_ContainsLiteral` | P1 | integration |
| NUM-103 | `TestList_InNotIn` | P1 | integration |
| NUM-104 | `TestList_DateBetween` | P1 | integration |
| NUM-105 | `TestList_TimestampDayRangeIST` | P1 | integration |
| NUM-106 | `TestList_MoneyComparison` | P1 | integration |
| NUM-107 | `TestList_IsEmpty` | P1 | integration |
| NUM-108 | `TestList_FiltersAndSearchAreANDed` | P1 | integration |
| NUM-109 | `TestList_RejectUnknownFieldOrOperator` | P1 | integration |
| NUM-110 | `TestList_NoSQLInjection` | P1 | integration |
| NUM-111 | `TestList_SortWhitelist` | P1 | integration |
| NUM-112 | `TestList_DefaultSort` | P1 | integration |
| NUM-113 | `TestList_StablePagination` | P1 | integration |
| NUM-114 | `TestList_SortByValue` | P1 | integration |
| NUM-115 | `TestList_EmptyLast` | P1 | integration |
| NUM-116 | `TestList_TextSortNoCase` | P1 | integration |
| NUM-117 | `TestList_PageSizeRules` | P1 | integration |
| NUM-118 | `TestList_CountIndependentOfPage` | P1 | integration |
| NUM-119 | `TestList_SearchFields` | P1 | integration |
| NUM-120 | `TestList_SearchLiteralChars` | P1 | integration |
| NUM-121 | `TestList_PermissionPredicateAlwaysApplied` | P1 | integration |
| NUM-122 | `TestList_NoReadRight` | P1 | integration |
| NUM-123 | `TestList_DisabledHiddenInPickers` | P1 | integration |
| NUM-124 | `TestList_ChildLineFilterSameRow` | P2 | integration |
| NUM-125 | `TestE2E_ListStateInURL` | P2 | e2e |
| NUM-126 | `TestList_TotalsRowAllMatching` | P2 | integration |
| NUM-127 |  | P2 | integration |
| NUM-140 | `TestImport_Template` | P1 | integration |
| NUM-141 | `TestImport_HeaderMapping` | P1 | unit |
| NUM-142 | `TestImport_RequiredAndDuplicateColumns` | P1 | unit |
| NUM-143 | `TestImport_UnknownColumnWarning` | P1 | unit |
| NUM-144 | `TestImport_DryRunWritesNothing` | P1 | integration |
| NUM-145 | `TestImport_AllOrNothing` | P1 | integration |
| NUM-146 | `TestImport_RowValidationAllErrors` | P1 | integration |
| NUM-147 | `TestImport_DuplicateCodes` | P1 | integration |
| NUM-148 | `TestImport_LinkByCode` | P1 | integration |
| NUM-149 | `TestImport_NumbersIndianFormat` | P1 | integration |
| NUM-150 | `TestImport_DatesDayFirst` | P1 | integration |
| NUM-151 | `TestImport_EncodingUTF8Only` | P1 | integration |
| NUM-152 | `TestImport_CSVMechanics` | P1 | unit |
| NUM-153 | `TestImport_Limits` | P1 | unit |
| NUM-154 | `TestImport_TextPreserved` | P1 | unit |
| NUM-155 | `TestImport_UnescapeFormula` | P1 | unit |
| NUM-156 | `TestImport_NeedsImportRight` | P1 | integration |
| NUM-157 | `TestImport_SemicolonHint` | P2 | unit |
| NUM-158 | `TestImport_ErrorRowsDownload` | P2 | integration |
| NUM-159 | `TestExport_MatchesListAndPermissions` | P1 | integration |
| NUM-160 | `TestExport_NeedsExportRight` | P1 | integration |
| NUM-161 | `TestExport_ValueFormats` | P1 | integration |
| NUM-162 | `TestExport_FormulaEscapeTextOnly` | P1 | unit |
| NUM-163 | `TestExportImport_RoundTrip` | P1 | integration |
| NUM-164 | `TestExport_RowLimit` | P2 | integration |

## Changes made in reconciliation

Changes made on 24 October 2026 to bring the seven files in line with spec §3.1 (R1–R7), §3.2 and §5, and with `00-fixtures.md`. No test ID was renumbered, no case was deleted and no case was merged. R-numbers say which resolution drove each change.

### New cases (spec R2, `withdraw`)

| ID | Title | Go test | P | Level |
|---|---|---|---|---|
| DOC-095 | Withdraw moves a pending document back to an editable draft | `TestDoc_Withdraw_PendingToDraft` | P1 | integration |
| DOC-096 | Withdraw is one transaction | `TestDoc_Withdraw_Atomic` | P1 | integration |
| APR-091 | Submitter withdraws a pending document back to draft | `TestApproval_Withdraw_SubmitterBackToDraft` | P1 | integration |
| APR-092 | Withdraw needs the submit right and visibility | `TestApproval_Withdraw_NeedsSubmitRight` | P1 | integration |
| APR-093 | Withdrawn document leaves the approver's list and shows as withdrawn to the requester | `TestApproval_Withdraw_LeavesInbox` | P1 | integration |
| APR-094 | Withdraw is refused outside pending approval | `TestApproval_Withdraw_OnlyFromPending` | P1 | integration |
| APR-095 | Approver approves while the submitter's page is stale (race) | `TestApproval_Withdraw_StaleAfterApprove` | P1 | integration |
| APR-096 | Re-submit after withdraw opens a new request on current rules and amount | `TestApproval_Withdraw_ResubmitNewRequest` | P1 | integration |
| APR-097 | Requester withdraws from the browser and the approver's count drops | `TestE2E_ApprovalWithdraw` (renamed by CD-72) | P1 | e2e |

### Go test renames

| ID | Old | New | Why |
|---|---|---|---|
| APR-027 | `TestApprovalSubmit_LocksAndNumbers` | `TestApprovalSubmit_LocksWithoutNumber` | R1: no number at submit |
| FILE-050 | `TestPrint_RequiresRead` | `TestPrint_RequiresPrintRight` | R3: print needs the `print` right |
| NUM-156 | `TestImport_NeedsCreateRight` | `TestImport_NeedsImportRight` | R4: import needs the `import` right |
| FILE-073 … FILE-078 | `TestInWordsINR` (shared with FILE-072) | `TestInWordsINR_ZeroAndPaiseOnly`, `_WholeRupees`, `_BelowThousand`, `_Thousands`, `_Lakh`, `_Crore` | unique Go test names |

### 01-auth.md
- Test-case intro: fixture table of users, roles, seeded rights rows and restrictions; POs default to PRJ-METRO; unnumbered documents show `Draft {id}` (R1, R5).
- AUTH-073: `Draft 4`; adds revise of a rejected document needing `write` (R1, R4).
- AUTH-074: withdraw and close added beside submit, all needing `submit`; extra role `Purchase Clerk` and user `clerk@vardhan.test` created in the test (R2, R4).
- AUTH-075: reject needs `approve`; there is no reject right (R4).
- AUTH-077: amendment shows `Draft {id}` until approved as `PO/26-27/00007-1` (R1).
- AUTH-079: read alone does not allow print; 404 for an out-of-scope PO; extra role `Purchase Viewer` created in the test (R3).
- AUTH-084: Priya sees Withdraw on her pending PO; Ravi does not (R2).
- AUTH-070, 076, 101: approved numbers `PO/26-27/000NN` (R1, R5).
- AUTH-071, 078, 087: drafts shown as `Draft {id}` (R1).
- AUTH-063, 066, 069, 113: `auth.Service.Can` / `Scope` returning `auth.ErrForbidden` (§5).
- AUTH-007, 008: disabled user is fixture `old@vardhan.test` (R5).
- AUTH-052, 054, 055, 061: extra users (`kiran@`, `nisha@`, `store1@`, `store2@vardhan.test`) created in the test (R5).
- AUTH-061, 062, 065, 068: Store User → Stores; fixture sites (R5).
- AUTH-089: Accounts User → Director added in the test (R5).
- AUTH-091–100: site and project restrictions use Suresh (PUNE-WH, METRO-SITE) and Priya (PRJ-METRO) (R5).
- AUTH-001–006, 009–062, 064, 067, 072, 080–083, 085, 086, 088, 090, 096, 102–112 (all other cases with names): fixture names, emails, password, clock, numbers (R5).
- Sections: D-AUTH-10 adds the R4 action→right mapping, no `reject`/`close` right; D-AUTH-13 example uses `PUNE-WH`; Interfaces rewritten to §5 names, print needs `print` (R3), NAM assigns at first approval (R1).

### 02-naming.md
- Intro/scope: points to 00-fixtures.md; numbering settings are Administrator-only by role (not one of the 11 rights) (R4, R5).
- NAM-004: second date uses fixture clock (R5).
- NAM-022: user ravi, reference PO (R5).
- NAM-023: Priya submits reference PO, Ravi approves; pending shows `Draft 42` in list, form and inbox (R1, R5).
- NAM-024: direct-approve example is now a GRN (`GRN/26-27/00001`) because the seed Expense rule (≥ ₹0) always needs approval (R5).
- NAM-025: retitled "A rejected or withdrawn document stays unnumbered…"; rejected state corrected to `rejected`; revise back to draft; adds withdraw: no number, counter unchanged, next number on later approval (R1, R2, R4).
- NAM-027: creation times on fixture date (R5).
- NAM-031, NAM-063: GRN has no rule, so a failed submit stays `draft` (not `pending_approval`) (R5).
- NAM-037: fuzz action list includes withdraw and revise (R2, R4).
- NAM-043, NAM-044: label written `Draft 51` (R1).
- NAM-051, 052, 054, 055, 056, 057, 058, 062: admin is fixture user Anil Kapoor; fixture clock (R5).
- NAM-059: date stated relative to fixture clock (R5).
- NAM-060: non-admins are ravi and priya (R5).
- NAM-061: admin Anil Kapoor; audit time `2026-10-24T05:00:00.000Z` (R5).
- NAM-064: Ravi approves; audit entry `approve`, `pending_approval → approved` (R1, R5).
- NAM-065: printer Ravi with `print` right; pending PO prints `Draft 42` with watermark (R1, R3).
- Sections: D-NAM-01 confirmed by R1 (and withdraw); Interfaces rewritten to §5 names (`naming.Service.Assign/AssignAmendment/Preview/ValidateTemplate`, `doc.Engine.Do`, `types.FiscalYear`, `auth.Service.HasRole`, `print.Register`).

### 03-document.md
- Intro and Hooks contract: lists withdraw and revise; numbers only at first approval, unnumbered show `Draft {id}`; Hooks block aligned to §5 `doc.Hooks` (`SourceLinks` replaced by `DocType.Links`); `doc.Engine.Do` (R1, R4, §5).
- DOC-001: fixture user/party/item, clock 05:00:00Z, shows `Draft 42` (R1, R5).
- DOC-004: adds pending_approval+withdraw→draft; "rejected+edit" renamed "rejected+revise" (R2, R4).
- DOC-005: messages name `Draft 42` (draft/pending/rejected) or `PO/26-27/00001`; withdraw refusals from draft, approved (incl. stale second click), rejected, closed, cancelled; pending save/submit/cancel point to Withdraw; rejected says "Revise"; closed+amend now "is closed and can't be changed"; rejected+close and rejected+amend added (R1, R2, R4).
- DOC-006: auto-approved receipt gets `GRN/26-27/00001` in the submit transaction, `Assign` called once (R1).
- DOC-007: pending has no number, `Assign` not called (R1).
- DOC-008: order CheckApprover → Validate → BeforeSubmit → NAM Assign → ApplyEffects → roll-up (R1, §3.2).
- DOC-009, DOC-010: "edit" → "revise"; no number while rejected or revised; fixture item/rates (R1, R4, R5).
- DOC-011: double submit message names `Draft 42`; auto-approved variant (R1).
- DOC-016: withdraw added to refused list; reopen later (R2, R6).
- DOC-027, DOC-070: withdraw and revise added (R2, R4).
- DOC-037, DOC-069: messages name `Draft 42` for unnumbered documents (R1).
- DOC-062: pending delete says "Withdraw it first, then delete it"; rejected says "Revise it…" (R1, R2, R4).
- DOC-064, DOC-065: amendment unnumbered (`Draft 43`) until approved as `PO/26-27/00001-1` (R1).
- DOC-076: failed transaction leaves no number and counter unchanged (R1).
- DOC-079: fuzz actions include withdraw/revise; new invariant "numbered exactly when approved, closed or cancelled" (R1, R2).
- DOC-083: audit sequence includes withdraw and revise; number first appears on approve (R1, R2).
- DOC-092: e2e uses Priya / Ravi / Suresh, reference PO, `Draft 42` then `PO/26-27/00001` (R1, R5).
- DOC-094: pending shows Withdraw to users with submit right; rejected shows Revise (R2, R4).
- DOC-017, 028, 034, 035, 036, 038, 045, 049, 050, 052, 054, 067, 072, 080, 082, 084, 087, 089: fixture parties, items, numbers, users, clock (R5).
- Sections: D-DOC-07 kept by R7; D-DOC-08 `submit` right, terminal kept by R6; D-DOC-14 amendment numbered on approval (R1); new D-DOC-23 (number timing, R1) and D-DOC-24 (withdraw, R2); Deferred rows note R6/R7; Interfaces rewritten to §5 names, no `reject`/`close` right, "no such transition in v1" removed, print needs `print` right (R3).

### 04-approvals.md
- New section "Withdraw (spec R2)" with APR-091 to APR-097 (see New cases) (R2).
- APR-027: retitled "Submit locks the document but gives it no number"; no number at submit; pending edit says "Withdraw it to make changes"; Go test renamed `TestApprovalSubmit_LocksAndNumbers` → `TestApprovalSubmit_LocksWithoutNumber` (R1, R2).
- APR-015, 016, 024, 025, 026, 029, 038, 052, 053, 057, 065, 085: number assigned in the approve (or auto-approve submit) transaction; pending shows `Draft {id}` (R1).
- APR-055: message names `Draft {id}` for unnumbered documents (R1).
- APR-063: revise keeps no number, still `Draft 42`; needs `write` (R1, R4).
- APR-064: revise needs `write`; 403 uses AUTH wording (R4).
- APR-069: request is closed "rejected", not "withdrawn"; "withdrawn" is only for the withdraw action (R2).
- APR-071, 072: stale message and single number `PO/26-27/00001` (R1).
- APR-073: pending edit message "Draft 42 is waiting for approval and can't be changed. Withdraw it to make changes." (R1, R2).
- APR-074: offered actions are Approve and Reject only (Ravi has no `submit` right on PO in the seeded matrix; matches AUTH-084) (R2, R4).
- APR-075: delete by Priya and cancel by Ravi give the DOC-062 / DOC-005 messages pointing to Withdraw (R2, R4).
- APR-076, APR-089: withdraw added to allowed changes and fuzz actions; invariants "number only when approved/closed/cancelled", "no open request on a draft" (R1, R2).
- APR-087, APR-088: amendment and copy have no number until approved (`PO/26-27/00042-1`) (R1).
- APR-030, 035, 036, 040, 082: Ravi is given Purchase User in the test so he can create/submit (seeded Purchase Manager has no create/submit) (R4, R5).
- APR-001, 002, 006, 010–013, 019–023, 032–034, 037, 039, 041–051, 054, 056, 058–062, 066–068, 070, 077–081, 083, 084, 086: fixture users, roles, seed rules and amounts; extra role `Site Auditor` (APR-006) (R5).
- Sections: D-APR-10 (reject needs `approve`, no number, revise needs `write`); D-APR-12 "Withdraw is in v1 (changed by spec R2)"; D-APR-16 includes withdraw; D-APR-19 Administrators; D-APR-20 history includes "withdrawn"; Deferred withdraw row removed; Interfaces use §5 names and NAM `Assign` at first approval (R1).

### 05-files-print.md
- FILE-050: rewritten for R3. Retitled "Print needs the print right on the DocType and visibility of the record". Not visible → 404 (Priya, PRJ-TECHPARK PO); read without print → 403 (Kavya); read+print → 200 (Ravi). Go test renamed `TestPrint_RequiresRead` → `TestPrint_RequiresPrintRight` (R3).
- FILE-070: Print button and print URL need read + print; read-only user gets no button and 403 (R3).
- FILE-052: draft/pending/rejected prints show `Draft 42` instead of a number (was "Not yet numbered") (R1).
- FILE-053, FILE-054: cancelled / approved / closed prints show `PO/26-27/00001` (R1).
- FILE-055, FILE-057, FILE-071: draft PO is `Draft 42` (R1).
- FILE-058: footer `Draft 42 · Page 1 of 3` for a draft; number once approved (R1).
- FILE-061: print titles for `PO/26-27/00001`, `PO/26-27/00001-1`, `Draft 42` (R1, R5).
- FILE-043, FILE-044: amendment is `Draft 43` until approved as `PO/26-27/00001-1` (R1).
- FILE-051: reference PO, Vardhan header with PUNE GSTIN, vendor SHREE `27AABCS1429B1ZU`; invalid `27AAACG1234F1Z9` removed (R5).
- FILE-063, FILE-064: SHREE snapshot; unregistered vendor `LOCAL` (R5).
- FILE-001, 016, 017, 018, 038, 042: drafts `Draft 42`; GRN `GRN/26-27/00001`; fixture clock (R1, R5).
- FILE-024, 025, 026, 028, 029, 030, 040, 041, 056, 062, 065, 067, 068, 069: fixture users, numbers, sites, items and clock (R5).
- FILE-080: signature takes `types.Money` (§5).
- Sections: D-FILE-15 changed to "print right + visibility (changed by spec R3)"; D-FILE-14 adds `Draft {id}` (R1); D-FILE-16 uses `print.InWordsINR(types.Money)`; Interfaces use §5 names (`auth.Service.Can`, `files.Service.CopyAll/DeleteAll`, `print.Register`, `naming.Service.Assign`).

### 06-masters.md
- MST-001, 002, 003, 017, 006, 009, 040, 042, 043, 044, 045, 048, 050, 051, 052, 058, 061, 063, 066, 068: Sharma Steel / `V-SHARMA` / Tata etc. replaced by fixture parties (`SHREE`, `BHARAT`, `COROM`) with their addresses and GSTINs; extra parties created in the Given where needed (R5).
- MST-008: title and Given say "Masters Manager role"; Ravi with Party `write` but no Masters Manager role cannot rename; Kavya can (R4, R5).
- MST-010, MST-014: draft PO message names `Draft 42` (R1).
- MST-011: print view needs the PO `print` right (R3).
- MST-012, 090–093: approved/cancelled numbers in `PO/26-27/000NN` / `GRN/26-27/00001` format (R5).
- MST-019–MST-029: company Vardhan (PAN `AAFCV2449G`), branches `PUNE` / `BLR` with fixture GSTINs; head office is `PUNE` (was `HO`); extra branches `MUM` / `NASHIK` created in the test (R5).
- MST-026: bad-checksum GSTIN is now `27AABCS1429B1Z5` (R5).
- MST-031, MST-036: `24AAUPV7468F1ZW` replaced by RAJKOT `24AAACR5055K1ZD` (R5).
- MST-038: state codes 99 and 25 kept on checksum-valid GSTINs (R5).
- MST-049: `COROM` made unregistered in the test (R5).
- MST-004, 007, 018, 070, 073, 075, 078–087: fixture items, SAC, UoMs (BOX = 100, DRM = 500); MST-087 uses test item `TEST-CLIP` because BOX is whole-number (R5).
- MST-100, 102, 103, 105–108: fixture projects and sites; dates in 2026 (R5).
- MST-111–MST-120: fixture employees and reporting chain; MST-111 re-enables the user before linking (consistency with MST-112/119) (R5).
- MST-123: `HOTEL` → `LODGING` (R5).
- Sections: Scope and D-MST-02 say "Masters Manager role" (R4); D-MST-25 confirmed by R5; test-data paragraph rewritten to fixtures with the list of kept non-fixture GSTINs; Interfaces use §5 names (`doc.Engine.IsReferenced`, `auth.Service.HasRole/Can/DisableUser`).

### 07-numbers-lists.md
- NUM-007, NUM-014, NUM-053, NUM-148: fixture items and UoMs (`BOLT-M12`, `MS-ANGLE-50`, "kg"); NUM-014's 0.6 factor stated as set up in the test (R5).
- NUM-026, NUM-030: use and check the reference PO figures (₹79,800.00 / ₹7,182.00 ×2 / ₹94,164.00) (R5).
- NUM-031, NUM-037, NUM-038, NUM-040, NUM-041, NUM-042: fixture parties and branches for intra/inter-state GST (SHREE→PUNE, BHARAT→PUNE, SHREE→BLR, LOCAL unregistered; SHREE set to SEZ in-test) (R5).
- NUM-039: "Nashik Store" becomes extra branch `NASHIK` created in the test (R5).
- NUM-074, NUM-075: fixture-clock examples (`2026-10-24T05:00:00.000Z` → 24-10-2026, FY 2026-27, "24-10-2026 10:30") (R5).
- NUM-100: approved rows `PO/26-27/00001..00005`; draft/pending rows show `Draft {id}` (R1).
- NUM-119: search finds `PO/26-27/00042`; a draft `Draft 43` is not found by number (R1, R5).
- NUM-101, 102, 103, 104, 105, 107, 108, 115, 116, 120, 123, 124, 140, 147: fixture projects, parties, items, numbers, admin, dates moved to 2026; extra parties/items created in the Given where the test needs them (R5).
- NUM-121: Priya restricted to PRJ-METRO; adds Suresh site scope check (R5).
- NUM-122, NUM-160: users Deepak / Suresh; 403 text follows AUTH wording (R4, R5).
- NUM-156: import needs the `import` right plus `create` per row; Go test renamed `TestImport_NeedsCreateRight` → `TestImport_NeedsImportRight` (R4).
- NUM-159: Priya (PRJ-METRO) with export right (R5).
- Sections: scope lists read/export/import rights; Decisions flag note replaced by a fixtures note; Interfaces use §5 names (`auth.Service.Can/Scope`, `list.Build`, `types.FiscalYear`, `clock.Clock`, `print.InWordsINR`, `gstcalc.LineAmount/SplitGST/IsInterState/Totals`).

### Cross-file fixes (made after the per-file pass)
- DOC-005: approve/reject from any non-pending state now uses APR-055/APR-057's message "<name> is not waiting for approval. It is <State>."; pending save/submit message unified with APR-027/APR-073 ("…can't be changed. Withdraw it to make changes."); closed+amend uses the closed message; rejected+close and rejected+amend added.
- DOC-016: approve on a closed document gets the DOC-005 "not waiting for approval" message.
- APR-027, APR-073: "can't be edited" → "can't be changed" (same text as DOC-005).
- APR-075: same delete/cancel messages as DOC-062/DOC-005; deleter is Priya (Ravi has no `delete` right).
- APR-074: no Withdraw for Ravi (seeded Purchase Manager has no `submit` on PO), matching AUTH-084.
- APR-092: the non-submitter who withdraws is Anil (Administrator) instead of Ravi.
- APR-030, 035, 036, 040, 082: Ravi given Purchase User in the test (see above).
- DOC-001, DOC-030, DOC-080, DOC-089, NAM-022, AUTH-078: the creator of a PO is Priya, not Ravi (seeded Purchase Manager has no `create`); DOC-030/DOC-089 second writer is Anil.
- FILE-073 … FILE-078: Go tests renamed from the shared `TestInWordsINR` to `TestInWordsINR_ZeroAndPaiseOnly`, `_WholeRupees`, `_BelowThousand`, `_Thousands`, `_Lakh`, `_Crore` so every Go test name is unique (FILE-072 keeps `TestInWordsINR`).

### Open points for the spec owner

- Spec §3 table counts are now out of date: DOC 96 (68 / 28), APR 97 (72 / 25), total 706 (544 / 162).
- `00-fixtures.md` has no seeded rights matrix. `01-auth.md` defines one (Purchase Manager: PO read, approve, cancel, amend, print, export; no create, write, submit or delete). The other files now follow it. Recommend moving that matrix, and the UoM codes assumed in `06-masters.md` (NOS, KGS, MTR, BOX, DRM, BAG, HRS), into `00-fixtures.md`.
- 403 message text is written in several files. Recommend AUTH owns a single pattern: "You don't have permission to {right} {DocType}. Ask your administrator for a role that allows it."
- Suspected overlaps across files, kept on purpose (different layer): NAM-065 / FILE-052 (draft print label); DOC-095 / APR-091 (withdraw happy path); DOC-005 / APR-094 (withdraw refusals); AUTH-079 / FILE-050 (print right); AUTH-080, AUTH-081 / NUM-160, NUM-156 (export and import rights).
