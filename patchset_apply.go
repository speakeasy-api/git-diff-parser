package git_diff_parser

import "fmt"

const patchsetOperationModify PatchsetOperation = "modify"

func applyPatchsetFile(tree map[string][]byte, file PatchsetFile) error {
	if file.Diff.IsBinary {
		return &UnsupportedPatchError{
			Operation: PatchsetOperationBinary,
			Path:      firstNonEmpty(file.Diff.ToFile, file.Diff.FromFile),
		}
	}

	op, sourcePath, targetPath, err := patchsetOperation(tree, file.Diff)
	if err != nil {
		return err
	}

	switch op {
	case PatchsetOperationCreate:
		if _, exists := tree[targetPath]; exists {
			return fmt.Errorf("cannot create existing file %q", targetPath)
		}
		content, err := applyPatchsetContent(nil, file)
		if err != nil {
			return err
		}
		tree[targetPath] = append([]byte(nil), content...)
		return nil
	case PatchsetOperationDelete:
		content, exists := tree[sourcePath]
		if !exists {
			return fmt.Errorf("cannot delete missing file %q", sourcePath)
		}
		if len(file.Diff.Hunks) > 0 {
			if _, err := applyPatchsetContent(content, file); err != nil {
				return err
			}
		}
		delete(tree, sourcePath)
		return nil
	case PatchsetOperationRename:
		content, exists := tree[sourcePath]
		if !exists {
			return fmt.Errorf("cannot rename missing file %q", sourcePath)
		}
		if targetPath != sourcePath {
			if _, exists := tree[targetPath]; exists {
				return fmt.Errorf("cannot rename %q to existing file %q", sourcePath, targetPath)
			}
		}
		applied, err := applyPatchsetContent(content, file)
		if err != nil {
			return err
		}
		delete(tree, sourcePath)
		tree[targetPath] = append([]byte(nil), applied...)
		return nil
	case PatchsetOperationCopy:
		content, exists := tree[sourcePath]
		if !exists {
			return fmt.Errorf("cannot copy missing file %q", sourcePath)
		}
		if _, exists := tree[targetPath]; exists {
			return fmt.Errorf("cannot copy to existing file %q", targetPath)
		}
		applied, err := applyPatchsetContent(content, file)
		if err != nil {
			return err
		}
		tree[targetPath] = append([]byte(nil), applied...)
		return nil
	case PatchsetOperationModeChange, patchsetOperationModify:
		content, exists := tree[targetPath]
		if !exists {
			return fmt.Errorf("cannot modify missing file %q", targetPath)
		}
		applied, err := applyPatchsetContent(content, file)
		if err != nil {
			return err
		}
		tree[targetPath] = append([]byte(nil), applied...)
		return nil
	default:
		return fmt.Errorf("unsupported patch operation")
	}
}

func patchsetOperation(tree map[string][]byte, fileDiff FileDiff) (PatchsetOperation, string, string, error) {
	sourcePath, targetPath := patchsetPaths(fileDiff)

	switch {
	case fileDiff.RenameFrom != "" || fileDiff.RenameTo != "":
		return PatchsetOperationRename, sourcePath, targetPath, nil
	case fileDiff.CopyFrom != "" || fileDiff.CopyTo != "":
		return PatchsetOperationCopy, sourcePath, targetPath, nil
	case fileDiff.Type == FileDiffTypeAdded:
		return PatchsetOperationCreate, "", targetPath, nil
	case fileDiff.Type == FileDiffTypeDeleted:
		return PatchsetOperationDelete, sourcePath, "", nil
	}

	if fileDiff.NewMode != "" && fileDiff.OldMode == "" {
		if _, exists := tree[targetPath]; exists {
			return "", "", "", fmt.Errorf("cannot create existing file %q", targetPath)
		}
		return PatchsetOperationCreate, "", targetPath, nil
	}
	if fileDiff.OldMode != "" || fileDiff.NewMode != "" {
		return PatchsetOperationModeChange, sourcePath, targetPath, nil
	}

	return patchsetOperationModify, sourcePath, targetPath, nil
}

func patchsetPaths(fileDiff FileDiff) (string, string) {
	sourcePath := firstNonEmpty(fileDiff.RenameFrom, fileDiff.CopyFrom, fileDiff.FromFile, fileDiff.ToFile)
	targetPath := firstNonEmpty(fileDiff.RenameTo, fileDiff.CopyTo, fileDiff.ToFile, fileDiff.FromFile)
	return sourcePath, targetPath
}

func applyPatchsetContent(pristine []byte, file PatchsetFile) ([]byte, error) {
	if len(file.Diff.Hunks) == 0 {
		return append([]byte(nil), pristine...), nil
	}

	hunks := make([]patchHunk, 0, len(file.Diff.Hunks))
	for _, hunk := range file.Diff.Hunks {
		hunks = append(hunks, patchHunkFromHunk(hunk))
	}

	outcome, err := NewPatchApply(ApplyOptions{Mode: ApplyModeApply}).newApplySession(pristine).apply(validatedPatch{hunks: hunks})
	if err != nil {
		return nil, err
	}
	if len(outcome.conflicts) > 0 {
		return nil, &ApplyError{DirectMisses: len(outcome.conflicts)}
	}

	result := renderApplyResult(pristine, outcome, ApplyOptions{Mode: ApplyModeApply})
	return append([]byte(nil), result.Content...), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
