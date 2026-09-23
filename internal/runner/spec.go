package runner

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/AmerDwight/network-skill-lab/internal/content"
)

func SpecFromLab(attemptID, image string, lab content.Lab, params map[string]string, setup []byte) (SandboxSpec, error) {
	topology, err := lab.Topology.Resolve(params)
	if err != nil {
		return SandboxSpec{}, fmt.Errorf("resolve topology: %w", err)
	}

	nodes := make([]NodeSpec, 0, len(topology.Nodes))
	for _, name := range slices.Sorted(maps.Keys(topology.Nodes)) {
		nodes = append(nodes, NodeSpec{Name: name, Role: topology.Nodes[name].Role})
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
		Env:       content.ParamsEnv(params),
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
