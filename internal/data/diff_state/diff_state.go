package diff_state

type DiffState int

const (
	Added DiffState = iota
	Deleted
	Modified
	Equal
	Unknown
)
