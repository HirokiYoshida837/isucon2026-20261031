package isureal

import "testing"

func TestCountSigBatch(t *testing.T) {
	for _, c := range []struct{ n, batch, lanes, scalar int64 }{
		{3, 1, 3, 0}, {16, 1, 16, 0}, {18, 1, 16, 2}, {19, 2, 19, 0}, {34, 2, 32, 2},
	} {
		b0, l0, s0 := sigStat[sPushBatch].Load(), sigStat[sPushLanes].Load(), sigStat[sPushScalar].Load()
		countSigBatch(sPushBatch, int(c.n))
		if b, l, s := sigStat[sPushBatch].Load()-b0, sigStat[sPushLanes].Load()-l0, sigStat[sPushScalar].Load()-s0; b != c.batch || l != c.lanes || s != c.scalar {
			t.Fatalf("n=%d: batch=%d lanes=%d scalar=%d", c.n, b, l, s)
		}
	}
	flushSigStat("test")
	flushSigStat("test") // 増分が無ければ何も出さない
	for i := range sigStat {
		if sigStatLast[i] != sigStat[i].Load() {
			t.Fatal("flush で前回値が更新されない")
		}
	}
}
