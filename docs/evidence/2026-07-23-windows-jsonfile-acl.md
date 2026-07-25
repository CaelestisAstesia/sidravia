# Stage 1 Evidence: Windows jsonfile ACL 现场验证

- 日期：2026-07-23
- 源码提交：`f843755a309a0a4d6f1ed3bf6cba2f1d0e09d43a`
- Windows：`Microsoft Windows NT 10.0.26200.0`
- Windows 用户 SID：`S-1-5-21-3611674850-300571451-4219134381-1001`
- Go：`go version go1.26.4 windows/amd64`
- 进程与文件系统：真实 Windows 进程；模块副本与被测临时文件均位于 NTFS（%LOCALAPPDATA%\Temp）。
- 准确命令：在 NTFS 副本目录执行 `go.exe test -count=1 -v ./internal/daemon/persistence/jsonfile/`。
- 结论：新建目录/文件 DACL 收紧为受保护的 owner+SYSTEM、替换不放宽 ACL、宽松父继承被中和、ACL 失败不留明文目标，全部在真实 Windows 上验证通过。

完整输出：

~~~text
=== RUN   TestSecureStoreSatisfiesStore
--- PASS: TestSecureStoreSatisfiesStore (0.00s)
=== RUN   TestSecureStoreRejectsRelativeOrNonJSONReplacementBeforeOperations
=== RUN   TestSecureStoreRejectsRelativeOrNonJSONReplacementBeforeOperations/relative
=== RUN   TestSecureStoreRejectsRelativeOrNonJSONReplacementBeforeOperations/unclean
=== RUN   TestSecureStoreRejectsRelativeOrNonJSONReplacementBeforeOperations/invalid_JSON
=== RUN   TestSecureStoreRejectsRelativeOrNonJSONReplacementBeforeOperations/multiple_JSON
=== RUN   TestSecureStoreRejectsRelativeOrNonJSONReplacementBeforeOperations/newline_suffix
--- PASS: TestSecureStoreRejectsRelativeOrNonJSONReplacementBeforeOperations (0.00s)
    --- PASS: TestSecureStoreRejectsRelativeOrNonJSONReplacementBeforeOperations/relative (0.00s)
    --- PASS: TestSecureStoreRejectsRelativeOrNonJSONReplacementBeforeOperations/unclean (0.00s)
    --- PASS: TestSecureStoreRejectsRelativeOrNonJSONReplacementBeforeOperations/invalid_JSON (0.00s)
    --- PASS: TestSecureStoreRejectsRelativeOrNonJSONReplacementBeforeOperations/multiple_JSON (0.00s)
    --- PASS: TestSecureStoreRejectsRelativeOrNonJSONReplacementBeforeOperations/newline_suffix (0.00s)
=== RUN   TestSecureStoreReadSecuresDirectoryAndExistingDestinationBeforeOpen
--- PASS: TestSecureStoreReadSecuresDirectoryAndExistingDestinationBeforeOpen (0.00s)
=== RUN   TestSecureStoreReadDistinguishesMissingEmptyAndOversizedFiles
--- PASS: TestSecureStoreReadDistinguishesMissingEmptyAndOversizedFiles (0.00s)
=== RUN   TestSecureStoreReplaceUsesSameDirectorySecureTempAndOneNewline
--- PASS: TestSecureStoreReplaceUsesSameDirectorySecureTempAndOneNewline (0.00s)
=== RUN   TestSecureStoreReplaceOrdersWriteSyncCloseContextAndCommit
--- PASS: TestSecureStoreReplaceOrdersWriteSyncCloseContextAndCommit (0.00s)
=== RUN   TestSecureStoreShortWritePreservesDestinationAndCleansTemp
--- PASS: TestSecureStoreShortWritePreservesDestinationAndCleansTemp (0.00s)
=== RUN   TestSecureStoreWriteSyncAndCloseFailuresPreserveDestination
=== RUN   TestSecureStoreWriteSyncAndCloseFailuresPreserveDestination/write
=== RUN   TestSecureStoreWriteSyncAndCloseFailuresPreserveDestination/sync
=== RUN   TestSecureStoreWriteSyncAndCloseFailuresPreserveDestination/close
--- PASS: TestSecureStoreWriteSyncAndCloseFailuresPreserveDestination (0.00s)
    --- PASS: TestSecureStoreWriteSyncAndCloseFailuresPreserveDestination/write (0.00s)
    --- PASS: TestSecureStoreWriteSyncAndCloseFailuresPreserveDestination/sync (0.00s)
    --- PASS: TestSecureStoreWriteSyncAndCloseFailuresPreserveDestination/close (0.00s)
=== RUN   TestSecureStoreDirectoryAndDestinationHardeningFailuresPrecedeTempCreation
--- PASS: TestSecureStoreDirectoryAndDestinationHardeningFailuresPrecedeTempCreation (0.00s)
=== RUN   TestSecureStoreCancellationBeforeFinalCommitPreservesDestination
--- PASS: TestSecureStoreCancellationBeforeFinalCommitPreservesDestination (0.00s)
=== RUN   TestSecureStorePreCommitFailurePreservesDestinationAndCleansTemp
--- PASS: TestSecureStorePreCommitFailurePreservesDestinationAndCleansTemp (0.00s)
=== RUN   TestSecureStoreCancellationAfterKnownCommitStillReturnsSuccess
--- PASS: TestSecureStoreCancellationAfterKnownCommitStillReturnsSuccess (0.00s)
=== RUN   TestSecureStoreKnownCommitWithDirectorySyncErrorStillReturnsSuccess
--- PASS: TestSecureStoreKnownCommitWithDirectorySyncErrorStillReturnsSuccess (0.00s)
=== RUN   TestSecureStoreInitialCreationFailureLeavesNoDestination
--- PASS: TestSecureStoreInitialCreationFailureLeavesNoDestination (0.00s)
=== RUN   TestSecureStoreTempNameCollisionRetriesWithDeterministicRandomSeam
--- PASS: TestSecureStoreTempNameCollisionRetriesWithDeterministicRandomSeam (0.00s)
=== RUN   TestSecureStoreExhaustedTempNameCollisionsFailBeforePayloadWrite
--- PASS: TestSecureStoreExhaustedTempNameCollisionsFailBeforePayloadWrite (0.00s)
=== RUN   TestSecureStoreCommittedPostCommitFlushDiagnosticStillReturnsSuccess
--- PASS: TestSecureStoreCommittedPostCommitFlushDiagnosticStillReturnsSuccess (0.00s)
=== RUN   TestWindowsOwnerDefaultsToCurrentNonSystemToken
--- PASS: TestWindowsOwnerDefaultsToCurrentNonSystemToken (0.00s)
=== RUN   TestWindowsSystemTokenRequiresExplicitIntendedOwner
    store_windows_test.go:62: test process is not LocalSystem
--- SKIP: TestWindowsSystemTokenRequiresExplicitIntendedOwner (0.00s)
=== RUN   TestWindowsExplicitOwnerSIDSeamAcceptsCanonicalUserSID
--- PASS: TestWindowsExplicitOwnerSIDSeamAcceptsCanonicalUserSID (0.00s)
=== RUN   TestWindowsDirectoryAndFileDACLIsProtectedOwnerAndSystemOnly
--- PASS: TestWindowsDirectoryAndFileDACLIsProtectedOwnerAndSystemOnly (0.01s)
=== RUN   TestWindowsSecurityDescriptorOwnerAndGroupAreIntendedOwner
--- PASS: TestWindowsSecurityDescriptorOwnerAndGroupAreIntendedOwner (0.01s)
=== RUN   TestWindowsSecureTempHasFinalDACLBeforeAnyPayloadWrite
--- PASS: TestWindowsSecureTempHasFinalDACLBeforeAnyPayloadWrite (0.00s)
=== RUN   TestWindowsSecureTempRetriesNameCollisions
--- PASS: TestWindowsSecureTempRetriesNameCollisions (0.00s)
=== RUN   TestWindowsSecureTempExhaustsNameCollisionsBeforePayloadWrite
--- PASS: TestWindowsSecureTempExhaustsNameCollisionsBeforePayloadWrite (0.00s)
=== RUN   TestWindowsReplacementDoesNotWidenACL
--- PASS: TestWindowsReplacementDoesNotWidenACL (0.02s)
=== RUN   TestWindowsPermissiveParentInheritanceIsNeutralized
--- PASS: TestWindowsPermissiveParentInheritanceIsNeutralized (0.01s)
=== RUN   TestWindowsACLFailureLeavesOldBytesAndNoPlaintextDestination
--- PASS: TestWindowsACLFailureLeavesOldBytesAndNoPlaintextDestination (0.00s)
=== RUN   TestWindowsReplaceFileCommitsExistingDestination
--- PASS: TestWindowsReplaceFileCommitsExistingDestination (0.02s)
=== RUN   TestWindowsReplaceFileUsesExactlyZeroFlags
--- PASS: TestWindowsReplaceFileUsesExactlyZeroFlags (0.00s)
=== RUN   TestWindowsMoveFileExWriteThroughCommitsInitialDestination
--- PASS: TestWindowsMoveFileExWriteThroughCommitsInitialDestination (0.00s)
=== RUN   TestReadLimitedAcceptsExactLimitAndRejectsOneByteOver
--- PASS: TestReadLimitedAcceptsExactLimitAndRejectsOneByteOver (0.00s)
=== RUN   TestDecodeStrictRejectsEmptyTrailingUnknownDuplicateAndWrongType
=== RUN   TestDecodeStrictRejectsEmptyTrailingUnknownDuplicateAndWrongType/empty
=== RUN   TestDecodeStrictRejectsEmptyTrailingUnknownDuplicateAndWrongType/trailing_value
=== RUN   TestDecodeStrictRejectsEmptyTrailingUnknownDuplicateAndWrongType/unknown_field
=== RUN   TestDecodeStrictRejectsEmptyTrailingUnknownDuplicateAndWrongType/duplicate_root_key
=== RUN   TestDecodeStrictRejectsEmptyTrailingUnknownDuplicateAndWrongType/wrong_type
--- PASS: TestDecodeStrictRejectsEmptyTrailingUnknownDuplicateAndWrongType (0.00s)
    --- PASS: TestDecodeStrictRejectsEmptyTrailingUnknownDuplicateAndWrongType/empty (0.00s)
    --- PASS: TestDecodeStrictRejectsEmptyTrailingUnknownDuplicateAndWrongType/trailing_value (0.00s)
    --- PASS: TestDecodeStrictRejectsEmptyTrailingUnknownDuplicateAndWrongType/unknown_field (0.00s)
    --- PASS: TestDecodeStrictRejectsEmptyTrailingUnknownDuplicateAndWrongType/duplicate_root_key (0.00s)
    --- PASS: TestDecodeStrictRejectsEmptyTrailingUnknownDuplicateAndWrongType/wrong_type (0.00s)
=== RUN   TestDecodeStrictRejectsNestedOwnedDuplicateKeys
--- PASS: TestDecodeStrictRejectsNestedOwnedDuplicateKeys (0.00s)
=== RUN   TestDecodeStrictSkipsOnlyTheExactOpaqueJSONPointer
--- PASS: TestDecodeStrictSkipsOnlyTheExactOpaqueJSONPointer (0.00s)
=== RUN   TestStrictJSONValidationRejectsInvalidUTF8
=== RUN   TestStrictJSONValidationRejectsInvalidUTF8/decode_strict
=== RUN   TestStrictJSONValidationRejectsInvalidUTF8/require_fields
=== RUN   TestStrictJSONValidationRejectsInvalidUTF8/require_schema
=== RUN   TestStrictJSONValidationRejectsInvalidUTF8/validate_opaque
=== RUN   TestStrictJSONValidationRejectsInvalidUTF8/raw_envelope
--- PASS: TestStrictJSONValidationRejectsInvalidUTF8 (0.00s)
    --- PASS: TestStrictJSONValidationRejectsInvalidUTF8/decode_strict (0.00s)
    --- PASS: TestStrictJSONValidationRejectsInvalidUTF8/require_fields (0.00s)
    --- PASS: TestStrictJSONValidationRejectsInvalidUTF8/require_schema (0.00s)
    --- PASS: TestStrictJSONValidationRejectsInvalidUTF8/validate_opaque (0.00s)
    --- PASS: TestStrictJSONValidationRejectsInvalidUTF8/raw_envelope (0.00s)
=== RUN   TestOpaqueAndRawValidationAcceptLargeJSONNumbers
--- PASS: TestOpaqueAndRawValidationAcceptLargeJSONNumbers (0.00s)
=== RUN   TestRequireObjectFieldsDistinguishesMissingFromZeroValue
--- PASS: TestRequireObjectFieldsDistinguishesMissingFromZeroValue (0.00s)
=== RUN   TestRequireSchemaVersionAcceptsOnlyIntegerOne
=== RUN   TestRequireSchemaVersionAcceptsOnlyIntegerOne/missing
=== RUN   TestRequireSchemaVersionAcceptsOnlyIntegerOne/null
=== RUN   TestRequireSchemaVersionAcceptsOnlyIntegerOne/string
=== RUN   TestRequireSchemaVersionAcceptsOnlyIntegerOne/fractional
=== RUN   TestRequireSchemaVersionAcceptsOnlyIntegerOne/negative
=== RUN   TestRequireSchemaVersionAcceptsOnlyIntegerOne/zero
=== RUN   TestRequireSchemaVersionAcceptsOnlyIntegerOne/one
=== RUN   TestRequireSchemaVersionAcceptsOnlyIntegerOne/two
--- PASS: TestRequireSchemaVersionAcceptsOnlyIntegerOne (0.00s)
    --- PASS: TestRequireSchemaVersionAcceptsOnlyIntegerOne/missing (0.00s)
    --- PASS: TestRequireSchemaVersionAcceptsOnlyIntegerOne/null (0.00s)
    --- PASS: TestRequireSchemaVersionAcceptsOnlyIntegerOne/string (0.00s)
    --- PASS: TestRequireSchemaVersionAcceptsOnlyIntegerOne/fractional (0.00s)
    --- PASS: TestRequireSchemaVersionAcceptsOnlyIntegerOne/negative (0.00s)
    --- PASS: TestRequireSchemaVersionAcceptsOnlyIntegerOne/zero (0.00s)
    --- PASS: TestRequireSchemaVersionAcceptsOnlyIntegerOne/one (0.00s)
    --- PASS: TestRequireSchemaVersionAcceptsOnlyIntegerOne/two (0.00s)
=== RUN   TestValidateOpaqueObjectOrNullRejectsScalarArrayAndInvalidJSON
=== RUN   TestValidateOpaqueObjectOrNullRejectsScalarArrayAndInvalidJSON/object
=== RUN   TestValidateOpaqueObjectOrNullRejectsScalarArrayAndInvalidJSON/null
=== RUN   TestValidateOpaqueObjectOrNullRejectsScalarArrayAndInvalidJSON/scalar
=== RUN   TestValidateOpaqueObjectOrNullRejectsScalarArrayAndInvalidJSON/array
=== RUN   TestValidateOpaqueObjectOrNullRejectsScalarArrayAndInvalidJSON/invalid
--- PASS: TestValidateOpaqueObjectOrNullRejectsScalarArrayAndInvalidJSON (0.00s)
    --- PASS: TestValidateOpaqueObjectOrNullRejectsScalarArrayAndInvalidJSON/object (0.00s)
    --- PASS: TestValidateOpaqueObjectOrNullRejectsScalarArrayAndInvalidJSON/null (0.00s)
    --- PASS: TestValidateOpaqueObjectOrNullRejectsScalarArrayAndInvalidJSON/scalar (0.00s)
    --- PASS: TestValidateOpaqueObjectOrNullRejectsScalarArrayAndInvalidJSON/array (0.00s)
    --- PASS: TestValidateOpaqueObjectOrNullRejectsScalarArrayAndInvalidJSON/invalid (0.00s)
=== RUN   TestMarshalDeterministicUsesOwnedStructOrderWithoutNewline
--- PASS: TestMarshalDeterministicUsesOwnedStructOrderWithoutNewline (0.00s)
=== RUN   TestMarshalRawRecordEnvelopePreservesEveryRawElementByte
--- PASS: TestMarshalRawRecordEnvelopePreservesEveryRawElementByte (0.00s)
=== RUN   TestMarshalRawRecordEnvelopeRejectsOversizedOrFramingInvalidElement
=== RUN   TestMarshalRawRecordEnvelopeRejectsOversizedOrFramingInvalidElement/oversized
=== RUN   TestMarshalRawRecordEnvelopeRejectsOversizedOrFramingInvalidElement/multiple_values
=== RUN   TestMarshalRawRecordEnvelopeRejectsOversizedOrFramingInvalidElement/empty
--- PASS: TestMarshalRawRecordEnvelopeRejectsOversizedOrFramingInvalidElement (0.00s)
    --- PASS: TestMarshalRawRecordEnvelopeRejectsOversizedOrFramingInvalidElement/oversized (0.00s)
    --- PASS: TestMarshalRawRecordEnvelopeRejectsOversizedOrFramingInvalidElement/multiple_values (0.00s)
    --- PASS: TestMarshalRawRecordEnvelopeRejectsOversizedOrFramingInvalidElement/empty (0.00s)
PASS
ok  	sidravia/internal/daemon/persistence/jsonfile	0.352s
~~~
