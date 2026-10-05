package project

// ptr is the test helper for optional fields.
func ptr[T any](v T) *T { return &v }
