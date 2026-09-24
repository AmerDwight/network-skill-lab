package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonSaltLen = 16
	argonKeyLen  = 32
)

var ErrInvalidHash = errors.New("invalid password hash")

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func VerifyPassword(hash, password string) (bool, error) {
	params, salt, key, err := decodeHash(hash)
	if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(key, candidate) == 1, nil
}

func decodeHash(hash string) (argonParams, []byte, []byte, error) {
	fields := strings.Split(hash, "$")
	if len(fields) != 6 || fields[0] != "" || fields[1] != "argon2id" {
		return argonParams{}, nil, nil, fmt.Errorf("%w: unexpected format", ErrInvalidHash)
	}

	var version int
	if _, err := fmt.Sscanf(fields[2], "v=%d", &version); err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("%w: parse version: %w", ErrInvalidHash, err)
	}
	if version != argon2.Version {
		return argonParams{}, nil, nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidHash, version)
	}

	var params argonParams
	if _, err := fmt.Sscanf(fields[3], "m=%d,t=%d,p=%d", &params.memory, &params.time, &params.threads); err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("%w: parse parameters: %w", ErrInvalidHash, err)
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(fields[4])
	if err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("%w: decode salt: %w", ErrInvalidHash, err)
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(fields[5])
	if err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("%w: decode key: %w", ErrInvalidHash, err)
	}
	if len(salt) == 0 || len(key) == 0 {
		return argonParams{}, nil, nil, fmt.Errorf("%w: empty salt or key", ErrInvalidHash)
	}
	return params, salt, key, nil
}
