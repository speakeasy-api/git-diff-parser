package git_diff_parser

import "fmt"

const patchsetOperationModify patchsetOperation = "modify"

func classifyPatchsetFile(file *patchsetFile) error {
	if err := validatePatchsetFileHeaders(&file.Diff); err != nil {
		return err
	}

	if file.Diff.IsBinary {
		file.Operation = patchsetOperationBinary
		file.SourcePath, file.TargetPath = patchsetPaths(&file.Diff)
		return nil
	}

	operation, sourcePath, targetPath := determinePatchsetOperation(&file.Diff)
	file.Operation = operation
	file.SourcePath = sourcePath
	file.TargetPath = targetPath
	return nil
}

func determinePatchsetOperation(fileDiff *fileDiff) (op patchsetOperation, sourcePath, targetPath string) {
	sourcePath, targetPath = patchsetPaths(fileDiff)

	switch {
	case fileDiff.RenameFrom != "" || fileDiff.RenameTo != "":
		return patchsetOperationRename, sourcePath, targetPath
	case fileDiff.CopyFrom != "" || fileDiff.CopyTo != "":
		return patchsetOperationCopy, sourcePath, targetPath
	case fileDiff.Type == fileDiffTypeAdded:
		return patchsetOperationCreate, "", targetPath
	case fileDiff.Type == fileDiffTypeDeleted:
		return patchsetOperationDelete, sourcePath, ""
	}

	if fileDiff.NewMode != "" && fileDiff.OldMode == "" {
		return patchsetOperationCreate, "", targetPath
	}
	if fileDiff.OldMode != "" || fileDiff.NewMode != "" {
		return patchsetOperationModeChange, sourcePath, targetPath
	}

	return patchsetOperationModify, sourcePath, targetPath
}

func validatePatchsetFileHeaders(fileDiff *fileDiff) error {
	hasCopy := fileDiff.CopyFrom != "" || fileDiff.CopyTo != ""
	hasRename := fileDiff.RenameFrom != "" || fileDiff.RenameTo != ""
	hasCreate := fileDiff.Type == fileDiffTypeAdded || (fileDiff.NewMode != "" && fileDiff.OldMode == "")
	hasDelete := fileDiff.Type == fileDiffTypeDeleted

	switch {
	case hasCopy && hasRename:
		return fmt.Errorf("invalid patch operation: copy and rename cannot be combined")
	case hasCreate && hasCopy:
		return fmt.Errorf("invalid patch operation: create and copy cannot be combined")
	case hasCreate && hasRename:
		return fmt.Errorf("invalid patch operation: create and rename cannot be combined")
	case hasDelete && hasCopy:
		return fmt.Errorf("invalid patch operation: delete and copy cannot be combined")
	case hasDelete && hasRename:
		return fmt.Errorf("invalid patch operation: delete and rename cannot be combined")
	}

	if len(fileDiff.Hunks) == 0 {
		return nil
	}

	sourcePath, targetPath := patchsetPaths(fileDiff)
	if !hasCreate && fileDiff.oldFileHeaderPath == "" {
		return fmt.Errorf("patch lacks old filename information for %q", sourcePath)
	}
	if !hasDelete && fileDiff.newFileHeaderPath == "" {
		return fmt.Errorf("patch lacks new filename information for %q", targetPath)
	}
	if fileDiff.oldFileHeaderPath != "" && sourcePath != "" && fileDiff.oldFileHeaderPath != sourcePath {
		return fmt.Errorf("inconsistent old filename: %q != %q", fileDiff.oldFileHeaderPath, sourcePath)
	}
	if fileDiff.newFileHeaderPath != "" && targetPath != "" && fileDiff.newFileHeaderPath != targetPath {
		return fmt.Errorf("inconsistent new filename: %q != %q", fileDiff.newFileHeaderPath, targetPath)
	}

	return nil
}

func patchsetPaths(fileDiff *fileDiff) (sourcePath, targetPath string) {
	sourcePath = firstNonEmpty(fileDiff.RenameFrom, fileDiff.CopyFrom, fileDiff.FromFile, fileDiff.ToFile)
	targetPath = firstNonEmpty(fileDiff.RenameTo, fileDiff.CopyTo, fileDiff.ToFile, fileDiff.FromFile)
	return sourcePath, targetPath
}

func applyPatchsetFiles(tree map[string][]byte, files []*patchsetFile) (map[string][]byte, error) {
	base := cloneTree(tree)
	current := cloneTree(tree)
	willDelete := patchsetDeletes(files)

	type pendingWrite struct {
		path    string
		content []byte
	}

	var deletes []string
	var writes []pendingWrite

	for _, file := range files {
		switch file.Operation {
		case patchsetOperationBinary:
			return nil, &unsupportedPatchError{
				Operation: patchsetOperationBinary,
				Path:      firstNonEmpty(file.TargetPath, file.SourcePath),
			}
		case patchsetOperationCreate:
			if err := ensureCanCreate(current, file.TargetPath, willDelete); err != nil {
				return nil, err
			}
			content, err := applyPatchsetContent(nil, file)
			if err != nil {
				return nil, err
			}
			current[file.TargetPath] = append([]byte(nil), content...)
			writes = append(writes, pendingWrite{path: file.TargetPath, content: content})
		case patchsetOperationDelete:
			content, exists := current[file.SourcePath]
			if !exists {
				return nil, fmt.Errorf("cannot delete missing file %q", file.SourcePath)
			}
			if len(file.Diff.Hunks) > 0 {
				if _, err := applyPatchsetContent(content, file); err != nil {
					return nil, err
				}
			}
			delete(current, file.SourcePath)
			deletes = append(deletes, file.SourcePath)
		case patchsetOperationRename:
			content, exists := base[file.SourcePath]
			if !exists {
				return nil, fmt.Errorf("cannot rename missing file %q", file.SourcePath)
			}
			if file.TargetPath != file.SourcePath {
				if err := ensureCanCreate(current, file.TargetPath, willDelete); err != nil {
					return nil, err
				}
			}
			applied, err := applyPatchsetContent(content, file)
			if err != nil {
				return nil, err
			}
			delete(current, file.SourcePath)
			current[file.TargetPath] = append([]byte(nil), applied...)
			deletes = append(deletes, file.SourcePath)
			writes = append(writes, pendingWrite{path: file.TargetPath, content: applied})
		case patchsetOperationCopy:
			content, exists := base[file.SourcePath]
			if !exists {
				return nil, fmt.Errorf("cannot copy missing file %q", file.SourcePath)
			}
			if err := ensureCanCreate(current, file.TargetPath, willDelete); err != nil {
				return nil, err
			}
			applied, err := applyPatchsetContent(content, file)
			if err != nil {
				return nil, err
			}
			current[file.TargetPath] = append([]byte(nil), applied...)
			writes = append(writes, pendingWrite{path: file.TargetPath, content: applied})
		case patchsetOperationModeChange, patchsetOperationModify:
			content, exists := current[file.TargetPath]
			if !exists {
				return nil, fmt.Errorf("cannot modify missing file %q", file.TargetPath)
			}
			applied, err := applyPatchsetContent(content, file)
			if err != nil {
				return nil, err
			}
			current[file.TargetPath] = append([]byte(nil), applied...)
			writes = append(writes, pendingWrite{path: file.TargetPath, content: applied})
		default:
			return nil, fmt.Errorf("unsupported patch operation")
		}
	}

	out := cloneTree(tree)
	for _, path := range deletes {
		delete(out, path)
	}
	for _, write := range writes {
		out[write.path] = append([]byte(nil), write.content...)
	}
	return out, nil
}

func patchsetDeletes(files []*patchsetFile) map[string]bool {
	deletes := make(map[string]bool)
	for _, file := range files {
		switch file.Operation {
		case patchsetOperationDelete, patchsetOperationRename:
			deletes[file.SourcePath] = true
		}
	}
	return deletes
}

func ensureCanCreate(tree map[string][]byte, path string, willDelete map[string]bool) error {
	if _, exists := tree[path]; exists && !willDelete[path] {
		return fmt.Errorf("cannot create existing file %q", path)
	}
	return nil
}

func applyPatchsetContent(pristine []byte, file *patchsetFile) ([]byte, error) {
	if len(file.Diff.Hunks) == 0 {
		return append([]byte(nil), pristine...), nil
	}

	hunks := make([]patchHunk, 0, len(file.Diff.Hunks))
	for i := range file.Diff.Hunks {
		hunks = append(hunks, patchHunkFromHunk(&file.Diff.Hunks[i]))
	}

	result, err := newPatchApply(applyOptions{Mode: applyModeApply}).applyValidatedPatch(pristine, validatedPatch{
		rejectHead: formatRejectHeader(&file.Diff),
		hunks:      hunks,
	})
	if err != nil {
		return nil, err
	}

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
