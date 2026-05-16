package util

// SelectionInfo stores remembered selection metadata for a scoped key.
type SelectionInfo[T any] struct {
	Index int
	Entry *T
}

// SelectionMemory stores and retrieves remembered selections by key.
type SelectionMemory[T any] struct {
	selections map[string]SelectionInfo[T]
}

func NewSelectionMemory[T any]() *SelectionMemory[T] {
	return &SelectionMemory[T]{
		selections: map[string]SelectionInfo[T]{},
	}
}

func (s *SelectionMemory[T]) Remember(key string, index int, entry *T) {
	s.selections[key] = SelectionInfo[T]{
		Index: index,
		Entry: entry,
	}
}

func (s *SelectionMemory[T]) Get(key string) *SelectionInfo[T] {
	selectionInfo, ok := s.selections[key]
	if !ok {
		return nil
	}
	return &selectionInfo
}
