package tools_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zamomd/zamo/internal/knowledge"
	"github.com/zamomd/zamo/internal/tools"
)

var aiStamp = tools.Stamp{
	Origin: "test",
	Meta: func(title string) knowledge.WriteMeta {
		return knowledge.WriteMeta{Author: knowledge.AuthorAI, Source: knowledge.SourceMCP, Title: title}
	},
}

func TestWriteAppliesTheBatchAwaitingReview(t *testing.T) {
	svc := openService(t)
	write := tools.Write(svc, aiStamp)

	out := call[tools.WriteOutput](t, write, `{"title":"notes on go","ops":[
		{"op":"add","temp_id":"t1","content":"Go contexts"},
		{"op":"add","temp_id":"t2","parent":"t1","content":"deadlines propagate","rationale":"core idea"}
	]}`)

	assert.NotEmpty(t, out.GroupID)
	require.Contains(t, out.IDs, "t2")
	pending, err := svc.Unreviewed(t.Context())
	require.NoError(t, err)
	require.Len(t, pending.Blocks, 2, "an AI batch awaits review")
	history, err := svc.BlockHistory(t.Context(), knowledge.ID(out.IDs["t2"]))
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "test", history[0].Origin, "the caller's origin is stamped on every op")
	assert.Equal(t, "core idea", history[0].Rationale)
}

func TestWriteRefusesAnOriginFromTheModel(t *testing.T) {
	write := tools.Write(openService(t), aiStamp)

	_, err := write.Call(t.Context(), jsontext.Value(
		`{"title":"t","ops":[{"op":"add","temp_id":"t1","content":"x","origin":"owner"}]}`))

	assert.ErrorIs(t, err, tools.ErrBadArguments)
}

func TestWriteRefusesTooManyOperations(t *testing.T) {
	svc := openService(t)
	write := tools.Write(svc, aiStamp)
	ops := []string{`{"op":"add","temp_id":"root","content":"Big"}`}
	for i := range tools.MaxOps {
		ops = append(ops, fmt.Sprintf(`{"op":"add","temp_id":"c%d","parent":"root","content":"x"}`, i))
	}

	_, err := write.Call(t.Context(), jsontext.Value(`{"title":"big","ops":[`+strings.Join(ops, ",")+`]}`))

	require.ErrorIs(t, err, knowledge.ErrInvalidOps)
	require.ErrorContains(t, err, fmt.Sprint(tools.MaxOps), "the refusal names the cap")
	topics, mapErr := svc.TopicMap(t.Context())
	require.NoError(t, mapErr)
	assert.Empty(t, topics, "nothing was written")
}

func TestWriteRefusesATreeDeeperThanTheCap(t *testing.T) {
	write := tools.Write(openService(t), aiStamp)
	ops := []string{`{"op":"add","temp_id":"d0","content":"Deep"}`}
	for i := 1; i <= tools.MaxDepth; i++ {
		ops = append(ops, fmt.Sprintf(`{"op":"add","temp_id":"d%d","parent":"d%d","content":"x"}`, i, i-1))
	}

	_, err := write.Call(t.Context(), jsontext.Value(`{"title":"deep","ops":[`+strings.Join(ops, ",")+`]}`))

	require.ErrorIs(t, err, knowledge.ErrInvalidOps)
	assert.ErrorContains(t, err, fmt.Sprint(tools.MaxDepth))
}

func TestWriteAllowsATreeAtTheDepthCap(t *testing.T) {
	write := tools.Write(openService(t), aiStamp)
	ops := []string{`{"op":"add","temp_id":"d1","content":"Deep"}`}
	for i := 2; i <= tools.MaxDepth; i++ {
		ops = append(ops, fmt.Sprintf(`{"op":"add","temp_id":"d%d","parent":"d%d","content":"x"}`, i, i-1))
	}

	_, err := write.Call(t.Context(), jsontext.Value(`{"title":"deep","ops":[`+strings.Join(ops, ",")+`]}`))

	assert.NoError(t, err)
}

func TestWriteExplainsAChildUnderADocument(t *testing.T) {
	svc := openService(t)
	doc, err := svc.CreateTopic(t.Context(), "Essay", knowledge.ModeDoc)
	require.NoError(t, err)
	write := tools.Write(svc, aiStamp)

	_, err = write.Call(t.Context(), jsontext.Value(
		`{"title":"t","ops":[{"op":"add","temp_id":"t1","parent":"`+string(doc)+`","content":"x"}]}`))

	require.ErrorIs(t, err, knowledge.ErrDocumentHasChildren)
	assert.ErrorContains(t, err, "edit its root content instead")
}

func TestWritePlacesABlockAfterItsAnchor(t *testing.T) {
	svc := openService(t)
	ids := seedTopic(t, svc, "Ordered", "first", "last")
	write := tools.Write(svc, aiStamp)

	call[tools.WriteOutput](t, write, `{"title":"insert","ops":[
		{"op":"add","temp_id":"t1","parent":"`+string(ids[0])+`","after":"`+string(ids[1])+`","content":"middle"}
	]}`)

	blocks, err := svc.GetSubtree(t.Context(), ids[0])
	require.NoError(t, err)
	var order []string
	for _, b := range blocks[1:] {
		order = append(order, b.Content)
	}
	assert.Equal(t, []string{"first", "middle", "last"}, order)
}
