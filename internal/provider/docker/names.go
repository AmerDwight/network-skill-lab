package docker

import "github.com/docker/docker/api/types/filters"

const (
	labelManaged  = "nsl.managed"
	labelInstance = "nsl.instance"
	labelAttempt  = "nsl.attempt"
	labelNode     = "nsl.node"
	labelRole     = "nsl.role"

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

func attemptLabels(instance, attempt string) map[string]string {
	return map[string]string{labelManaged: "true", labelInstance: instance, labelAttempt: attempt}
}

func nodeLabels(instance, attempt, node, role string) map[string]string {
	labels := attemptLabels(instance, attempt)
	labels[labelNode] = node
	labels[labelRole] = role
	return labels
}

func attemptFilter(attempt string) filters.Args {
	return filters.NewArgs(filters.Arg(labelFilter, labelAttempt+"="+attempt))
}

func instanceFilter(instance string) filters.Args {
	return filters.NewArgs(
		filters.Arg(labelFilter, labelManaged+"=true"),
		filters.Arg(labelFilter, labelInstance+"="+instance),
	)
}
