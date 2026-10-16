package vm

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"text/template"
)

const metadataNamespace = "https://network-skill-lab.invalid/vm"

type owner struct {
	Instance string `xml:"instance,attr"`
	Attempt  string `xml:"attempt,attr"`
	Node     string `xml:"node,attr,omitempty"`
	Link     string `xml:"link,attr,omitempty"`
}

type domainParams struct {
	Name      string
	Owner     owner
	MemoryMiB int
	VCPUs     int
	Overlay   string
	Seed      string
	Networks  []string
	SerialLog string
}

type networkParams struct {
	Name  string
	Owner owner
}

var templates = template.Must(template.New("").Funcs(template.FuncMap{"x": escape, "ns": func() string { return metadataNamespace }}).Parse(`
{{- define "metadata" -}}
  <metadata>
    <nsl:sandbox xmlns:nsl='{{ns}}' instance='{{x .Instance}}' attempt='{{x .Attempt}}'{{if .Node}} node='{{x .Node}}'{{end}}{{if .Link}} link='{{x .Link}}'{{end}}/>
  </metadata>
{{- end -}}

{{- define "domain" -}}
<domain type='kvm'>
  <name>{{x .Name}}</name>
{{template "metadata" .Owner}}
  <memory unit='MiB'>{{.MemoryMiB}}</memory>
  <vcpu>{{.VCPUs}}</vcpu>
  <os><type arch='x86_64' machine='q35'>hvm</type><boot dev='hd'/></os>
  <features><acpi/><apic/></features>
  <cpu mode='host-passthrough'/>
  <clock offset='utc'/>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' discard='unmap'/>
      <source file='{{x .Overlay}}'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='{{x .Seed}}'/>
      <target dev='sda' bus='sata'/>
      <readonly/>
    </disk>
{{- range .Networks}}
    <interface type='network'><source network='{{x .}}'/><model type='virtio'/></interface>
{{- end}}
    <channel type='unix'><target type='virtio' name='org.qemu.guest_agent.0'/></channel>
    <serial type='file'><source path='{{x .SerialLog}}' append='off'/><target port='0'/></serial>
    <console type='file'><source path='{{x .SerialLog}}' append='off'/><target type='serial' port='0'/></console>
    <rng model='virtio'><backend model='random'>/dev/urandom</backend></rng>
    <memballoon model='none'/>
  </devices>
</domain>
{{end -}}

{{- define "network" -}}
<network>
  <name>{{x .Name}}</name>
{{template "metadata" .Owner}}
  <bridge stp='off'/>
</network>
{{end -}}
`))

func escape(s string) (string, error) {
	var b bytes.Buffer
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return "", err
	}
	return b.String(), nil
}

func render(name string, data any) ([]byte, error) {
	var b bytes.Buffer
	if err := templates.ExecuteTemplate(&b, name, data); err != nil {
		return nil, fmt.Errorf("render %s xml: %w", name, err)
	}
	return b.Bytes(), nil
}

func domainXML(p domainParams) ([]byte, error) {
	return render("domain", p)
}

func networkXML(p networkParams) ([]byte, error) {
	return render("network", p)
}

type ownerDoc struct {
	Metadata struct {
		Sandbox *owner `xml:"https://network-skill-lab.invalid/vm sandbox"`
	} `xml:"metadata"`
}

func parseOwner(data []byte) (owner, bool) {
	var doc ownerDoc
	if err := xml.Unmarshal(data, &doc); err != nil || doc.Metadata.Sandbox == nil {
		return owner{}, false
	}
	return *doc.Metadata.Sandbox, true
}
