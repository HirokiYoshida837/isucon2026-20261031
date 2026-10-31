package isureal

import (
	"testing"
	"time"
)

func TestFailureDetectorObserve(t *testing.T) {
	base := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)

	t.Run("閾値に達したら BAN して数え直す", func(t *testing.T) {
		d := newFailureDetector(10, 10*time.Second)
		for i := range 9 {
			if d.observe("10.0.0.1", base.Add(time.Duration(i)*time.Second)) {
				t.Fatalf("banned at failure %d", i+1)
			}
		}
		if !d.observe("10.0.0.1", base.Add(9*time.Second)) {
			t.Fatal("not banned at failure 10")
		}
		if d.observe("10.0.0.1", base.Add(9*time.Second)) {
			t.Fatal("banned again right after reset")
		}
	})

	t.Run("window の外の失敗は数えない", func(t *testing.T) {
		d := newFailureDetector(10, 10*time.Second)
		for i := range 9 {
			d.observe("10.0.0.2", base.Add(time.Duration(i)*time.Millisecond))
		}
		if d.observe("10.0.0.2", base.Add(10*time.Second)) {
			t.Fatal("banned although the first 9 failures are outside the window")
		}
	})

	t.Run("IP ごとに数える", func(t *testing.T) {
		d := newFailureDetector(2, 5*time.Second)
		if d.observe("10.0.0.3", base) || d.observe("10.0.0.4", base) {
			t.Fatal("banned at the first failure")
		}
		if !d.observe("10.0.0.3", base.Add(time.Second)) {
			t.Fatal("not banned at the second failure")
		}
	})
}

func TestClassifyFailure(t *testing.T) {
	cases := []struct {
		action string
		status int
		abuse  string
		want   failureKind
	}{
		{"login", 401, "", loginFailureKind},
		{"login", 200, "", ""},
		{"put_profile_image", 400, "invalid_image", invalidImageKind},
		{"put_profile_image", 400, "", ""},
		{"get_image", 403, "invalid_signature", invalidSignatureKind},
		{"get_image", 403, "", ""},
		{"", 401, "", ""},
	}
	for _, c := range cases {
		if got := classifyFailure(c.action, c.status, c.abuse); got != c.want {
			t.Errorf("classifyFailure(%q, %d, %q) = %q, want %q", c.action, c.status, c.abuse, got, c.want)
		}
	}
}
