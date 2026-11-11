package muster

import "testing"

func TestMCPCallRejectsInvalidInnerParameters(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params string
	}{
		{"string arguments", `{"name":"scripted","arguments":"wrong"}`},
		{"array arguments", `{"name":"scripted","arguments":[]}`},
		{"boolean arguments", `{"name":"scripted","arguments":true}`},
		{"number arguments", `{"name":"scripted","arguments":42}`},
		{"missing name", `{"arguments":{}}`},
		{"empty name", `{"name":"","arguments":{}}`},
		{"null name", `{"name":null,"arguments":{}}`},
		{"number name", `{"name":42,"arguments":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{
				cfg: Config{Bearer: "test-bearer", Tools: map[string][]ToolResponse{
					"scripted": {{Result: "first"}, {Result: "second"}},
					"":         {{Result: "unnamed"}},
				}},
				calls: map[string]int{},
			}
			reply := postMetaCallNull(t, s, tc.params)
			if reply.Error == nil || reply.Error.Code != -32602 {
				t.Fatalf("expected invalid parameters, got %+v", reply)
			}
			if len(reply.Result) != 0 || len(s.calls) != 0 {
				t.Fatalf("invalid call returned a result or advanced script: result=%s calls=%v", reply.Result, s.calls)
			}
			_, inner := unwrapReplyNull(t, postMetaCallNull(t, s, `{"name":"scripted","arguments":{}}`))
			if inner.IsError || len(inner.Content) != 1 || inner.Content[0].Text != "first" {
				t.Fatalf("valid call did not receive first answer: %+v", inner)
			}
		})
	}
}

func TestMCPCallAcceptsZeroArguments(t *testing.T) {
	for _, params := range []string{
		`{"name":"scripted"}`,
		`{"name":"scripted","arguments":null}`,
		`{"name":"scripted","arguments":{}}`,
	} {
		t.Run(params, func(t *testing.T) {
			s := &Server{
				cfg: Config{Bearer: "test-bearer", Tools: map[string][]ToolResponse{
					"scripted": {{Result: "first"}, {Result: "second"}},
				}},
				calls: map[string]int{},
			}
			for _, want := range []string{"first", "second"} {
				outer, inner := unwrapReplyNull(t, postMetaCallNull(t, s, params))
				if outer.IsError || inner.IsError || len(inner.Content) != 1 || inner.Content[0].Text != want {
					t.Fatalf("expected %s, got outer=%+v inner=%+v", want, outer, inner)
				}
			}
		})
	}
}
