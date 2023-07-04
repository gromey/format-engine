package engine

type Default[T any] struct{}

func (d Default[T]) Skip(string) bool {
	return false
}

func (d Default[T]) Parse(string, *T) (bool, error) {
	return false, nil
}

func (d Default[T]) f() {}
