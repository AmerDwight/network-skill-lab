package docker

import "github.com/docker/docker/api/types/filters"

const (
	labelManaged = "nsl.managed"
	labelAttempt = "nsl.attempt"
	labelNode    = "nsl.node"

	namePrefix  = "nsl-"
	mgmtSuffix  = "-mgmt"
	labelFilter = "label"
)

func containerName(attempt, node string) string {
	return namePrefix + attempt + "-" + node
}

func mgmtNetworkName(attempt string) string {
	return namePrefix + attempt + mgmtSuffix
}

func linkNetworkName(attempt, link string) string {
	return namePrefix + attempt + "-" + link
}

func attemptLabels(attempt string) map[string]string {
	return map[string]string{labelManaged: "true", labelAttempt: attempt}
}

func nodeLabels(attempt, node string) map[string]string {
	labels := attemptLabels(attempt)
	labels[labelNode] = node
	return labels
}

func attemptFilter(attempt string) filters.Args {
	return filters.NewArgs(filters.Arg(labelFilter, labelAttempt+"="+attempt))
}

func managedFilter() filters.Args {
	return filters.NewArgs(filters.Arg(labelFilter, labelManaged+"=true"))
}
