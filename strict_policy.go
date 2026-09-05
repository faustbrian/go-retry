package retry

import "fmt"

// NewPolicyStrict validates and copies config while rejecting explicitly
// supplied typed-nil optional collaborators.
func NewPolicyStrict(config Config) (*Policy, error) {
	return newPolicy(config, true)
}

func newPolicy(config Config, strict bool) (*Policy, error) {
	switch {
	case nilLike(config.Backoff):
		return nil, fmt.Errorf("%w: backoff is required", ErrInvalidPolicy)
	case config.MaxAttempts == 0:
		return nil, fmt.Errorf("%w: max attempts must be positive", ErrInvalidPolicy)
	case nilLike(config.Clock):
		return nil, fmt.Errorf("%w: clock is required", ErrInvalidPolicy)
	case nilLike(config.Sleeper):
		return nil, fmt.Errorf("%w: sleeper is required", ErrInvalidPolicy)
	case nilLike(config.Classifier):
		return nil, fmt.Errorf("%w: classifier is required", ErrInvalidPolicy)
	case config.MaxElapsed < 0 || config.AttemptTimeout < 0 || config.MinDelay < 0 || config.MaxDelay < 0 || config.MaxSleep < 0:
		return nil, fmt.Errorf("%w: durations cannot be negative", ErrInvalidPolicy)
	case config.MaxDelay > 0 && config.MinDelay > config.MaxDelay:
		return nil, fmt.Errorf("%w: minimum delay exceeds maximum delay", ErrInvalidPolicy)
	case config.HistoryLimit > MaxHistoryEntries:
		return nil, fmt.Errorf("%w: history limit exceeds %d", ErrInvalidPolicy, MaxHistoryEntries)
	case strict && config.Random != nil && nilLike(config.Random):
		return nil, fmt.Errorf("%w: random is typed nil", ErrInvalidPolicy)
	case strict && config.Observer != nil && nilLike(config.Observer):
		return nil, fmt.Errorf("%w: observer is typed nil", ErrInvalidPolicy)
	default:
		if nilLike(config.Random) {
			config.Random = nil
		}
		if nilLike(config.Observer) {
			config.Observer = nil
		}
		return &Policy{config: config}, nil
	}
}
