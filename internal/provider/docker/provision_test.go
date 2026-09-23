package docker

import "testing"

func TestFailedUnits(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		want   string
	}{
		{"empty", "", "none"},
		{"blank lines", "\n  \n", "none"},
		{
			"one unit",
			"systemd-udevd.service loaded failed failed Rule-based Manager for Device Events and Files\n",
			"systemd-udevd.service loaded failed failed Rule-based Manager for Device Events and Files",
		},
		{
			"two units",
			"a.service loaded failed failed A\nb.service loaded failed failed B\n",
			"a.service loaded failed failed A; b.service loaded failed failed B",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := failedUnits([]byte(tc.stdout)); got != tc.want {
				t.Errorf("failedUnits = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsActive(t *testing.T) {
	for _, stdout := range []string{"active\n", " active "} {
		if !isActive([]byte(stdout)) {
			t.Errorf("isActive(%q) = false, want true", stdout)
		}
	}
	for _, stdout := range []string{"", "activating\n", "inactive\n", "failed\n"} {
		if isActive([]byte(stdout)) {
			t.Errorf("isActive(%q) = true, want false", stdout)
		}
	}
}
