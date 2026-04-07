// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package apply

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/envsync/envsync/internal/envfile"
	"github.com/envsync/envsync/internal/fsutil"
	"github.com/envsync/envsync/internal/revision"
	"github.com/envsync/envsync/internal/store"
)

type Policy string

const (
	PolicyInteractive Policy = "interactive"
	PolicyOverwrite   Policy = "overwrite"
	PolicyKeepLocal   Policy = "keep-local"
	PolicyThreeWay    Policy = "three-way"
	PolicyFail        Policy = "fail"
)

var (
	ErrInteractiveRequired = errors.New("interactive input required")
	ErrConflictRefused     = errors.New("conflict policy refused to apply incoming data")
	ErrMergeBaseMissing    = errors.New("three-way merge requires a base revision")
	ErrUnknownAncestry     = errors.New("three-way merge base revision is unknown locally")
	ErrMergeConflict       = errors.New("three-way merge produced unresolved conflicts")
)

type ConflictResolutionAction string

const (
	ConflictUseLocal   ConflictResolutionAction = "use-local"
	ConflictUseRemote  ConflictResolutionAction = "use-remote"
	ConflictUseCustom  ConflictResolutionAction = "use-custom"
	ConflictKeepAbsent ConflictResolutionAction = "keep-absent"
)

type ConflictResolution struct {
	Key    string
	Action ConflictResolutionAction
	Value  string
}

type Options struct {
	ProjectID        string
	TargetFile       string
	IncomingFile     string
	IncomingData     []byte
	BaseRevisionID   string
	NewRevisionID    string
	Policy           Policy
	Interactive      bool
	BackupEnabled    bool
	BackupKey        [32]byte
	MaxVersions      int
	OnDiff           func(diff *envfile.DiffResult)
	ConfirmApply     func(diff *envfile.DiffResult) bool
	ResolveConflicts func(conflicts []envfile.Conflict) ([]ConflictResolution, bool)
}

type Result struct {
	Applied                  bool
	Changed                  bool
	BackupCreated            bool
	InteractiveRequired      bool
	ManualInterventionNeeded bool
	ConflictPolicyApplied    string
	Diff                     *envfile.DiffResult
	Summary                  string
	VariableCount            int
	FinalRevisionID          string
}

func Apply(opts Options) (*Result, error) {
	if opts.TargetFile == "" {
		opts.TargetFile = ".env"
	}
	if opts.Policy == "" {
		opts.Policy = PolicyInteractive
	}
	if opts.MaxVersions <= 0 {
		opts.MaxVersions = 10
	}

	incomingEnv, err := envfile.Parse(string(opts.IncomingData))
	if err != nil {
		return nil, fmt.Errorf("parsing incoming env data: %w", err)
	}

	result := &Result{
		ConflictPolicyApplied: string(opts.Policy),
		VariableCount:         incomingEnv.VariableCount(),
	}

	localData, err := os.ReadFile(opts.TargetFile)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading target file: %w", err)
	}

	var (
		revStore     *revision.Store
		localCurrent *revision.Metadata
	)
	if opts.ProjectID != "" {
		revStore, err = revision.New()
		if err != nil {
			return nil, fmt.Errorf("creating revision store: %w", err)
		}
		if err == nil && !os.IsNotExist(err) && len(localData) > 0 {
			localCurrent, err = revStore.SyncCurrent(opts.ProjectID, localData, opts.BackupKey)
			if err != nil {
				return nil, fmt.Errorf("syncing local revision state: %w", err)
			}
		}
	}

	if os.IsNotExist(err) || len(localData) == 0 {
		if err := fsutil.AtomicWriteFile(opts.TargetFile, opts.IncomingData, 0600); err != nil {
			return nil, fmt.Errorf("writing %s: %w", opts.TargetFile, err)
		}
		result.Applied = true
		result.Changed = true
		result.Summary = "created local env file"
		if revStore != nil {
			finalRevisionID, revErr := persistRevisionState(revStore, opts, opts.IncomingData, nil)
			if revErr != nil {
				return nil, revErr
			}
			result.FinalRevisionID = finalRevisionID
		}
		return result, nil
	}

	localEnv, err := envfile.Parse(string(localData))
	if err != nil {
		return nil, fmt.Errorf("parsing local env file: %w", err)
	}

	diff := envfile.Diff(localEnv, incomingEnv)
	result.Diff = diff
	result.Summary = diff.Summary()
	if opts.OnDiff != nil {
		opts.OnDiff(diff)
	}

	if !diff.HasChanges() {
		if revStore != nil {
			finalRevisionID, revErr := persistRevisionState(revStore, opts, opts.IncomingData, localCurrent)
			if revErr != nil {
				return nil, revErr
			}
			result.FinalRevisionID = finalRevisionID
		}
		return result, nil
	}

	var rendered []byte
	switch opts.Policy {
	case PolicyInteractive:
		if !opts.Interactive || opts.ConfirmApply == nil {
			result.InteractiveRequired = true
			result.ManualInterventionNeeded = true
			return result, ErrInteractiveRequired
		}
		if !opts.ConfirmApply(diff) {
			result.ManualInterventionNeeded = true
			return result, nil
		}
		rendered = opts.IncomingData
	case PolicyOverwrite:
		rendered = opts.IncomingData
	case PolicyKeepLocal:
		result.ManualInterventionNeeded = true
		return result, ErrConflictRefused
	case PolicyFail:
		result.ManualInterventionNeeded = true
		return result, ErrConflictRefused
	case PolicyThreeWay:
		merged, mergeErr := applyThreeWay(localEnv, incomingEnv, opts, revStore)
		if mergeErr != nil {
			if errors.Is(mergeErr, ErrInteractiveRequired) {
				result.InteractiveRequired = true
			}
			result.ManualInterventionNeeded = true
			return result, mergeErr
		}
		rendered = []byte(envfile.Write(merged))
	default:
		return nil, fmt.Errorf("unsupported apply policy %q", opts.Policy)
	}

	if bytes.Equal(localData, rendered) {
		result.Summary = "no changes"
		return result, nil
	}

	if opts.BackupEnabled && opts.ProjectID != "" {
		vStore, err := store.New(opts.MaxVersions)
		if err != nil {
			return nil, fmt.Errorf("creating backup store: %w", err)
		}
		if _, err := vStore.Append(opts.ProjectID, localData, opts.BackupKey); err != nil {
			return nil, fmt.Errorf("creating pre-apply backup: %w", err)
		}
		result.BackupCreated = true
	}

	if err := fsutil.AtomicWriteFile(opts.TargetFile, rendered, 0600); err != nil {
		return nil, fmt.Errorf("writing %s: %w", opts.TargetFile, err)
	}

	if revStore != nil {
		finalRevisionID, revErr := persistRevisionState(revStore, opts, rendered, localCurrent)
		if revErr != nil {
			return nil, revErr
		}
		result.FinalRevisionID = finalRevisionID
	}

	result.Applied = true
	result.Changed = true
	return result, nil
}

func applyThreeWay(localEnv, incomingEnv *envfile.EnvFile, opts Options, revStore *revision.Store) (*envfile.EnvFile, error) {
	if opts.ProjectID == "" || revStore == nil {
		return nil, ErrMergeBaseMissing
	}
	if opts.BaseRevisionID == "" {
		return nil, ErrMergeBaseMissing
	}
	baseData, err := revStore.LoadRevision(opts.ProjectID, opts.BaseRevisionID, opts.BackupKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnknownAncestry, err)
	}

	baseEnv, err := envfile.Parse(string(baseData))
	if err != nil {
		return nil, fmt.Errorf("parsing merge base: %w", err)
	}

	mergeResult := envfile.ThreeWayMerge(baseEnv, localEnv, incomingEnv)
	if !mergeResult.HasConflicts() {
		return mergeResult.Merged, nil
	}

	if !opts.Interactive || opts.ResolveConflicts == nil {
		return nil, ErrMergeConflict
	}

	resolutions, ok := opts.ResolveConflicts(mergeResult.Conflicts)
	if !ok {
		return nil, ErrInteractiveRequired
	}
	resolutionMap := make(map[string]ConflictResolution, len(resolutions))
	for _, resolution := range resolutions {
		resolutionMap[resolution.Key] = resolution
	}

	for _, conflict := range mergeResult.Conflicts {
		resolution, ok := resolutionMap[conflict.Key]
		if !ok {
			return nil, ErrMergeConflict
		}

		switch resolution.Action {
		case ConflictUseLocal:
			if conflict.OurValue == "(deleted)" {
				mergeResult.Merged.Delete(conflict.Key)
			} else {
				mergeResult.Merged.Set(conflict.Key, conflict.OurValue)
			}
		case ConflictUseRemote:
			if conflict.TheirValue == "(deleted)" {
				mergeResult.Merged.Delete(conflict.Key)
			} else {
				mergeResult.Merged.Set(conflict.Key, conflict.TheirValue)
			}
		case ConflictUseCustom:
			mergeResult.Merged.Set(conflict.Key, resolution.Value)
		case ConflictKeepAbsent:
			mergeResult.Merged.Delete(conflict.Key)
		default:
			return nil, fmt.Errorf("unsupported conflict resolution %q for %s", resolution.Action, conflict.Key)
		}
	}

	return mergeResult.Merged, nil
}

func persistRevisionState(revStore *revision.Store, opts Options, rendered []byte, localCurrent *revision.Metadata) (string, error) {
	if revStore == nil || opts.ProjectID == "" {
		return "", nil
	}

	parentID := opts.BaseRevisionID
	if parentID == "" && localCurrent != nil {
		parentID = localCurrent.ID
	}

	revisionID := opts.NewRevisionID
	if revisionID == "" || !bytes.Equal(rendered, opts.IncomingData) {
		revisionID = revision.RevisionID(rendered)
	}

	if _, err := revStore.SaveRevision(opts.ProjectID, revisionID, parentID, rendered, opts.BackupKey); err != nil {
		return "", fmt.Errorf("saving revision metadata: %w", err)
	}
	if err := revStore.MarkCurrent(opts.ProjectID, revisionID, rendered); err != nil {
		return "", fmt.Errorf("updating current revision state: %w", err)
	}
	return revisionID, nil
}
