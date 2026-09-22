package webapi

import (
	"math"
	"time"
)

// The sample generator.
//
// Every value is a pure function of the series' seed and the absolute
// time bucket, with no state carried between calls. That is what lets an
// auto-refreshing dashboard behave like a real one: the window slides, the
// curve scrolls left, and the history the operator already looked at does
// not silently rewrite itself. A random walk would have looked just as
// plausible for one frame and reshuffled on every refresh.
//
// Three sine waves at unrelated periods give the shape — a slow swell, a
// medium ripple, and a fast one — and a hash adds the jitter that keeps it
// from looking drawn.

func hash01(seed uint64, bucket int64) float64 {
	x := seed ^ uint64(bucket)*0x9e3779b97f4a7c15
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return float64(x>>11) / float64(1<<53)
}

func seedOf(name string) uint64 {
	var h uint64 = 0xcbf29ce484222325
	for i := 0; i < len(name); i++ {
		h ^= uint64(name[i])
		h *= 0x100000001b3
	}
	return h
}

// wave returns a value in [0,1] for one series at one bucket.
func wave(seed uint64, bucket int64) float64 {
	t := float64(bucket)
	p1 := hash01(seed, 1) * 2 * math.Pi
	p2 := hash01(seed, 2) * 2 * math.Pi
	p3 := hash01(seed, 3) * 2 * math.Pi

	v := 0.5
	v += 0.20 * math.Sin(t/61+p1)
	v += 0.11 * math.Sin(t/17+p2)
	v += 0.06 * math.Sin(t/7+p3)
	v += 0.05 * (hash01(seed, bucket) - 0.5) * 2

	return clamp01(v)
}

// burst is wave with the quiet stretches flattened and the busy ones
// exaggerated — the shape of something that arrives in waves rather than
// steadily, like a scanner working through an address range.
func burst(seed uint64, bucket int64) float64 {
	base := wave(seed, bucket)
	return clamp01(math.Pow(base, 3.2) * 1.6)
}

func clamp01(v float64) float64 {
	return math.Min(1, math.Max(0, v))
}

// scale maps a [0,1] wave onto a metric's own range.
func scale(v, lo, hi float64) float64 {
	return lo + v*(hi-lo)
}

// samples builds one series over the window, oldest point first.
func samples(name string, r TimeRange, lo, hi float64, gen func(uint64, int64) float64) []float64 {
	seed := seedOf(name)
	endBucket := bucketOf(r.To, r.StepSeconds)
	out := make([]float64, r.Points)
	for i := 0; i < r.Points; i++ {
		bucket := endBucket - int64(r.Points-1-i)
		out[i] = round2(scale(gen(seed, bucket), lo, hi))
	}
	return out
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// bucketOf converts an instant to its step index. Two requests a second
// apart land on the same bucket, and so return the same value.
func bucketOf(iso string, stepSeconds int) int64 {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil || stepSeconds <= 0 {
		return 0
	}
	return t.Unix() / int64(stepSeconds)
}
