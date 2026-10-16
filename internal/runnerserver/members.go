package runnerserver

import "github.com/AmerDwight/network-skill-lab/internal/runner"

var _ runner.Remotes = (*Hub)(nil)

func (h *Hub) Members() []runner.Member {
	infos := h.Runners()
	members := make([]runner.Member, 0, len(infos))
	for _, info := range infos {
		members = append(members, runner.Member{
			Name:         info.Name,
			Capabilities: info.Capabilities,
			Capacity:     info.Capacity,
			Connected:    info.Connected,
		})
	}
	return members
}
