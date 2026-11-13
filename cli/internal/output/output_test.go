package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestFailureKeepsRetrySemanticsWithoutInternalFieldsInHumanOutput(t *testing.T) {
	detail := Error{Code: "REQUEST_UNKNOWN", Category: "transport", Message: "操作结果尚未确认。", RetryAction: "same_request"}
	var human bytes.Buffer
	if (Printer{Err: &human}).Failure(detail, 5) != 5 {
		t.Fatal("failure exit changed")
	}
	for _, internal := range []string{detail.Code, detail.Category, "retry_action", detail.RetryAction} {
		if strings.Contains(human.String(), internal) {
			t.Fatalf("internal field shown to user: %s", internal)
		}
	}
	if !strings.Contains(human.String(), "原请求文件和请求引用") {
		t.Fatal("safe retry instruction missing")
	}
	var machine bytes.Buffer
	(Printer{JSON: true, Out: &machine}).Failure(detail, 5)
	var result Envelope
	if err := json.Unmarshal(machine.Bytes(), &result); err != nil || result.Error == nil || *result.Error != detail {
		t.Fatal("structured error lost diagnostic or retry semantics")
	}
}
