package content

import "testing"

func withCaseParamInTicket(f *fixture) {
	withCases(f)
	f.replace("lab", `zh: "{{node_a}}`, `zh: "{{fault}} {{node_a}}`)
	f.replace("lab", `en: "{{node_a}}`, `en: "{{fault}} {{node_a}}`)
}

func TestTemplatesResolveAgainstEveryCase(t *testing.T) {
	requireValid(t, withCaseParamInTicket)
}

func TestTemplatesMissingFromOneCaseAreRejected(t *testing.T) {
	requireInvalid(t, func(f *fixture) {
		withCaseParamInTicket(f)
		f.files["cases/wrong-mtu.yaml"] = fixtureFile{"params: { mtu: { gen: const, value: \"1200\" } }\n", 0o644}
	}, `ticket.zh: case wrong-mtu: unknown param "fault"`)
}

func TestCheckpointTitlesResolveAgainstEveryCase(t *testing.T) {
	requireInvalid(t, func(f *fixture) {
		withCases(f)
		f.replace("lab", `title: { zh: "{{iface}} 已 UP"`, `title: { zh: "{{fault}} {{iface}} 已 UP"`)
		f.files["cases/wrong-mtu.yaml"] = fixtureFile{"params: { mtu: { gen: const, value: \"1200\" } }\n", 0o644}
	}, `checkpoints[0].title.zh: case wrong-mtu: unknown param "fault"`)
}
