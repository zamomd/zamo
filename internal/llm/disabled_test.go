package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/zamomd/zamo/internal/knowledge"
)

func TestDisabledChatIsUnavailable(t *testing.T) {
	_, err := Disabled{}.Chat(t.Context(), Request{})

	assert.ErrorIs(t, err, knowledge.ErrLLMUnavailable)
}

func TestDisabledPingIsUnavailable(t *testing.T) {
	assert.ErrorIs(t, Disabled{}.Ping(t.Context()), knowledge.ErrLLMUnavailable)
}
