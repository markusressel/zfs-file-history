package diff_state

// Counts is the number of entries in each state, e.g. of the entries of a folder compared to a snapshot.
type Counts struct {
	Added    int
	Deleted  int
	Modified int
	Equal    int
	// Unknown are the entries whose state is not known (yet)
	Unknown int
}

// Add counts an entry with the given state.
func (c *Counts) Add(state DiffState) {
	switch state {
	case Added:
		c.Added++
	case Deleted:
		c.Deleted++
	case Modified:
		c.Modified++
	case Equal:
		c.Equal++
	default:
		c.Unknown++
	}
}
