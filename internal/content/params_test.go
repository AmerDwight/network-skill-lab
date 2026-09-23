package content

import (
	"maps"
	"net/netip"
	"strconv"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func resolveOne(t *testing.T, seed uint64, params ...Param) map[string]string {
	t.Helper()
	resolved, err := Params(params).Resolve(seed)
	if err != nil {
		t.Fatalf("Resolve(%d) error = %v", seed, err)
	}
	return resolved
}

func TestResolveIsDeterministicPerSeed(t *testing.T) {
	params := Params{
		{Name: "subnet", Gen: GenCIDR, Base: "10.0.0.0/8", Prefix: 24},
		{Name: "ip_a", Gen: GenIPIn, Subnet: "{{subnet}}", Index: ptr(10)},
		{Name: "ip_b", Gen: GenIPIn, Subnet: "{{subnet}}", Range: []int{20, 200}},
		{Name: "iface", Gen: GenChoice, Of: []string{"eth1", "eth2", "eth3"}},
		{Name: "mtu", Gen: GenInt, Min: ptr(1000), Max: ptr(1500)},
	}

	first, err := params.Resolve(42)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	again, err := params.Resolve(42)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !maps.Equal(first, again) {
		t.Errorf("the same seed produced %v and %v", first, again)
	}

	changed := 0
	for seed := uint64(1); seed <= 20; seed++ {
		other, err := params.Resolve(seed)
		if err != nil {
			t.Fatalf("Resolve(%d) error = %v", seed, err)
		}
		if other["subnet"] != first["subnet"] {
			changed++
		}
	}
	if changed == 0 {
		t.Error("every seed produced the same subnet")
	}
}

func TestResolveCIDR(t *testing.T) {
	base := netip.MustParsePrefix("10.0.0.0/8")
	for seed := uint64(1); seed <= 200; seed++ {
		got := resolveOne(t, seed, Param{Name: "subnet", Gen: GenCIDR, Base: "10.0.0.0/8", Prefix: 24})["subnet"]
		prefix, err := netip.ParsePrefix(got)
		if err != nil {
			t.Fatalf("seed %d produced %q: %v", seed, got, err)
		}
		if prefix.Bits() != 24 {
			t.Fatalf("seed %d produced /%d, want /24", seed, prefix.Bits())
		}
		if !base.Contains(prefix.Addr()) {
			t.Fatalf("seed %d produced %s, which is outside %s", seed, prefix, base)
		}
		if prefix.Masked() != prefix {
			t.Fatalf("seed %d produced the unmasked prefix %s", seed, prefix)
		}
	}
}

func TestResolveCIDRAvoidsTheManagementBlock(t *testing.T) {
	for seed := uint64(1); seed <= 300; seed++ {
		got := resolveOne(t, seed, Param{Name: "subnet", Gen: GenCIDR, Base: "172.0.0.0/8", Prefix: 16})["subnet"]
		prefix := netip.MustParsePrefix(got)
		if prefix.Overlaps(reservedBlock) {
			t.Fatalf("seed %d produced %s, which overlaps %s", seed, prefix, reservedBlock)
		}
	}
}

func TestResolveCIDRRejectsAPrefixThatIsNotSmaller(t *testing.T) {
	tests := []struct {
		name   string
		param  Param
		errish string
	}{
		{"prefix equals base", Param{Name: "subnet", Gen: GenCIDR, Base: "10.0.0.0/8", Prefix: 8}, "must be greater than the base prefix"},
		{"prefix below base", Param{Name: "subnet", Gen: GenCIDR, Base: "10.0.0.0/8", Prefix: 4}, "must be greater than the base prefix"},
		{"prefix above 32", Param{Name: "subnet", Gen: GenCIDR, Base: "10.0.0.0/8", Prefix: 33}, "at most 32"},
		{"base is not a prefix", Param{Name: "subnet", Gen: GenCIDR, Base: "10.0.0.0", Prefix: 24}, "base:"},
		{"base fully inside the management block", Param{Name: "subnet", Gen: GenCIDR, Base: "172.16.0.0/16", Prefix: 24}, "avoids 172.16.0.0/12"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Params{tt.param}.Resolve(1)
			if err == nil || !strings.Contains(err.Error(), tt.errish) {
				t.Fatalf("Resolve() error = %v, want it to contain %q", err, tt.errish)
			}
		})
	}
}

func TestResolveIPInIndex(t *testing.T) {
	resolved := resolveOne(t, 7,
		Param{Name: "subnet", Gen: GenConst, Value: "10.0.5.0/24"},
		Param{Name: "ip_a", Gen: GenIPIn, Subnet: "{{subnet}}", Index: ptr(10)},
		Param{Name: "ip_b", Gen: GenIPIn, Subnet: "{{subnet}}", Index: ptr(20)},
	)
	if resolved["ip_a"] != "10.0.5.10" || resolved["ip_b"] != "10.0.5.20" {
		t.Errorf("addresses = %v", resolved)
	}
}

func TestResolveIPInRange(t *testing.T) {
	for seed := uint64(1); seed <= 100; seed++ {
		resolved := resolveOne(t, seed,
			Param{Name: "subnet", Gen: GenConst, Value: "10.0.5.0/24"},
			Param{Name: "ip", Gen: GenIPIn, Subnet: "{{subnet}}", Range: []int{5, 9}},
		)
		addr := netip.MustParseAddr(resolved["ip"])
		last := addr.As4()[3]
		if last < 5 || last > 9 {
			t.Fatalf("seed %d produced %s, want an address between .5 and .9", seed, addr)
		}
	}
}

func TestResolveIPInRejectsBadSelectors(t *testing.T) {
	const subnet = "10.0.5.0/24"
	tests := []struct {
		name   string
		param  Param
		errish string
	}{
		{"network address", Param{Name: "ip", Gen: GenIPIn, Subnet: subnet, Index: ptr(0)}, "is outside 1..254"},
		{"broadcast address", Param{Name: "ip", Gen: GenIPIn, Subnet: subnet, Index: ptr(255)}, "is outside 1..254"},
		{"range reaches the broadcast address", Param{Name: "ip", Gen: GenIPIn, Subnet: subnet, Range: []int{250, 255}}, "is outside 1..254"},
		{"range starts at the network address", Param{Name: "ip", Gen: GenIPIn, Subnet: subnet, Range: []int{0, 10}}, "is outside 1..254"},
		{"no selector", Param{Name: "ip", Gen: GenIPIn, Subnet: subnet}, "needs index or a range"},
		{"both selectors", Param{Name: "ip", Gen: GenIPIn, Subnet: subnet, Index: ptr(3), Range: []int{1, 5}}, "mutually exclusive"},
		{"subnet is not a prefix", Param{Name: "ip", Gen: GenIPIn, Subnet: "10.0.5.0", Index: ptr(3)}, "subnet:"},
		{"subnet is an unknown param", Param{Name: "ip", Gen: GenIPIn, Subnet: "{{nope}}", Index: ptr(3)}, `unknown param "nope"`},
		{"subnet is too small", Param{Name: "ip", Gen: GenIPIn, Subnet: "10.0.5.0/31", Index: ptr(1)}, "no usable addresses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Params{tt.param}.Resolve(1)
			if err == nil || !strings.Contains(err.Error(), tt.errish) {
				t.Fatalf("Resolve() error = %v, want it to contain %q", err, tt.errish)
			}
		})
	}
}

func TestResolveIPInRejectsDuplicateIndexes(t *testing.T) {
	_, err := Params{
		{Name: "ip_a", Gen: GenIPIn, Subnet: "10.0.5.0/24", Index: ptr(10)},
		{Name: "ip_b", Gen: GenIPIn, Subnet: "10.0.5.0/24", Index: ptr(10)},
	}.Resolve(1)
	if err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("Resolve() error = %v, want a duplicate address error", err)
	}
}

func TestResolveIPInDrawsDistinctAddressesFromTheSameSubnet(t *testing.T) {
	params := Params{
		{Name: "ip_a", Gen: GenIPIn, Subnet: "10.0.5.0/24", Range: []int{1, 3}},
		{Name: "ip_b", Gen: GenIPIn, Subnet: "10.0.5.0/24", Range: []int{1, 3}},
		{Name: "ip_c", Gen: GenIPIn, Subnet: "10.0.5.0/24", Range: []int{1, 3}},
		{Name: "ip_d", Gen: GenIPIn, Subnet: "10.0.6.0/24", Range: []int{1, 3}},
	}
	for seed := uint64(1); seed <= 50; seed++ {
		resolved, err := params.Resolve(seed)
		if err != nil {
			t.Fatalf("Resolve(%d) error = %v", seed, err)
		}
		if resolved["ip_a"] == resolved["ip_b"] || resolved["ip_a"] == resolved["ip_c"] || resolved["ip_b"] == resolved["ip_c"] {
			t.Fatalf("seed %d drew duplicates: %v", seed, resolved)
		}
		if !strings.HasPrefix(resolved["ip_d"], "10.0.6.") {
			t.Fatalf("seed %d drew %q from the wrong subnet", seed, resolved["ip_d"])
		}
	}
}

func TestResolveChoice(t *testing.T) {
	of := []string{"eth1", "eth2", "eth3"}
	seen := map[string]bool{}
	for seed := uint64(1); seed <= 100; seed++ {
		got := resolveOne(t, seed, Param{Name: "iface", Gen: GenChoice, Of: of})["iface"]
		if !contains(of, got) {
			t.Fatalf("seed %d produced %q, which is not in %v", seed, got, of)
		}
		seen[got] = true
	}
	if len(seen) != len(of) {
		t.Errorf("only %d of %d choices were ever drawn", len(seen), len(of))
	}

	if _, err := (Params{{Name: "iface", Gen: GenChoice}}).Resolve(1); err == nil {
		t.Error("an empty choice list resolved, want an error")
	}
}

func TestResolveInt(t *testing.T) {
	low, high := false, false
	for seed := uint64(1); seed <= 200; seed++ {
		got := resolveOne(t, seed, Param{Name: "mtu", Gen: GenInt, Min: ptr(3), Max: ptr(5)})["mtu"]
		value, err := strconv.Atoi(got)
		if err != nil {
			t.Fatalf("seed %d produced %q: %v", seed, got, err)
		}
		if value < 3 || value > 5 {
			t.Fatalf("seed %d produced %d, want 3..5", seed, value)
		}
		low = low || value == 3
		high = high || value == 5
	}
	if !low || !high {
		t.Error("the range is not inclusive on both ends")
	}
}

func TestResolveIntRejectsBadBounds(t *testing.T) {
	tests := []struct {
		name  string
		param Param
	}{
		{"no bounds", Param{Name: "n", Gen: GenInt}},
		{"no max", Param{Name: "n", Gen: GenInt, Min: ptr(1)}},
		{"max below min", Param{Name: "n", Gen: GenInt, Min: ptr(5), Max: ptr(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (Params{tt.param}).Resolve(1); err == nil {
				t.Fatal("Resolve() succeeded, want an error")
			}
		})
	}
}

func TestResolveUnknownGenerator(t *testing.T) {
	if _, err := (Params{{Name: "n", Gen: "dice"}}).Resolve(1); err == nil {
		t.Fatal("Resolve() succeeded, want an error")
	}
}

func TestPickCaseWithoutCases(t *testing.T) {
	picked, err := Lab{}.PickCase(1)
	if err != nil {
		t.Fatalf("PickCase() error = %v", err)
	}
	if picked.ID != DefaultCaseID {
		t.Errorf("case id = %q, want %q", picked.ID, DefaultCaseID)
	}
}

func TestPickCaseFollowsTheWeights(t *testing.T) {
	lab := Lab{Cases: []Case{
		{ID: "default", File: "cases/default.yaml", Weight: 3},
		{ID: "wrong-mtu", File: "cases/wrong-mtu.yaml", Weight: 1},
	}}

	const draws = 2000
	counts := map[string]int{}
	for seed := uint64(1); seed <= draws; seed++ {
		picked, err := lab.PickCase(seed)
		if err != nil {
			t.Fatalf("PickCase(%d) error = %v", seed, err)
		}
		counts[picked.ID]++
	}

	const tolerance = 0.05
	for id, want := range map[string]float64{"default": 0.75, "wrong-mtu": 0.25} {
		got := float64(counts[id]) / draws
		if got < want-tolerance || got > want+tolerance {
			t.Errorf("case %s was drawn %.3f of the time, want %.2f +- %.2f", id, got, want, tolerance)
		}
	}
}

func TestPickCaseRejectsNonPositiveWeights(t *testing.T) {
	lab := Lab{Cases: []Case{{ID: "default", Weight: 0}}}
	if _, err := lab.PickCase(1); err == nil {
		t.Fatal("PickCase() succeeded, want an error")
	}
}

func TestResolveForAppliesTheCase(t *testing.T) {
	lab := Lab{
		Params: Params{
			{Name: "node_a", Gen: GenConst, Value: "web01"},
			{Name: "fault", Gen: GenConst, Value: "link"},
		},
		Cases: []Case{{
			ID:     "wrong-mtu",
			File:   "cases/wrong-mtu.yaml",
			Weight: 1,
			Params: Params{
				{Name: "fault", Gen: GenConst, Value: "mtu"},
				{Name: "extra", Gen: GenConst, Value: "{{node_a}}-extra"},
			},
			SetupEnv: map[string]string{"NSL_FAULT_MTU": "1200"},
		}},
	}

	resolved, err := lab.ResolveFor(1)
	if err != nil {
		t.Fatalf("ResolveFor() error = %v", err)
	}
	if resolved.CaseID != "wrong-mtu" {
		t.Errorf("case id = %q", resolved.CaseID)
	}
	want := map[string]string{"node_a": "web01", "fault": "mtu", "extra": "web01-extra"}
	if !maps.Equal(resolved.Params, want) {
		t.Errorf("params = %v, want %v", resolved.Params, want)
	}
	if !maps.Equal(resolved.SetupEnv, map[string]string{"NSL_FAULT_MTU": "1200"}) {
		t.Errorf("setup env = %v", resolved.SetupEnv)
	}
}

func TestResolvedEnvMergesSetupEnv(t *testing.T) {
	resolved := Resolved{
		Params:   map[string]string{"iface": "eth1", "fault": "link"},
		SetupEnv: map[string]string{"NSL_FAULT": "mtu", "NSL_EXTRA": "1"},
	}
	want := map[string]string{"NSL_IFACE": "eth1", "NSL_FAULT": "mtu", "NSL_EXTRA": "1"}
	if got := resolved.Env(); !maps.Equal(got, want) {
		t.Errorf("Env() = %v, want %v", got, want)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
