package git_diff_parser

type missRenderer interface {
	appendMiss(out, source, desired []fileLine) []fileLine
	result(content []byte, misses int) (ApplyResult, error)
}

func (p *PatchApply) missRenderer() missRenderer {
	if p.options.Mode == ApplyModeMerge {
		return mergeMissRenderer{applier: p}
	}
	return directMissRenderer{}
}

type directMissRenderer struct{}

func (directMissRenderer) appendMiss(out, source, _ []fileLine) []fileLine {
	return appendSourceLines(out, source...)
}

func (directMissRenderer) result(content []byte, misses int) (ApplyResult, error) {
	result := ApplyResult{Content: content}
	if misses == 0 {
		return result, nil
	}
	result.DirectMisses = misses
	return result, &ApplyError{DirectMisses: misses}
}

type mergeMissRenderer struct {
	applier *PatchApply
}

func (r mergeMissRenderer) appendMiss(out, source, desired []fileLine) []fileLine {
	return r.applier.appendConflict(out, source, desired)
}

func (mergeMissRenderer) result(content []byte, misses int) (ApplyResult, error) {
	result := ApplyResult{Content: content}
	if misses == 0 {
		return result, nil
	}
	result.MergeConflicts = misses
	return result, &ApplyError{
		MergeConflicts:   misses,
		ConflictingHunks: misses,
	}
}
