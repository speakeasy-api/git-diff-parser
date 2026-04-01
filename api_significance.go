package git_diff_parser

type (
	ContentChange     = contentChange
	ContentChangeType = contentChangeType
	FileDiff          = fileDiff
	FileDiffType      = fileDiffType
)

const (
	ContentChangeTypeAdd    = contentChangeTypeAdd
	ContentChangeTypeDelete = contentChangeTypeDelete
	ContentChangeTypeModify = contentChangeTypeModify
	ContentChangeTypeNOOP   = contentChangeTypeNOOP

	FileDiffTypeAdded    = fileDiffTypeAdded
	FileDiffTypeDeleted  = fileDiffTypeDeleted
	FileDiffTypeModified = fileDiffTypeModified
)

func SignificantChange(diff string, isSignificant func(*FileDiff, *ContentChange) (bool, string)) (significant bool, msg string, err error) {
	return significantChange(diff, func(fileDiff *fileDiff, change *contentChange) (bool, string) {
		return isSignificant(fileDiff, change)
	})
}
