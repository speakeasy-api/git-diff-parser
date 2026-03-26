package git_diff_parser

// applyMode controls how the apply engine treats hunks that cannot be placed
// directly into the target content.
type applyMode int

const (
	// applyModeApply keeps the output neutral when a hunk cannot be applied.
	applyModeApply applyMode = iota
	// applyModeMerge renders conflict markers into the output for misses.
	applyModeMerge
)

// conflictLabels controls the labels rendered into conflict markers.
// The zero value renders neutral markers without any labels.
type conflictLabels struct {
	Current  string
	Incoming string
}

// applyOptions configures the apply engine.
type applyOptions struct {
	Mode             applyMode
	ConflictLabels   conflictLabels
	IgnoreWhitespace bool
	Reverse          bool
	UnidiffZero      bool
	Recount          bool
	InaccurateEOF    bool
}

func defaultApplyOptions() applyOptions {
	return applyOptions{
		Mode: applyModeMerge,
		ConflictLabels: conflictLabels{
			Current:  "Current",
			Incoming: "Incoming patch",
		},
	}
}

// patchApply holds apply-time configuration and mirrors Git's stateful apply design.
type patchApply struct {
	options applyOptions
}

func newPatchApply(options applyOptions) *patchApply {
	return &patchApply{options: normalizeApplyOptions(options)}
}

func (o applyOptions) normalize() applyOptions {
	if o.Mode != applyModeMerge {
		o.Mode = applyModeApply
	}
	if o.Mode == applyModeMerge {
		defaults := defaultApplyOptions()
		if o.ConflictLabels.Current == "" {
			o.ConflictLabels.Current = defaults.ConflictLabels.Current
		}
		if o.ConflictLabels.Incoming == "" {
			o.ConflictLabels.Incoming = defaults.ConflictLabels.Incoming
		}
	}
	return o
}

func normalizeApplyOptions(options applyOptions) applyOptions {
	return options.normalize()
}
