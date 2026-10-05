package wailsbind

type emitted struct {
	name string
	data []any
}

type fakeEmitter struct {
	events []emitted
}

func (f *fakeEmitter) Emit(name string, data ...any) bool {
	f.events = append(f.events, emitted{name: name, data: data})
	return true
}
