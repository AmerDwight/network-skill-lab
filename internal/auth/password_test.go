package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=1,p=4$") {
		t.Fatalf("hash = %q", hash)
	}

	ok, err := VerifyPassword(hash, "correct horse battery")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !ok {
		t.Error("VerifyPassword() = false for the right password")
	}

	ok, err = VerifyPassword(hash, "correct horse batter")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if ok {
		t.Error("VerifyPassword() = true for the wrong password")
	}

	other, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if other == hash {
		t.Error("two hashes of the same password are identical, the salt is not random")
	}
}

func TestVerifyPasswordRejectsATamperedHash(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	fields := strings.Split(hash, "$")
	key := []byte(fields[5])
	if key[0] == 'A' {
		key[0] = 'B'
	} else {
		key[0] = 'A'
	}
	fields[5] = string(key)

	ok, err := VerifyPassword(strings.Join(fields, "$"), "correct horse battery")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if ok {
		t.Error("VerifyPassword() = true for a tampered hash")
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	hashes := []string{
		"",
		"plain-text",
		"$argon2i$v=19$m=65536,t=1,p=4$c2FsdHNhbHRzYWx0$a2V5",
		"$argon2id$v=18$m=65536,t=1,p=4$c2FsdHNhbHRzYWx0$a2V5",
		"$argon2id$v=19$m=x,t=1,p=4$c2FsdHNhbHRzYWx0$a2V5",
		"$argon2id$v=19$m=65536,t=1,p=4$!!!$a2V5",
	}
	for _, hash := range hashes {
		if _, err := VerifyPassword(hash, "whatever"); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("VerifyPassword(%q) error = %v, want ErrInvalidHash", hash, err)
		}
	}
}

func TestValidateUsername(t *testing.T) {
	valid := []string{"abc", "alice", "a-b_c", "a1234567890123456789012345678901"}
	for _, username := range valid {
		if err := ValidateUsername(username); err != nil {
			t.Errorf("ValidateUsername(%q) error = %v", username, err)
		}
	}

	invalid := []string{"", "ab", "Alice", "alice bob", "a.b", "a12345678901234567890123456789012", "ålice"}
	for _, username := range invalid {
		if err := ValidateUsername(username); !errors.Is(err, ErrInvalidUsername) {
			t.Errorf("ValidateUsername(%q) error = %v, want ErrInvalidUsername", username, err)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("12345678"); err != nil {
		t.Errorf("ValidatePassword() error = %v", err)
	}
	if err := ValidatePassword("1234567"); !errors.Is(err, ErrInvalidPassword) {
		t.Errorf("ValidatePassword(short) error = %v, want ErrInvalidPassword", err)
	}
}
