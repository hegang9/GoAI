package message

import "testing"

func TestTargetValidation(t *testing.T) {
	t.Parallel()

	if err := TopicTarget().Validate(); err != nil {
		t.Fatalf("TopicTarget().Validate() error = %v", err)
	}

	groupTarget, err := ConsumerGroupTarget("analytics")
	if err != nil {
		t.Fatalf("ConsumerGroupTarget() error = %v", err)
	}
	if groupTarget.Kind != TargetConsumerGroup || groupTarget.ConsumerGroup != "analytics" {
		t.Fatalf("ConsumerGroupTarget() = %+v", groupTarget)
	}

	invalid := []Target{
		{},
		{Kind: TargetTopic, ConsumerGroup: "unexpected"},
		{Kind: TargetConsumerGroup},
		{Kind: TargetKind(99)},
	}
	for _, target := range invalid {
		if err := target.Validate(); err == nil {
			t.Fatalf("Target.Validate() error = nil for %+v", target)
		}
	}
}
