package types

import "time"

type TaskSchedulerConfig struct {
	Workers           int
	LeaseDuration     time.Duration
	MaxAttempts       int
	HeartbeatInterval time.Duration
}
