package vm

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

const (
	namePrefix  = "nsl-"
	maxNameLen  = 64
	hashLen     = 8
	defaultInst = "default"
)

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, s)
}

func objectName(instance, attempt, item string) string {
	name := namePrefix + sanitize(instance) + "-" + sanitize(attempt) + "-" + sanitize(item)
	if len(name) <= maxNameLen {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	suffix := "-" + hex.EncodeToString(sum[:])[:hashLen]
	return name[:maxNameLen-len(suffix)] + suffix
}

func domainName(instance, attempt, node string) string {
	return objectName(instance, attempt, node)
}

func networkName(instance, attempt, link string) string {
	return objectName(instance, attempt, link)
}

func instanceDir(root, instance string) string {
	return filepath.Join(root, dirName(instance))
}

func sandboxDir(root, instance, attempt string) string {
	return filepath.Join(instanceDir(root, instance), dirName(attempt))
}

func dirName(s string) string {
	name := sanitize(s)
	if name == "" || name == "." || name == ".." {
		return "_" + name
	}
	return name
}
