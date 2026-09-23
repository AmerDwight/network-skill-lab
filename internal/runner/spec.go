package runner

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/AmerDwight/network-skill-lab/internal/content"
)

const RoleK3sServer = "k3s-server"

var defaultK3sDisable = []string{"traefik", "metrics-server"}

func k3sDisable(node content.Node) []string {
	if node.Role != RoleK3sServer {
		return nil
	}
	if node.K3s == nil {
		return slices.Clone(defaultK3sDisable)
	}
	return slices.Clone(node.K3s.Disable)
}

func SpecFromLab(attemptID, image string, lab content.Lab, resolved content.Resolved, setup []byte) (SandboxSpec, error) {
	topology, err := lab.Topology.Resolve(resolved.Params)
	if err != nil {
		return SandboxSpec{}, fmt.Errorf("resolve topology: %w", err)
	}

	nodes := make([]NodeSpec, 0, len(topology.Nodes))
	for _, name := range slices.Sorted(maps.Keys(topology.Nodes)) {
		node := topology.Nodes[name]
		nodes = append(nodes, NodeSpec{Name: name, Role: node.Role, K3sDisable: k3sDisable(node)})
	}

	links := make([]LinkSpec, 0, len(topology.Links))
	ifaceCount := map[string]int{}
	for i, link := range topology.Links {
		endpoints := make([]EndpointSpec, 0, len(link.Endpoints))
		for _, endpoint := range link.Endpoints {
			ifaceCount[endpoint.Node]++
			expected := fmt.Sprintf("eth%d", ifaceCount[endpoint.Node])
			if endpoint.Iface != expected {
				return SandboxSpec{}, fmt.Errorf("links[%d]: node %s uses %s on its link %d, but Docker assigns interfaces in connect order, so it must be %s", i, endpoint.Node, endpoint.Iface, ifaceCount[endpoint.Node], expected)
			}
			endpoints = append(endpoints, EndpointSpec{Node: endpoint.Node, Iface: endpoint.Iface, Address: link.Addresses[endpoint.Node]})
		}
		links = append(links, LinkSpec{Name: fmt.Sprintf("l%d", i+1), Subnet: link.Subnet, Endpoints: endpoints})
	}

	return SandboxSpec{
		AttemptID: attemptID,
		Image:     image,
		Nodes:     nodes,
		Links:     links,
		Setup:     Script{Name: lab.Setup, Content: setup},
		Env:       resolved.Env(),
		Internet:  lab.InternetEnabled(),
	}, nil
}

func (s SandboxSpec) IfacesEnv(node string) string {
	var pairs []string
	for _, link := range s.Links {
		for _, endpoint := range link.Endpoints {
			if endpoint.Node == node {
				pairs = append(pairs, endpoint.Iface+"="+endpoint.Address)
			}
		}
	}
	return strings.Join(pairs, ",")
}
