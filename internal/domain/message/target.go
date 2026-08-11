package message

import (
	"errors"
	"strings"
)

var ErrInvalidTarget = errors.New("invalid delivery target")

// TargetKind 表示消息到期后的逻辑投递方式，不暴露具体 MQ 拓扑。
type TargetKind uint8

const (
	TargetTopic         TargetKind = 1
	TargetConsumerGroup TargetKind = 2
)

// Target 表示发布到原 Topic，或精确回投到指定消费者组。
type Target struct {
	Kind          TargetKind
	ConsumerGroup string
}

func TopicTarget() Target {
	return Target{Kind: TargetTopic}
}

func ConsumerGroupTarget(consumerGroup string) (Target, error) {
	target := Target{
		Kind:          TargetConsumerGroup,
		ConsumerGroup: consumerGroup,
	}
	if err := target.Validate(); err != nil {
		return Target{}, err
	}
	return target, nil
}

// Validate 保证 Topic 目标不携带组名，组目标必须指定组名。
func (t Target) Validate() error {
	switch t.Kind {
	case TargetTopic:
		if t.ConsumerGroup != "" {
			return errors.Join(ErrInvalidTarget, errors.New("topic target has consumer group"))
		}
	case TargetConsumerGroup:
		if strings.TrimSpace(t.ConsumerGroup) == "" {
			return errors.Join(ErrInvalidTarget, errors.New("consumer group is empty"))
		}
	default:
		return errors.Join(ErrInvalidTarget, errors.New("target kind is invalid"))
	}
	return nil
}
