package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
)

// Argon2id parameters, pinned in docs/02-TECH-STACK.md § Go: t=3, m=64 MiB,
// p=4, 32-byte hash, 16-byte salt. Source:
// https://pkg.go.dev/golang.org/x/crypto/argon2 — IDKey(password, salt,
// time, memory, threads, keyLen); memory is in KiB, hence 64*1024.
const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	saltLen             = 16
)

// maxArgonTime, maxArgonMemory and maxArgonThreads bound the parameters
// Verify will ever pass to argon2.IDKey when re-deriving a hash to check
// a login against. Hash itself always writes the exact pinned values
// above, but Verify parses whatever m=/t=/p= a stored hash string
// contains — and a hash is data, not code Verify controls. If a hash
// with, say, an absurd memory value ever ended up in the database (a
// migration bug, a restored backup, a compromised row), verifying a
// login against it would otherwise make argon2.IDKey try to allocate
// that much memory on every attempt: a stored denial-of-service that
// fires the moment anyone — attacker or legitimate user — logs into that
// one account. These ceilings are deliberately generous relative to the
// pinned values (room for a future, still-reasonable parameter bump
// without touching this file) while nowhere near what would hurt the
// process.
const (
	maxArgonTime    uint32 = 10
	maxArgonMemory  uint32 = 64 * 1024 // 64 MiB
	maxArgonThreads uint8  = 8
)

// MinPasswordLength is the shortest password Hash accepts. Enforced here —
// not left to each caller — so every path that ever sets a password
// (login has none to set; staff creation and password reset do) gets the
// same boundary validation for free (docs/03-ARCHITECTURE.md § Auth spec:
// "reject passwords < 8 chars at the API boundary").
const MinPasswordLength = 8

// Hash returns password's argon2id hash encoded as a PHC string:
// `$argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>`, salt and hash each
// base64 (unpadded, standard alphabet — the PHC string format's own
// convention) and freshly randomized per call (crypto/rand). It rejects a
// password shorter than MinPasswordLength — counted in Unicode
// characters (utf8.RuneCountInString), matching the contract's
// `minLength`/`maxLength` semantics on StaffCreate/SetStaffPassword, not
// bytes, so a password made of multi-byte characters isn't scored by an
// unrelated byte count — with a *apierr.Error the caller can return
// unwrapped as the request's response.
func Hash(password string) (string, error) {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return "", apierr.Validation(map[string]string{
			"password": fmt.Sprintf("must be at least %d characters", MinPasswordLength),
		})
	}

	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// Verify reports whether password matches encoded, a PHC string Hash
// produced. It re-derives a hash with the parameters and salt embedded in
// encoded and compares in constant time (crypto/subtle), so neither the
// early-exit timing nor the result itself leaks which byte differed.
//
// A malformed encoded that Verify cannot even parse (never produced by
// Hash, but possible if a hash's storage were ever corrupted) is reported
// as an error, not as ok=false — callers must not conflate "not this
// password" with "not a hash at all". A hash that DOES parse but carries
// a salt/digest length or a time/memory/thread cost outside this
// package's pinned envelope (maxArgonTime/maxArgonMemory/maxArgonThreads,
// saltLen, argonKeyLen above) is different: Verify treats it as ok=false
// with no error — the same outward result as a wrong password — and,
// critically, never calls argon2.IDKey with those out-of-envelope values
// at all. Doing otherwise would let a single hostile row in `users`
// (however it got there) turn every login attempt against that account
// into an attempt to run argon2 with attacker-chosen cost, which is a
// stored denial-of-service, not a mere data-integrity bug.
func Verify(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, fmt.Errorf("auth: not a recognized argon2id hash")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, fmt.Errorf("auth: parse hash version: %w", err)
	}

	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false, fmt.Errorf("auth: parse hash params: %w", err)
	}
	if memory == 0 || memory > maxArgonMemory ||
		timeCost == 0 || timeCost > maxArgonTime ||
		threads == 0 || threads > maxArgonThreads {
		return false, nil
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("auth: decode hash salt: %w", err)
	}
	if len(salt) != saltLen {
		return false, nil
	}

	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("auth: decode hash digest: %w", err)
	}
	// Every hash this package ever produces has exactly argonKeyLen bytes
	// of digest (Hash always requests that length); a different length
	// means encoded is not one of ours.
	if len(want) != int(argonKeyLen) {
		return false, nil
	}

	got := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, argonKeyLen)

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is a valid argon2id PHC hash of a password nobody will ever
// enter, computed once (lazily, on first use) at full production cost. Its
// only purpose is timing parity: when a login names a username that does
// not exist, verifying against dummyHash makes an unknown-user rejection
// take the same time as a wrong-password rejection, so a timing side
// channel can't be used to enumerate valid usernames.
var (
	dummyHashOnce  sync.Once
	dummyHashValue string
)

// dummyPassword is arbitrary and never checked against anything; it only
// needs to satisfy MinPasswordLength so Hash doesn't reject it.
const dummyPassword = "never-a-real-password-this-is-only-for-timing-parity"

func dummyHash() string {
	dummyHashOnce.Do(func() {
		h, err := Hash(dummyPassword)
		if err != nil {
			// Unreachable: dummyPassword is a fixed, valid-length literal
			// and the only failure mode of Hash beyond length validation
			// is crypto/rand itself failing, which would already have
			// taken the whole process down elsewhere.
			panic("auth: computing dummy hash: " + err.Error())
		}
		dummyHashValue = h
	})
	return dummyHashValue
}
