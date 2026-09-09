// Package telemetry provides the small, dependency-free metric primitives used
// by the control plane's Prometheus exposition endpoint.
package telemetry

import (
	"sync/atomic"
	"time"
)

var reconciliationBounds = [...]time.Duration{time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second, 120 * time.Second, 300 * time.Second}
var reconciliationBuckets [len(reconciliationBounds)]atomic.Uint64
var reconciliationCount atomic.Uint64
var reconciliationNanoseconds atomic.Uint64
var jwksRefreshFailures atomic.Uint64

func ObserveReconciliation(duration time.Duration) {
	reconciliationCount.Add(1)
	reconciliationNanoseconds.Add(uint64(duration))
	for index, bound := range reconciliationBounds {
		if duration <= bound {
			reconciliationBuckets[index].Add(1)
		}
	}
}
func RecordJWKSRefreshFailure() { jwksRefreshFailures.Add(1) }

type Snapshot struct {
	Bounds              []time.Duration
	Buckets             []uint64
	Count               uint64
	SumSeconds          float64
	JWKSRefreshFailures uint64
}

func Current() Snapshot {
	result := Snapshot{Bounds: append([]time.Duration(nil), reconciliationBounds[:]...), Buckets: make([]uint64, len(reconciliationBuckets)), Count: reconciliationCount.Load(), SumSeconds: float64(reconciliationNanoseconds.Load()) / float64(time.Second), JWKSRefreshFailures: jwksRefreshFailures.Load()}
	for index := range reconciliationBuckets {
		result.Buckets[index] = reconciliationBuckets[index].Load()
	}
	return result
}
