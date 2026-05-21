package git_diff_parser

import "fmt"

type PatchOperationType string

const (
	PatchOperationTypeModify     PatchOperationType = "modify"
	PatchOperationTypeCreate     PatchOperationType = "create"
	PatchOperationTypeDelete     PatchOperationType = "delete"
	PatchOperationTypeRename     PatchOperationType = "rename"
	PatchOperationTypeCopy       PatchOperationType = "copy"
	PatchOperationTypeModeChange PatchOperationType = "mode_change"
	PatchOperationTypeBinary     PatchOperationType = "binary"
)

// PatchOperation describes one file-level operation in a git patchset.
//
// Values returned by ParsePatchOperations can be passed to ApplyPatchOperations.
type PatchOperation struct {
	Type       PatchOperationType
	SourcePath string
	TargetPath string
	OldMode    string
	NewMode    string
	IndexMode  string
	IsBinary   bool
	Patch      []byte

	file *patchsetFile
}

// MutatesFileSet reports whether this operation adds, removes, or moves a file.
func (op *PatchOperation) MutatesFileSet() bool {
	switch op.Type {
	case PatchOperationTypeCreate, PatchOperationTypeDelete, PatchOperationTypeRename, PatchOperationTypeCopy:
		return true
	default:
		return false
	}
}

// ParsePatchOperations parses patchData into ordered file-level operations.
func ParsePatchOperations(patchData []byte) ([]PatchOperation, error) {
	patchset, errs := parsePatchset(patchData)
	if len(errs) > 0 {
		return nil, fmt.Errorf("unsupported patch syntax: %w", errs[0])
	}

	operations := make([]PatchOperation, 0, len(patchset.Files))
	for i := range patchset.Files {
		operation, err := patchOperationFromFile(&patchset.Files[i])
		if err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}

	return operations, nil
}

// ApplyPatchOperations applies ordered patch operations to a copy of tree.
func ApplyPatchOperations(tree map[string][]byte, operations []PatchOperation) (map[string][]byte, error) {
	files, err := patchsetFilesFromOperations(operations)
	if err != nil {
		return nil, err
	}
	return applyPatchsetFiles(tree, files)
}

func patchsetFilesFromOperations(operations []PatchOperation) ([]*patchsetFile, error) {
	files := make([]*patchsetFile, 0, len(operations))
	for i := range operations {
		file, err := operations[i].patchsetFile()
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}

	return files, nil
}

func patchOperationFromFile(file *patchsetFile) (PatchOperation, error) {
	return PatchOperation{
		Type:       publicPatchOperationType(file.Operation),
		SourcePath: file.SourcePath,
		TargetPath: file.TargetPath,
		OldMode:    file.Diff.OldMode,
		NewMode:    file.Diff.NewMode,
		IndexMode:  file.Diff.IndexMode,
		IsBinary:   file.Diff.IsBinary,
		Patch:      append([]byte(nil), file.Patch...),
		file:       file,
	}, nil
}

func publicPatchOperationType(op patchsetOperation) PatchOperationType {
	switch op {
	case patchsetOperationCreate:
		return PatchOperationTypeCreate
	case patchsetOperationDelete:
		return PatchOperationTypeDelete
	case patchsetOperationRename:
		return PatchOperationTypeRename
	case patchsetOperationCopy:
		return PatchOperationTypeCopy
	case patchsetOperationModeChange:
		return PatchOperationTypeModeChange
	case patchsetOperationBinary:
		return PatchOperationTypeBinary
	default:
		return PatchOperationTypeModify
	}
}

func (op *PatchOperation) patchsetFile() (*patchsetFile, error) {
	if op.file != nil {
		return op.file, nil
	}
	if len(op.Patch) > 0 {
		patchset, errs := parsePatchset(op.Patch)
		if len(errs) > 0 {
			return nil, fmt.Errorf("unsupported patch syntax: %w", errs[0])
		}
		if len(patchset.Files) != 1 {
			return nil, fmt.Errorf("patch operation contains %d file diffs, expected 1", len(patchset.Files))
		}
		return &patchset.Files[0], nil
	}

	return nil, fmt.Errorf("patch operation has no patch data")
}
