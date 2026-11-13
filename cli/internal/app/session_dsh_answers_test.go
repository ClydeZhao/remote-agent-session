package app

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
)

func TestDSHMultiSelectPreservesExplicitSelectionsAndFreeText(t *testing.T) {
	var question relay.DSHQuestion
	if err := json.Unmarshal([]byte(`{"id":"animals","question":"选动物","multiSelect":true,"options":[{"label":"猫"},{"label":"狗"}]}`), &question); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		value    string
		selected []string
		custom   bool
	}{
		{`["猫","狗"]`, []string{"猫", "狗"}, false},
		{"猫", []string{"猫"}, false},
		{"A,B", []string{}, true},
		{`["猫","未知"]`, []string{}, true},
		{`["猫","猫"]`, []string{}, true},
		{"我想稍后决定", []string{}, true},
		{"[]", []string{}, true},
	} {
		t.Run(test.value, func(t *testing.T) {
			answer := dshQuestionAnswer(question, test.value)
			if !reflect.DeepEqual(answer["selected"], test.selected) {
				t.Fatal(answer)
			}
			value, ok := answer["custom"]
			if ok != test.custom || (ok && value != test.value) {
				t.Fatal("free text changed", answer)
			}
		})
	}
	question.MultiSelect = false
	answer := dshQuestionAnswer(question, `["猫","狗"]`)
	if answer["custom"] != `["猫","狗"]` {
		t.Fatal("single-select became multi-select", answer)
	}
}
