package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRewardsSurviveCloneAndJSON(t *testing.T) {
	for name, rewards := range map[string]map[string][]int{
		"unknown":     nil,
		"known empty": {},
		"paid":        {"critical": {50000, 70000}},
	} {
		t.Run(name, func(t *testing.T) {
			original := Program{Rewards: rewards}
			clone := original.Clone()
			raw, err := json.Marshal(clone)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Program
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(original.Rewards, clone.Rewards) || !reflect.DeepEqual(original.Rewards, decoded.Rewards) {
				t.Fatalf("reward knowledge lost: original=%#v clone=%#v decoded=%#v", original.Rewards, clone.Rewards, decoded.Rewards)
			}
			if len(clone.Rewards) > 0 {
				clone.Rewards["critical"][1] = 1
				delete(clone.Rewards, "critical")
				if original.Rewards["critical"][1] != 70000 {
					t.Fatal("clone mutated original rewards")
				}
			}
		})
	}
}
