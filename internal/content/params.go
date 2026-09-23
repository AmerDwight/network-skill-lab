package content

import (
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"net/netip"
	"slices"
	"strconv"
)

const (
	GenConst  = "const"
	GenCIDR   = "cidr"
	GenIPIn   = "ip_in"
	GenChoice = "choice"
	GenInt    = "int"
)

const DefaultCaseID = "default"

const (
	caseStream   uint64 = 0x9e3779b97f4a7c15
	paramStream  uint64 = 0xc2b2ae3d27d4eb4f
	drawAttempts        = 64
)

var reservedBlock = netip.MustParsePrefix("172.16.0.0/12")

type Case struct {
	File     string            `yaml:"file"`
	Weight   int               `yaml:"weight"`
	ID       string            `yaml:"-"`
	Params   Params            `yaml:"-"`
	SetupEnv map[string]string `yaml:"-"`
}

type caseFile struct {
	Params   Params            `yaml:"params"`
	SetupEnv map[string]string `yaml:"setup_env"`
}

type Resolved struct {
	CaseID   string
	Params   map[string]string
	SetupEnv map[string]string
}

func (r Resolved) Env() map[string]string {
	env := ParamsEnv(r.Params)
	maps.Copy(env, r.SetupEnv)
	return env
}

func (p Params) Resolve(seed uint64) (map[string]string, error) {
	var errs []error
	resolved := p.resolve(seed, func(name string, err error) {
		errs = append(errs, fmt.Errorf("param %s: %w", name, err))
	})
	return resolved, errors.Join(errs...)
}

func (p Params) resolve(seed uint64, onError func(name string, err error)) map[string]string {
	rng := rand.New(rand.NewPCG(seed, paramStream))
	resolved := make(map[string]string, len(p))
	drawn := map[string]map[string]bool{}
	for _, param := range p {
		value, err := resolveParam(rng, param, resolved, drawn)
		if err != nil {
			onError(param.Name, err)
			continue
		}
		resolved[param.Name] = value
	}
	return resolved
}

func resolveParam(rng *rand.Rand, p Param, resolved map[string]string, drawn map[string]map[string]bool) (string, error) {
	switch p.Gen {
	case GenConst:
		return Render(p.Value, resolved)
	case GenCIDR:
		return resolveCIDR(rng, p, resolved)
	case GenIPIn:
		return resolveIPIn(rng, p, resolved, drawn)
	case GenChoice:
		if len(p.Of) == 0 {
			return "", errors.New("of must not be empty")
		}
		return Render(p.Of[rng.IntN(len(p.Of))], resolved)
	case GenInt:
		if p.Min == nil || p.Max == nil {
			return "", errors.New("min and max are required")
		}
		if *p.Max < *p.Min {
			return "", fmt.Errorf("max %d is below min %d", *p.Max, *p.Min)
		}
		return strconv.Itoa(*p.Min + rng.IntN(*p.Max-*p.Min+1)), nil
	default:
		return "", fmt.Errorf("unknown generator %q, allowed: %v", p.Gen, allowedGens)
	}
}

func resolveCIDR(rng *rand.Rand, p Param, resolved map[string]string) (string, error) {
	base, err := parsePrefix(p.Base, "base", resolved)
	if err != nil {
		return "", err
	}
	if p.Prefix <= base.Bits() || p.Prefix > 32 {
		return "", fmt.Errorf("prefix %d must be greater than the base prefix %d and at most 32", p.Prefix, base.Bits())
	}

	span := uint64(1) << (p.Prefix - base.Bits())
	step := uint64(1) << (32 - p.Prefix)
	start := uint64(addrToUint(base.Addr()))
	for range drawAttempts {
		candidate := netip.PrefixFrom(uintToAddr(uint32(start+rng.Uint64N(span)*step)), p.Prefix)
		if !candidate.Overlaps(reservedBlock) {
			return candidate.String(), nil
		}
	}
	return "", fmt.Errorf("no /%d inside %s avoids %s", p.Prefix, base, reservedBlock)
}

func resolveIPIn(rng *rand.Rand, p Param, resolved map[string]string, drawn map[string]map[string]bool) (string, error) {
	subnet, err := parsePrefix(p.Subnet, "subnet", resolved)
	if err != nil {
		return "", err
	}
	size := uint64(1) << (32 - subnet.Bits())
	if size < 4 {
		return "", fmt.Errorf("subnet %s has no usable addresses", subnet)
	}
	last := size - 2

	taken, ok := drawn[subnet.String()]
	if !ok {
		taken = map[string]bool{}
		drawn[subnet.String()] = taken
	}
	network := uint64(addrToUint(subnet.Addr()))

	switch {
	case p.Index != nil && p.Range != nil:
		return "", errors.New("index and range are mutually exclusive")
	case p.Index != nil:
		offset := *p.Index
		if offset < 1 || uint64(offset) > last {
			return "", fmt.Errorf("index %d is outside 1..%d of %s", offset, last, subnet)
		}
		address := uintToAddr(uint32(network + uint64(offset))).String()
		if taken[address] {
			return "", fmt.Errorf("address %s is already used in %s", address, subnet)
		}
		taken[address] = true
		return address, nil
	case len(p.Range) == 2:
		lo, hi := p.Range[0], p.Range[1]
		if lo < 1 || hi < lo || uint64(hi) > last {
			return "", fmt.Errorf("range [%d, %d] is outside 1..%d of %s", lo, hi, last, subnet)
		}
		for range drawAttempts {
			offset := uint64(lo) + rng.Uint64N(uint64(hi-lo+1))
			address := uintToAddr(uint32(network + offset)).String()
			if !taken[address] {
				taken[address] = true
				return address, nil
			}
		}
		return "", fmt.Errorf("every address in [%d, %d] of %s is already used", lo, hi, subnet)
	default:
		return "", errors.New("needs index or a range of two values")
	}
}

func parsePrefix(template, field string, resolved map[string]string) (netip.Prefix, error) {
	rendered, err := Render(template, resolved)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%s: %w", field, err)
	}
	prefix, err := netip.ParsePrefix(rendered)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%s: %w", field, err)
	}
	if !prefix.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("%s: %s is not an IPv4 prefix", field, prefix)
	}
	return prefix.Masked(), nil
}

func addrToUint(addr netip.Addr) uint32 {
	b := addr.As4()
	return binary.BigEndian.Uint32(b[:])
}

func uintToAddr(v uint32) netip.Addr {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return netip.AddrFrom4(b)
}

func (l Lab) PickCase(seed uint64) (Case, error) {
	if len(l.Cases) == 0 {
		return Case{ID: DefaultCaseID, Weight: 1}, nil
	}
	total := 0
	for _, c := range l.Cases {
		if c.Weight <= 0 {
			return Case{}, fmt.Errorf("case %s: weight must be a positive integer, got %d", c.ID, c.Weight)
		}
		total += c.Weight
	}

	draw := rand.New(rand.NewPCG(seed, caseStream)).IntN(total)
	for _, c := range l.Cases {
		draw -= c.Weight
		if draw < 0 {
			return c, nil
		}
	}
	return l.Cases[len(l.Cases)-1], nil
}

func (l Lab) ResolveFor(seed uint64) (Resolved, error) {
	picked, err := l.PickCase(seed)
	if err != nil {
		return Resolved{}, err
	}
	params, err := mergeParams(l.Params, picked.Params).Resolve(seed)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{CaseID: picked.ID, Params: params, SetupEnv: maps.Clone(picked.SetupEnv)}, nil
}

func (l Lab) CaseByID(id string) (Case, bool) {
	for _, c := range l.Cases {
		if c.ID == id {
			return c, true
		}
	}
	return Case{}, false
}

func mergeParams(base, overrides Params) Params {
	if len(overrides) == 0 {
		return base
	}
	merged := slices.Clone(base)
	for _, override := range overrides {
		if i := slices.IndexFunc(merged, func(p Param) bool { return p.Name == override.Name }); i >= 0 {
			merged[i] = override
			continue
		}
		merged = append(merged, override)
	}
	return merged
}
