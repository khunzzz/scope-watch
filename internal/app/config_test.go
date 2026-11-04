package app

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestChatIDList(t *testing.T) {
	for _, tc := range []struct {
		input   string
		want    []string
		invalid bool
	}{
		{"", []string{}, false},
		{"123456789,-1009876543210", []string{"123456789", "-1009876543210"}, false},
		{" 123; -100\n123\t+123 ", []string{"123", "-100"}, false},
		{"0", nil, true},
		{"123,nope", nil, true},
		{"9223372036854775808", nil, true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseChatIDs(tc.input)
			if (err != nil) != tc.invalid || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got=%v err=%v want=%v invalid=%t", got, err, tc.want, tc.invalid)
			}
		})
	}
	var many []string
	for i := 1; i <= 1000; i++ {
		many = append(many, fmt.Sprint(i))
	}
	got, err := parseChatIDs(strings.Join(many, ","))
	if err != nil || len(got) != 1000 {
		t.Fatalf("list was limited: count=%d err=%v", len(got), err)
	}
}
