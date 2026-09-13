package storage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// Executable group-level review manifest, not a claim of branch coverage. Private
// helpers are exercised through their public entry points. Function references
// deliberately make renamed/deleted detailed tests a compile failure. These tests
// remain independently runnable; no definition/type/call-site golden is relaxed.
// Cross-package integration is intentionally retained in apiserver, not copied:
// TestTenantAcceptanceConcurrentHTTP forces graph reconstruction;
// TestTenantAcceptanceLegacyParentAndReusedPaths runs RebuildAll and path reuse;
// TestTenantAcceptanceHeldHTTPAndSuspension covers held eviction/suspension.
// Run separately with:
// go test ./internal/apiserver -run '^TestTenantAcceptance' -count=1
// These references do not claim those integration tests ran in this phase.
var tenantCoverageManifest = []struct {
	name, methods string
	tests         []func(*testing.T)
}{
	{"notes", "InsertNote GetNoteByPath GetNoteByShortID GetNoteByTitle GetNoteByTitleScoped MergeMetadata UpdateNote DeleteNote", []func(*testing.T){TestTenantNotesCRUDIsolation, TestTenantNotesTitleRanking, TestTenantNotesTitleFallbackIsolation, TestTenantNotesForeignOnlyLookupsNotFound, TestTenantNotesExecutionSchemaGuard, TestTenantNotesCorruptChildOwnership, TestTenantNotesInsertRepair}},
	{"list", "ListNotes listQuery listNotes", []func(*testing.T){TestTenantNotesListFilters}},
	{"search", "SearchNotes searchFTS ftsMatchWords ftsMatchWordsResult ftsMatch searchExact searchLike searchAttachmentDerivedText searchTenant", []func(*testing.T){TestTenantSearchIsolation, TestTenantSearchFiltersAndPagination, TestTenantSearchAttachmentOwnership, TestTenantSearchPublicStrategyFallback, TestTenantSearchReceiverSnapshot, TestTenantSearchReceiverForeignCorpusStability, TestTenantSearchReceiverConflictMaintenance, TestTenantSearchSchemaBeforeEmpty}},
	{"graph", "GetBacklinks GetOutlinks GetRelated GetOrphans graphScope", []func(*testing.T){TestTenantGraphExactForeignPaths, TestTenantGraphIdenticalCorpusAndForeignMutation, TestTenantGraphExecutionGuards, TestTenantGraphLiteralPrefixes, TestTenantGraphCorruptChildEndpoints}},
	{"links", "SetLinks GetLinks ResolveLinksTo ResolveLinksToNote resolveLinksTo", []func(*testing.T){TestTenantLinksResolutionPrecedence, TestTenantLinksDelayedGlobalRepairKeepsResolved, TestTenantLinksTagsReplacePreservesForeign, TestTenantRepairMetadataAdmission, TestTenantRepairRejectsForgedTarget}},
	{"tags", "SetTags GetTags", []func(*testing.T){TestTenantLinksAndTagsRoundTrip}},
	{"attachments", "CreateAttachment GetAttachment GetAttachmentByDigest ListAttachments LinkAttachmentToEntry UnlinkAttachmentFromEntry ListAttachmentsForEntry ListEntryReferencesForAttachment CountAttachmentReferences UpsertAttachmentDerived GetAttachmentDerived ListAttachmentDerived DeleteAttachmentIfUnreferenced", []func(*testing.T){TestTenantAttachmentsIsolation, TestTenantAttachmentsGuards, TestTenantAttachmentsAttachDeleteAtomic}},
	{"embeddings", "UpsertNoteEmbeddings GetNoteEmbedding EmbeddingStatus SyncNoteEmbeddingMetadata DeleteNoteEmbeddings SearchByEmbedding", []func(*testing.T){TestTenantEmbeddingCandidatesBeforeLimit, TestTenantEmbeddingMutationOwnership, TestTenantEmbeddingGuardsBeforeFastPaths, TestTenantEmbeddingMigratedSchemaNeverFallsBack}},
	{"embedding-index", "ListEmbeddingNotes EmbeddingSource EmbeddingHealthCounts", []func(*testing.T){TestTenantEmbeddingIndexStorage}},
	{"events", "InsertEvent MarkProcessed GetEventsByType GetUnprocessed", []func(*testing.T){TestTenantEventsIsolation, TestTenantEventsGuards, TestTenantEventsV29NoFallback}},
	{"index-maintenance", "ListIndexedNoteStates DeleteAllNotes", []func(*testing.T){TestTenantCollisionRuntimeReads, TestTenantCollisionRuntimeWrites}},
	{"metadata", "RecordAccess GetAccessStats SetVerified GetStaleEntries", []func(*testing.T){TestTenantPhaseOneIsolation, TestTenantPhaseOneSchema28, TestTenantCollisionConcurrentWrites}},
	{"triggers", "ListTriggeredTasks CountInProgressByTrigger ActivateTask", []func(*testing.T){TestTenantPhaseOneIsolation, TestTenantPhaseOneSchema28}},
	{"project-pause", "SetProjectTaskPaused SetProjectAutomationsPaused setProjectPauseColumn SetAllProjectTasksPaused SetAllProjectAutomationsPaused IsProjectTaskPaused IsProjectAutomationsPaused isProjectPauseColumn ListProjectPauseStates listKnownProjectIDs", []func(*testing.T){TestTenantProjectPolicyIsolation, TestTenantProjectPolicyScopeGuards}},
	{"feature-pause", "SetFeaturePaused ListPausedFeatures IsFeaturePaused", []func(*testing.T){TestTenantProjectPolicyIsolation, TestTenantProjectPolicyScopeGuards}},
	{"cascade", "UpsertFeatureCascadeRoot DeleteFeatureCascadeRoot ListFeatureCascadeRoots", []func(*testing.T){TestTenantProjectPolicyIsolation, TestTenantProjectPolicyScopeGuards}},
	{"placement", "GetProjectPlacement UpsertProjectPlacement", []func(*testing.T){TestTenantProjectPolicyIsolation, TestTenantProjectPolicyScopeGuards}},
	{"claims", "ClaimTask ReleaseClaim GetClaim GetClaimsByRunner ExpireStaleClaims ReleaseAllByRunner RenewClaim", []func(*testing.T){TestTenantClaimsLifecycle, TestTenantClaimAssignmentContention, TestTenantClaimAssignmentForeignReferences, TestTenantClaimAssignmentScopeGuards}},
	{"assignments", "AssignFeatureIfEmpty ForceAssignFeature GetFeatureAssignment ClearFeatureAssignment ClearFeatureAssignmentsByRunner ListFeatureAssignmentsByRunner ListFeatureAssignmentsByProject", []func(*testing.T){TestTenantFeatureAssignmentsLifecycle, TestTenantClaimAssignmentForeignReferences}},
	{"dispatch", "CreateDispatchLease GetDispatchLeaseRow AckDispatchLease RejectDispatchLease ReleaseDispatchLease ClearDispatchLease ExpireDispatchLeases RecordPlacementReason ListPlacementReasonRows ListPlacementReasonRowsLimit PrunePlacementReasonsForTask GetDispatchLease ListPlacementReasons ListPlacementReasonsLimit ListExpiredDispatchLeases", []func(*testing.T){TestTenantDispatchLifecycle, TestTenantDispatchReasons, TestTenantDispatchReferences, TestTenantDispatchScopeGuards}},
	{"runners", "UpsertRunner GetRunner ListRunners ListRunnersByStatus DeleteRunner UpdateHeartbeat UpdateRunnerDispatchMetadata UpdateRunnerCapabilities UpdateAffinity SetRunnerStatus UpdateRunnerMaxParallel ExpireStaleRunners SetRunnerPaused", []func(*testing.T){TestTenantRegistryCollisions, TestTenantRegistrySweepPauseDurability, TestTenantRegistryScopeGuards}},
	{"instances", "UpsertInstance DeleteInstance DeleteInstancesByRunner GetInstance ListInstancesByRunner ListAllInstances ReplaceInstancesForRunner", []func(*testing.T){TestTenantRegistryForeignReferencesAndReplacementRollback, TestTenantCollisionReplacementRollback, TestTenantCollisionForeignRelationships}},
	{"clients", "UpsertBrainClient GetBrainClient UpsertBrainClientWorkspace ListBrainClientWorkspaces", []func(*testing.T){TestTenantRegistryCollisions, TestTenantRegistryKeyAtomicity}},
	{"webhooks", "CreateWebhook GetWebhook ListWebhooks UpdateWebhook DeleteWebhook CreateDelivery ListDeliveries", []func(*testing.T){TestPhase6WebhookIsolation, TestPhase6ScopeGuards}},
	{"stats", "GetStats", []func(*testing.T){TestTenantPhaseOneStats}},
	{"purge", "ListProjectNotePaths PurgeProjectState DeleteProjectNotes", []func(*testing.T){TestPhase6ProjectPurgeIsolation, TestTenantCollisionPurgeRollback}},
	{"execution-scope", "executionScope", []func(*testing.T){TestExecutionLedgersScopeGuards, TestExecutionLedgersMainProfiles, TestExecutionLedgersOwnershipConstraints, TestExecutionLedgersExactTableInventory, TestExecutionLedgersReopen, TestExecutionLedgersLimitsAndForeignReads, TestExecutionLedgersInvalidHandles, TestExecutionLedgersBootstrapRefusal}},
	{"bulk-ledger", "InsertBulkJob readBulkJobs ListBulkJobs GetBulkJob BulkJobByRequest BulkJobItems ClaimBulkJobItem FinishBulkJobItem TransitionBulkJob RetryBulkJob RecoverBulkJobs NextRunnableBulkJob QuarantineBulkJobItems", []func(*testing.T){TestExecutionLedgersBulkIsolation, TestExecutionLedgersBulkMutationIsolation}},
	{"budget-ledger", "ConfigureExecutionBudget ExecutionBudget ReserveBudget SettleBudget", []func(*testing.T){TestExecutionLedgersBudgets}},
	{"checkpoint-ledger", "SupervisorCheckpoints SupervisorCheckpoint CompareSupervisorCheckpoint SupervisorCheckpointVersions", []func(*testing.T){TestExecutionLedgersCheckpointsAndReceipts}},
	{"operation-ledger", "BeginSupervisorOperation SupervisorOperation FinishSupervisorOperation", []func(*testing.T){TestExecutionLedgersCheckpointsAndReceipts}},
	{"entry-sync", "syncScope ReadEntryChanges ReadSelectedEntries ReserveSyncOperation CompleteSyncOperation SyncDevices SaveSyncDevice SyncNote", []func(*testing.T){TestTenantSyncIsolation, TestTenantSyncTriggerReplacement, TestSuccessorReceiverRouting, TestTenantSyncGuards, TestTenantSyncMainProfiles, TestTenantSyncConcurrentCAS, TestSuccessorElevenTableInventory, TestSuccessorSearchRouting, TestSuccessorPartialLedgerRefused}},
}

func tenantCoverageTestName(test func(*testing.T)) string {
	name := runtime.FuncForPC(reflect.ValueOf(test).Pointer()).Name()
	return name[strings.LastIndex(name, ".")+1:]
}

func TestTenantWorkloadMethodInventory(t *testing.T) {
	want := map[string]string{}
	for _, group := range tenantCoverageManifest {
		if len(group.tests) == 0 {
			t.Fatalf("no executable tests for %s", group.name)
		}
		for _, method := range strings.Fields(group.methods) {
			if old, ok := want[method]; ok {
				t.Fatalf("duplicate %s in %s and %s", method, old, group.name)
			}
			want[method] = group.name
		}
	}
	if len(want) != 182 {
		t.Fatalf("manifest has %d workload methods, want 182", len(want))
	}
	files, err := filepath.Glob("*.go")
	collisionMust(t, err)
	actual := map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		collisionMust(t, err)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil {
				continue
			}
			r := fn.Recv.List[0].Type
			if ptr, ok := r.(*ast.StarExpr); ok {
				r = ptr.X
			}
			id, ok := r.(*ast.Ident)
			if !ok || id.Name != "TenantStore" {
				continue
			}
			// Binding/routing are explicitly not workload; their guards stay in
			// the detailed suites and exact production type surface proof.
			if fn.Name.Name == "TenantID" || fn.Name.Name == "contentScope" {
				continue
			}
			actual[fn.Name.Name] = true
			if _, ok := want[fn.Name.Name]; !ok {
				t.Errorf("unmapped workload method %s (%s)", fn.Name.Name, path)
			}
		}
	}
	for method := range want {
		if !actual[method] {
			t.Errorf("stale manifest method %s", method)
		}
	}
}

func TestTenantRuntimeCoverageManifest(t *testing.T) {
	for _, group := range tenantCoverageManifest {
		t.Run(group.name, func(t *testing.T) {
			for _, test := range group.tests { // Executed, not just named in a document.
				t.Run(tenantCoverageTestName(test), test)
			}
		})
	}
}
