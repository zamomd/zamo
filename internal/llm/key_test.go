package llm

import "testing"

func TestStaticKey(t *testing.T) {
	key, err := StaticKey("k")()
	if err != nil {
		t.Fatal(err)
	}
	if key != "k" {
		t.Errorf("key = %q, want k", key)
	}
}
