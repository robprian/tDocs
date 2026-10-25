package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Argon2id parameters (OWASP-adjacent for interactive logins on server hardware).
const (
	argonTime    uint32 = 2
	argonMemory  uint32 = 64 * 1024 // 64 MiB
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// HashPassword hashes a password with Argon2id and returns the encoded form:
// $argon2id$v=19$m=65536,t=2,p=4$<salt>$<hash>
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("empty password")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword verifies against Argon2id hashes produced by HashPassword.
// Legacy bcrypt hashes ($2a$/...) from earlier releases keep verifying so
// existing share passwords survive the upgrade.
func VerifyPassword(encoded, password string) bool {
	if strings.HasPrefix(encoded, "$argon2id$") {
		return verifyArgon2(encoded, password)
	}
	if strings.HasPrefix(encoded, "$2a$") || strings.HasPrefix(encoded, "$2b$") {
		return bcrypt.CompareHashAndPassword([]byte(encoded), []byte(password)) == nil
	}
	return false
}

func verifyArgon2(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var mem, timeN uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &timeN, &threads); err != nil {
		return false
	}
	if mem > 512*1024 || timeN > 10 || threads == 0 || threads > 16 {
		return false // refuse absurd parameters (DoS guard)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, timeN, mem, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// HashPasswordBcrypt keeps issuing bcrypt hashes where massive parallelism is
// a concern; prefer HashPassword for new secrets.
func HashPasswordBcrypt(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
