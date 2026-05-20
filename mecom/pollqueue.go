package mecom

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// PollResult is the freshest observed value for one queued parameter.
type PollResult struct {
	Parameter  Parameter
	Value      float64
	Error      string
	ObservedAt time.Time
}

// PollQueue rotates normal parameters in bulk-friendly chunks. Manual polls
// are inserted at the front and return through Latest once their turn is read.
type PollQueue struct {
	mu        sync.Mutex
	normal    []Parameter
	normalPos int
	front     []Parameter
	normalSet map[string]struct{}
	frontSet  map[string]struct{}
	latest    map[string]PollResult
}

func NewPollQueue(params []Parameter) *PollQueue {
	q := &PollQueue{
		normalSet: map[string]struct{}{},
		frontSet:  map[string]struct{}{},
		latest:    map[string]PollResult{},
	}
	for _, p := range params {
		q.Enqueue(p)
	}
	return q
}

func ParameterKey(p Parameter) string {
	return fmt.Sprintf("%d:%d:%s", p.ID, p.Instance, p.Type)
}

func (q *PollQueue) Enqueue(p Parameter) {
	q.mu.Lock()
	defer q.mu.Unlock()
	key := ParameterKey(p)
	q.seedLatestLocked(p)
	if _, ok := q.normalSet[key]; ok {
		return
	}
	q.normalSet[key] = struct{}{}
	q.normal = append(q.normal, p)
}

func (q *PollQueue) EnqueueFront(p Parameter) {
	q.mu.Lock()
	defer q.mu.Unlock()
	key := ParameterKey(p)
	q.seedLatestLocked(p)
	if _, ok := q.frontSet[key]; ok {
		return
	}
	q.frontSet[key] = struct{}{}
	q.front = append(q.front, p)
}

func (q *PollQueue) seedLatestLocked(p Parameter) {
	key := ParameterKey(p)
	if _, ok := q.latest[key]; ok {
		return
	}
	q.latest[key] = PollResult{
		Parameter: p,
		Value:     math.NaN(),
		Error:     "not_sampled",
	}
}

func (q *PollQueue) NextChunk(max int) []Parameter {
	if max <= 0 {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Parameter, 0, max)
	for len(out) < max && len(q.front) > 0 {
		p := q.front[0]
		q.front = q.front[1:]
		delete(q.frontSet, ParameterKey(p))
		out = append(out, p)
	}
	if len(q.normal) == 0 {
		return out
	}
	normalLimit := len(q.normal)
	for normalTaken := 0; normalTaken < normalLimit && len(out) < max; normalTaken++ {
		p := q.normal[q.normalPos]
		q.normalPos++
		if q.normalPos >= len(q.normal) {
			q.normalPos = 0
		}
		out = append(out, p)
	}
	return out
}

func (q *PollQueue) Record(result PollResult) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if result.ObservedAt.IsZero() {
		result.ObservedAt = time.Now()
	}
	q.latest[ParameterKey(result.Parameter)] = result
}

func (q *PollQueue) RecordBulk(params []Parameter, values []float64, observedAt time.Time, err error) {
	for i, p := range params {
		result := PollResult{Parameter: p, ObservedAt: observedAt}
		if i < len(values) {
			result.Value = values[i]
		}
		if err != nil {
			result.Error = err.Error()
		}
		q.Record(result)
	}
}

func (q *PollQueue) Latest(p Parameter) (PollResult, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	result, ok := q.latest[ParameterKey(p)]
	return result, ok
}
