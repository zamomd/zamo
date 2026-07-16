package llm

// KeySource yields the current API key. Implementations are called on every
// request rather than once at startup: a key file managed by sops-nix lives in
// a generation directory that is recreated and pruned on each activation, so a
// key read once goes stale and its resolved path stops existing.
type KeySource func() (string, error)

// StaticKey is a KeySource for a fixed key: local providers that need none,
// and tests.
func StaticKey(key string) KeySource {
	return func() (string, error) { return key, nil }
}
